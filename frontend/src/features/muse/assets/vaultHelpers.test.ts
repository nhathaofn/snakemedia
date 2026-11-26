import { describe, expect, it } from 'vitest';
import {
  MuseAssetBatchConflict,
  MuseAssetType,
  MuseFolderImage,
  MuseRefRole,
} from '../../../types';
import type { MuseAsset } from '../../../types';
import { buildCanonicalPreviewName, cleanToken, normalizeProjectName } from './assetRoles';
import { buildBatchRows } from './batchImport';

describe('Vault naming and convention helpers', () => {
  it('folds Vietnamese characters and formats canonical convention filenames', () => {
    expect(cleanToken('Áo Dài Đỏ')).toBe('ao_dai_do');
    expect(normalizeProjectName('Oliva Cottage')).toBe('OLIVA_COTTAGE');
    expect(
      buildCanonicalPreviewName({
        project: 'oliva',
        type: MuseAssetType.Char,
        alias: 'jimena+hester',
        angle: 'full',
        variant: 'work',
        state: 'soot',
        version: 2,
        rejected: true,
        ext: '.png',
      }),
    ).toBe('OLIVA_CHAR_jimena+hester_full_work_soot_v2_rej.png');
  });

  it('auto-fills fields from parsed filename hints and merges multiple angles under one tag', () => {
    const existing: MuseAsset[] = [];
    const images: MuseFolderImage[] = [
      MuseFolderImage.createFrom({
        path: 'C:/tmp/OLIVA_CHAR_jimena_front.png',
        fileName: 'OLIVA_CHAR_jimena_front.png',
        size: 100,
        parsed: {
          project: 'OLIVA',
          type: MuseAssetType.Char,
          role: MuseRefRole.Character,
          alias: 'jimena',
          angle: 'front',
        },
      }),
      MuseFolderImage.createFrom({
        path: 'C:/tmp/OLIVA_CHAR_jimena_close_soot.png',
        fileName: 'OLIVA_CHAR_jimena_close_soot.png',
        size: 100,
        parsed: {
          project: 'OLIVA',
          type: MuseAssetType.Char,
          role: MuseRefRole.Character,
          alias: 'jimena',
          angle: 'close',
          state: 'soot',
        },
      }),
    ];

    const { rows } = buildBatchRows({
      images,
      aliases: [],
      roles: [],
      locks: [],
      defaultRole: MuseRefRole.Prop,
      conflict: MuseAssetBatchConflict.Skip,
      mergeByAlias: true,
      existingAssets: existing,
      overrides: new Map(),
    });

    expect(rows).toHaveLength(2);
    expect(rows[0].project).toBe('OLIVA');
    expect(rows[0].tag).toBe('jimena');
    expect(rows[0].angle).toBe('front');
    expect(rows[0].status).toBe('ready');
    expect(rows[1].tag).toBe('jimena');
    expect(rows[1].angle).toBe('close');
    expect(rows[1].state).toBe('soot');
    expect(rows[1].status).toBe('ready');
    expect(rows[1].canonicalName).toBe('OLIVA_CHAR_jimena_close_soot.png');
  });
});
