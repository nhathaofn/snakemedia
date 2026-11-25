import { describe, expect, it } from 'vitest';
import { MuseAsset, MuseAssetBatchConflict, MuseFolderImage, MuseRefRole } from '../../../types';
import {
  buildBatchRows,
  parseAliasLines,
  parseLockList,
  parseRoleLines,
  sanitizeTag,
} from './batchImport';

const makeImage = (fileName: string, error = ''): MuseFolderImage =>
  MuseFolderImage.createFrom({
    path: `C:\\imgs\\${fileName}`,
    fileName,
    size: 1024,
    previewUrl: '',
    error,
  });

describe('batchImport helpers', () => {
  it('parses alias lines with @tag, delimiter, and spaces', () => {
    const bom = String.fromCharCode(0xfeff);
    const parsed = parseAliasLines(`${bom}@lan\r\nhop | Hộp quà Tết\r\nCô gái áo đỏ\r\n\r\n`);
    expect(parsed).toEqual([
      { tag: 'lan', name: '' },
      { tag: 'hop', name: 'Hộp quà Tết' },
      { tag: 'Cô-gái-áo-đỏ', name: 'Cô gái áo đỏ' },
    ]);
  });

  it('sanitizes tags up to 40 chars with fallback', () => {
    expect(sanitizeTag('  @Mèo (xám)!  ')).toBe('Mèo-xám');
    expect(sanitizeTag('***', '01_anh')).toBe('01_anh');
    expect(sanitizeTag('a'.repeat(50))).toHaveLength(40);
  });

  it('parses Vietnamese and English role aliases', () => {
    const roles = parseRoleLines('nhân vật\nkhuôn mặt\nsản phẩm\n-\nunknown-role');
    expect(roles[0].role).toBe(MuseRefRole.Character);
    expect(roles[1].role).toBe(MuseRefRole.Face);
    expect(roles[2].role).toBe(MuseRefRole.Product);
    expect(roles[3].role).toBeUndefined();
    expect(roles[3].error).toBeUndefined();
    expect(roles[4].error).toBeTruthy();
  });

  it('splits lock prompts in line, positional, and block modes', () => {
    const text = 'khoá 1\n\nkhoá 2 dòng 1\nkhoá 2 dòng 2\n\n';
    expect(parseLockList(text, 'line')).toEqual(['khoá 1', 'khoá 2 dòng 1', 'khoá 2 dòng 2']);
    expect(parseLockList('khoá 1\n\nkhoá 3\n', 'positional')).toEqual(['khoá 1', '', 'khoá 3']);
    expect(parseLockList(text, 'block')).toEqual(['khoá 1', 'khoá 2 dòng 1\nkhoá 2 dòng 2']);
  });

  it('resolves conflicts and reports count mismatches', () => {
    const existing = [
      MuseAsset.createFrom({
        id: 'old-1',
        tag: 'lan',
        name: 'Lan',
        role: MuseRefRole.Face,
        lock: '',
        images: ['/flow-media/muse-asset-1.png'],
        createdAt: '',
      }),
    ];
    const images = [makeImage('1.png'), makeImage('2.png'), makeImage('3.png')];
    const aliases = parseAliasLines('lan\nlan\nhop');
    const roles = parseRoleLines('face\nproduct');
    const locks = parseLockList('nốt ruồi\nchữ ABC', 'line');

    const skipRes = buildBatchRows({
      images,
      aliases,
      roles,
      locks,
      defaultRole: MuseRefRole.Character,
      conflict: MuseAssetBatchConflict.Skip,
      existingAssets: existing,
      overrides: new Map(),
    });
    expect(skipRes.rows.map((r) => r.status)).toEqual(['skip', 'skip', 'ready']);
    expect(skipRes.warnings).toHaveLength(2);

    const renameRes = buildBatchRows({
      images,
      aliases,
      roles,
      locks,
      defaultRole: MuseRefRole.Character,
      conflict: MuseAssetBatchConflict.Rename,
      existingAssets: existing,
      overrides: new Map(),
    });
    expect(renameRes.rows.map((r) => [r.status, r.resolvedTag])).toEqual([
      ['rename', 'lan_2'],
      ['rename', 'lan_3'],
      ['ready', 'hop'],
    ]);

    const overRes = buildBatchRows({
      images,
      aliases,
      roles,
      locks,
      defaultRole: MuseRefRole.Character,
      conflict: MuseAssetBatchConflict.Overwrite,
      existingAssets: existing,
      overrides: new Map(),
    });
    expect(overRes.rows[0].status).toBe('overwrite');
    expect(overRes.rows[1].status).toBe('overwrite');
  });
});
