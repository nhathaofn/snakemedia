import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { QueuePrompt } from '../../components/batch/QueueCells';
import { Button } from '../../components/ui';
import { api } from '../../services';
import { MuseBatchPanel, MuseQueueWorkspace } from './batch/MuseBatch';
import { MuseSinglePanel } from './single/MuseSingle';
import { MuseJobKind, MuseJobMode } from '../../types';

interface NodeFs {
  readFileSync(path: URL, encoding: 'utf8'): string;
  readdirSync(path: URL, options: { recursive: true }): string[];
}
const fs = (await import(/* @vite-ignore */ 'node:fs' as string)) as NodeFs;

const readSrc = (rel: string): string => fs.readFileSync(new URL(rel, import.meta.url), 'utf8');

describe('Muse Phase P2 UI & A11y Fixes (F-1 .. F-5)', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('F-1: MuseBatchPanel renders ReferencesField and MuseAccountStatus parity', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <MuseBatchPanel kind={MuseJobKind.Image} />
      </MemoryRouter>,
    );
    expect(html).toContain('Ảnh tham chiếu');
    expect(html).toContain('tài khoản Muse');
  });

  it('F-2: MuseSinglePanel keeps PromptCheckHint outside closed <details class="muse-extras">', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <MuseSinglePanel kind={MuseJobKind.Video} />
      </MemoryRouter>,
    );
    const detailsIdx = html.indexOf('<details class="muse-extras"');
    expect(detailsIdx).toBeGreaterThan(-1);

    const singleSrc = readSrc('./single/MuseSingle.tsx');
    const extrasIdx = singleSrc.indexOf('<ExtrasGroup');
    const hintIdx = singleSrc.indexOf('<PromptCheckHint');
    expect(hintIdx).toBeGreaterThan(-1);
    expect(hintIdx).toBeLessThan(extrasIdx);
  });

  it('F-3: Muse Save/Asset/Template/BatchImport dialogs lock closing during async save & exclude ignored errors', () => {
    const saveDialog = readSrc('./assets/SaveToVaultDialog.tsx');
    expect(saveDialog).toContain('onClose={() => !saving && onClose()}');

    const assetDialog = readSrc('./assets/AssetDialog.tsx');
    expect(assetDialog).toContain('onClose={() => !saving && onClose()}');

    const templateSection = readSrc('./assets/TemplateSection.tsx');
    expect(templateSection).toContain('onClose={() => !saving && onClose()}');

    const batchDialog = readSrc('./assets/BatchImportAssetsDialog.tsx');
    expect(batchDialog).toContain('!r.excluded');
    expect(batchDialog).toContain('onClose={closeDialog}');
    expect(batchDialog).toMatch(/const closeDialog = \(\) => \{\s*if \(importing\) return;/);
    expect(batchDialog).not.toContain('clearOverrideField');
  });

  it('F-4: MuseBatch renders Đã huỷ filter, updated Enter copy, min-gap unit, and object-fit contain', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <MuseQueueWorkspace kind={MuseJobKind.Image} />
      </MemoryRouter>,
    );
    expect(html).toContain('Đã huỷ');

    const batchSrc = readSrc('./batch/MuseBatch.tsx');
    expect(batchSrc).toContain('nhấn Enter');
    expect(batchSrc).not.toContain('bấm Thêm');

    const settingsSrc = readSrc('./settings/MuseSettingsPage.tsx');
    expect(settingsSrc).toContain('aria-label="Giãn cách tối thiểu"');
    expect(settingsSrc).toContain('unit="giây"');
    expect(settingsSrc).not.toContain('patch({ assetVaultDir');

    const libCss = readSrc('./library/MuseLibraryPage.css');
    expect(libCss).toContain('object-fit: contain;');
  });

  it('F-4: useMuseStudioStore.ensure resets started lock after failed load so retries succeed', async () => {
    vi.resetModules();
    const { useMuseStudioStore: freshStore } = await import('./shared/museStudioStore');
    const { api: freshApi } = await import('../../services');
    vi.restoreAllMocks();
    void api;

    const failSpy = vi
      .spyOn(freshApi, 'listMuseAssets')
      .mockRejectedValueOnce(new Error('offline'));
    vi.spyOn(freshApi, 'listMuseTemplates').mockResolvedValue({
      success: true,
      data: [],
      code: 200,
    } as never);

    freshStore.getState().ensure();
    await new Promise((r) => setTimeout(r, 20));
    expect(freshStore.getState().loadError).toContain('offline');

    failSpy.mockResolvedValueOnce({ success: true, data: [], code: 200 } as never);
    freshStore.getState().ensure();
    await new Promise((r) => setTimeout(r, 20));
    expect(freshStore.getState().loadError).toBeNull();
    expect(failSpy).toHaveBeenCalledTimes(2);
    void MuseJobMode;
  });

  it('F-5: Button defaults type="button", QueuePrompt exposes edit button, and no window.confirm remains in Muse/Accounts', () => {
    const btnHtml = renderToStaticMarkup(<Button>Lưu</Button>);
    expect(btnHtml).toContain('type="button"');

    const promptHtml = renderToStaticMarkup(
      <QueuePrompt prompt="test prompt" editable saveLabel="Lưu" onSave={() => {}} />,
    );
    expect(promptHtml).toContain('aria-label="Sửa prompt"');

    const indexHtml = readSrc('../../../index.html');
    expect(indexHtml).toContain('<html lang="vi">');

    const tokensCss = readSrc('../../styles/tokens.css');
    const cardMinCount = (tokensCss.match(/--muse-asset-card-min:/g) ?? []).length;
    expect(cardMinCount).toBe(1);

    const globalsCss = readSrc('../../styles/globals.css');
    expect(globalsCss).not.toContain('#App');

    const srcRoot = new URL('../../', import.meta.url);
    const allSrcFiles = fs
      .readdirSync(srcRoot, { recursive: true })
      .map((name) => name.replaceAll('\\', '/'))
      .filter((name) => /\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name));
    for (const rel of allSrcFiles) {
      expect(readSrc(`../../${rel}`), `${rel} must not call window.confirm`).not.toContain(
        'window.confirm(',
      );
    }

    const museSectionSrc = readSrc('./section.tsx');
    expect(museSectionSrc).toContain('key={MuseJobKind.Image}');
    expect(museSectionSrc).toContain('key={MuseJobKind.Video}');
  });
});
