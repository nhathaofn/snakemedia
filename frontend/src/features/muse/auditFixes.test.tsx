import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { BatchForm } from '../../components/batch/BatchForm';
import { AssetCard } from './assets/AssetCard';
import { TemplateSection } from './assets/TemplateSection';
import { JobRow } from './batch/JobRow';
import { MuseBatchPanel } from './batch/MuseBatch';
import { MuseLibraryPage } from './library/MuseLibraryPage';
import { MuseMediaLightbox } from './shared/MuseMediaLightbox';
import { MuseSinglePanel, SingleTile } from './single/MuseSingle';
import { MuseAssetType, MuseJobKind, MuseJobMode, MuseJobStatus, MuseRefRole } from '../../types';
import type { MuseAsset, MuseJob, MusePromptTemplate } from '../../types';
import type { JobActions } from './shared/useMuseActions';

interface NodeFs {
  readFileSync(path: URL, encoding: 'utf8'): string;
}
const fs = (await import(/* @vite-ignore */ 'node:fs' as string)) as NodeFs;
const readSrc = (rel: string): string => fs.readFileSync(new URL(rel, import.meta.url), 'utf8');

const noopActions: JobActions = {
  run: () => {},
  cancel: () => {},
  retry: () => {},
  retrySave: () => {},
  remove: () => {},
  editPrompt: () => {},
  save: () => {},
  reveal: () => {},
};

const makeJob = (overrides: Partial<Omit<MuseJob, 'convertValues'>> = {}): MuseJob =>
  ({
    id: 'job-1',
    kind: MuseJobKind.Video,
    mode: MuseJobMode.Single,
    prompt: 'mèo đi trên mái nhà',
    ratio: '9:16',
    duration: 10,
    references: [],
    status: MuseJobStatus.Queued,
    attempts: 0,
    files: [],
    savedFiles: [],
    createdAt: new Date().toISOString(),
    ...overrides,
  }) as unknown as MuseJob;

describe('Muse audit fixes & UI upgrades', () => {
  it('shows storyboard review instructions instead of generic Pause text and gates Play while waiting for frames', () => {
    const waitingBoard = makeJob({
      id: 'wait-sb',
      held: true,
      paused: false,
      after: 'sb-1',
      frames: [],
      step: 'Chờ storyboard',
    });
    const reviewReady = makeJob({
      id: 'review-sb',
      held: true,
      paused: true,
      review: true,
      after: 'sb-2',
      frames: ['/flow-media/f1.png'],
      step: 'Đã có storyboard, xem rồi bấm Chạy để làm video',
    });
    const manualPaused = makeJob({
      id: 'manual-pause',
      held: true,
      paused: true,
      step: '',
    });
    const noted = makeJob({
      id: 'noted',
      status: MuseJobStatus.Succeeded,
      files: ['/flow-media/out.mp4'],
      formatNote: 'ra 1080×1920 thay vì 16:9',
    });

    const waitHtml = renderToStaticMarkup(
      <SingleTile
        job={waitingBoard}
        actions={noopActions}
        onPreview={() => {}}
        onReuse={() => {}}
      />,
    );
    expect(waitHtml).toContain('Chờ storyboard');
    expect(waitHtml).toContain('Video · 9:16 · 10s');
    expect(waitHtml).not.toContain('aria-label="Bắt đầu"');

    const reviewHtml = renderToStaticMarkup(
      <SingleTile
        job={reviewReady}
        actions={noopActions}
        onPreview={() => {}}
        onReuse={() => {}}
      />,
    );
    expect(reviewHtml).toContain('Đã có storyboard, xem rồi bấm Chạy để làm video');
    expect(reviewHtml).toContain('aria-label="Bắt đầu"');

    const pausedHtml = renderToStaticMarkup(
      <SingleTile
        job={manualPaused}
        actions={noopActions}
        onPreview={() => {}}
        onReuse={() => {}}
      />,
    );
    expect(pausedHtml).toContain('Tạm dừng');

    const notedHtml = renderToStaticMarkup(
      <SingleTile job={noted} actions={noopActions} onPreview={() => {}} onReuse={() => {}} />,
    );
    expect(notedHtml).toContain('muse-tile-note-line');
    expect(notedHtml).toContain('ra 1080×1920 thay vì 16:9');
  });

  it('distinguishes Chờ bấm Chạy from Chờ chạy and shows queued job.step details in JobRow', () => {
    const heldBatch = makeJob({
      id: 'b-held',
      mode: MuseJobMode.Batch,
      held: true,
      paused: false,
    });
    const activeQueued = makeJob({
      id: 'b-queued',
      mode: MuseJobMode.Batch,
      held: false,
      paused: false,
    });
    const reviewPaused = makeJob({
      id: 'b-review',
      mode: MuseJobMode.Batch,
      held: true,
      paused: true,
      review: true,
      after: 'sb-1',
      frames: ['/flow-media/f1.png'],
      step: 'Đã có storyboard, xem rồi bấm Chạy để làm video',
    });

    const heldHtml = renderToStaticMarkup(
      <JobRow
        job={heldBatch}
        number={1}
        selected={false}
        accountName=""
        actions={noopActions}
        onToggle={() => {}}
        onPreview={() => {}}
      />,
    );
    expect(heldHtml).toContain('Chờ bấm Chạy');
    expect(heldHtml).toContain('is-held');

    const queuedHtml = renderToStaticMarkup(
      <JobRow
        job={activeQueued}
        number={2}
        selected={false}
        accountName=""
        actions={noopActions}
        onToggle={() => {}}
        onPreview={() => {}}
      />,
    );
    expect(queuedHtml).toContain('Chờ chạy');
    expect(queuedHtml).toContain('is-queued');

    const reviewHtml = renderToStaticMarkup(
      <JobRow
        job={reviewPaused}
        number={3}
        selected={false}
        accountName=""
        actions={noopActions}
        onToggle={() => {}}
        onPreview={() => {}}
      />,
    );
    expect(reviewHtml).toContain('Tạm dừng');
    expect(reviewHtml).toContain('Đã có storyboard, xem rồi bấm Chạy để làm video');
  });

  it('supports controlled prompt value in BatchForm and wires TemplateField + draft outputDir + batch progress in MuseBatch', () => {
    const controlledHtml = renderToStaticMarkup(
      <BatchForm
        id="test-batch"
        pending={[]}
        value="hello batch"
        onChange={() => {}}
        onAdd={async () => true}
      />,
    );
    expect(controlledHtml).toContain('hello batch');
    expect(controlledHtml).not.toContain('batch-meta has-extra');

    const withExtraActionsHtml = renderToStaticMarkup(
      <BatchForm
        id="test-batch-extra"
        pending={[]}
        value="hello batch"
        onChange={() => {}}
        renderPromptActions={() => <button type="button">Xem câu gửi</button>}
        onAdd={async () => true}
      />,
    );
    expect(withExtraActionsHtml).toContain('batch-meta has-extra');
    expect(withExtraActionsHtml).toContain('Xem câu gửi');

    const batchHtml = renderToStaticMarkup(
      <MemoryRouter>
        <MuseBatchPanel kind={MuseJobKind.Video} />
      </MemoryRouter>,
    );
    expect(batchHtml).toContain('Thư mục lưu');

    const batchSrc = readSrc('./batch/MuseBatch.tsx');
    expect(batchSrc).toContain('value={draft.prompt}');
    expect(batchSrc).toContain('outputDir = draft.outputDir');
    expect(batchSrc).toContain('<TemplateField');
    expect(batchSrc).toContain('onInsert={(text) =>');
    expect(batchSrc).toContain('muse-batch-progress');
    expect(batchSrc).toContain('<MuseMediaLightbox');
    expect(batchSrc).toContain('api.updateMuseJobsConfig(');
    expect(batchSrc).toContain('onUserChange={(dir) => syncConfig({ outputDir: dir })}');
    expect(batchSrc).toContain('onChange={(references) => updateAndSyncRefs({ references })}');
    expect(batchSrc).toContain('update={updateAndSyncRefs}');

    const folderSrc = readSrc('../../components/batch/OutputFolderField.tsx');
    expect(folderSrc).toContain('onUserChange?: (dir: string) => void;');
    expect(folderSrc).toContain('onUserChange?.(res.path);');
  });

  it('wires kind and boardFrames into MuseSinglePanel PromptCheckHint and renders MuseSingleWorkspace filter tabs', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter>
        <MuseSinglePanel kind={MuseJobKind.Image} />
      </MemoryRouter>,
    );
    expect(html).toContain('Tỉ lệ');

    const singleSrc = readSrc('./single/MuseSingle.tsx');
    expect(singleSrc).toContain('kind={kind}');
    expect(singleSrc).toContain('boardFrames={kind === MuseJobKind.Video ? draft.boardFrames : 0}');
    expect(singleSrc).toContain('muse-single-toolbar');
    expect(singleSrc).toContain('<MuseMediaLightbox');

    const extrasSrc = readSrc('./shared/StudioExtras.tsx');
    expect(extrasSrc).toContain('api\n        .checkMusePrompt(');
    expect(extrasSrc).toContain('kind, frames');

    const panelSrc = readSrc('./shared/MusePanel.tsx');
    expect(panelSrc).toContain('if (ready > 0) return null;');
    expect(panelSrc).not.toContain('studio-status is-ready');
    expect(panelSrc).not.toContain('{ready} tài khoản Muse sẵn sàng');

    const confirmSrc = readSrc('../../components/ui/ConfirmModal/ConfirmModal.tsx');
    expect(confirmSrc).toContain("import './ConfirmModal.css'");
    expect(confirmSrc).toContain('confirm-dialog-actions');
  });

  it('renders search & sort toolbar in MuseLibraryPage and separates AssetCard + TemplateSection cleanly', () => {
    const libHtml = renderToStaticMarkup(
      <MemoryRouter>
        <MuseLibraryPage />
      </MemoryRouter>,
    );
    expect(libHtml).toContain('Tìm kiếm trong thư viện');
    expect(libHtml).toContain('Sắp xếp thư viện');

    const asset = {
      id: 'a1',
      project: 'OLIVA',
      type: MuseAssetType.Char,
      tag: 'jimena',
      name: 'Jimena',
      role: MuseRefRole.Character,
      lock: 'nốt ruồi má trái',
      images: ['/muse-vault/a1.png'],
      items: [
        { id: 'i1', url: '/muse-vault/a1.png', rejected: false },
        { id: 'i2', url: '/muse-vault/a2.png', rejected: true },
      ],
      createdAt: '',
    } as unknown as MuseAsset;

    const cardHtml = renderToStaticMarkup(
      <AssetCard
        asset={asset}
        selected={false}
        hasSelection={false}
        onToggleSelect={() => {}}
        onEdit={() => {}}
        onDelete={() => {}}
      />,
    );
    expect(cardHtml).toContain('@jimena');
    expect(cardHtml).toContain('1/2 ảnh');
    expect(cardHtml).toContain('Khoá: nốt ruồi má trái');

    const tplHtml = renderToStaticMarkup(
      <TemplateSection
        templates={[
          { id: 't1', name: 'Đèn neon', text: 'phố đêm', createdAt: '' } as MusePromptTemplate,
        ]}
        onSave={async () => true}
        onRemove={async () => true}
      />,
    );
    expect(tplHtml).toContain('Đèn neon');
    expect(tplHtml).toContain('aria-label="Chép nội dung"');

    const lightboxSrc = readSrc('./shared/MuseMediaLightbox.tsx');
    expect(lightboxSrc).toContain('MuseMediaLightbox');
    expect(lightboxSrc).toContain('Chép prompt');
    expect(lightboxSrc).toContain('Lưu vào Kho');
    expect(lightboxSrc).toContain('Mở thư mục');
    expect(lightboxSrc).toContain('Lưu file');
    void MuseMediaLightbox;
  });
});
