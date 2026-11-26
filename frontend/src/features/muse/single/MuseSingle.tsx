import { memo, useCallback, useMemo, useState } from 'react';
import {
  AlertCircle,
  BookmarkPlus,
  CornerUpLeft,
  Download,
  FolderOpen,
  Maximize2,
  Play,
  RotateCcw,
  Sparkles,
  Square,
  Trash2,
} from 'lucide-react';
import { PromptBar, QuantityField } from '../../../components/studio';
import {
  Button,
  Icon,
  IconAction,
  RevealList,
  Spinner,
  VideoThumbnail,
} from '../../../components/ui';
import { LIST_REVEAL_STEP, MUSE_SINGLE_MAX_VARIANTS } from '../../../constants';
import { MuseJobKind, MuseJobMode, MuseJobStatus } from '../../../types';
import type { MuseJob } from '../../../types';
import { SaveToVaultDialog, type VaultImportSource } from '../assets/SaveToVaultDialog';
import { isActive, isEnded, jobNote, jobStatusLabels, jobsOf } from '../shared/format';
import { useMuseDraft } from '../shared/museDraft';
import { MuseMediaLightbox, type MuseLightboxItem } from '../shared/MuseMediaLightbox';
import { FormatFields, MusePanel, ReferencesField } from '../shared/MusePanel';
import { MuseNotices } from '../shared/MuseNotices';
import {
  activeExtras,
  ExtrasGroup,
  FixedAssetsField,
  ProjectScopeField,
  PromptCheckHint,
  referenceFileSummary,
  StoryboardField,
  TemplateField,
  useMuseStudio,
} from '../shared/StudioExtras';
import { useMuseStore, useVisibleMuseJobs } from '../shared/museStore';
import { useMuseActions, useMuseSubmit } from '../shared/useMuseActions';
import type { JobActions } from '../shared/useMuseActions';

type SingleFilter = 'all' | 'active' | 'succeeded' | 'failed';

const nouns: Record<MuseJobKind, string> = {
  [MuseJobKind.Image]: 'ảnh',
  [MuseJobKind.Video]: 'video',
};

const placeholders: Record<MuseJobKind, string> = {
  [MuseJobKind.Image]: 'Chân dung cô gái dưới mưa đèn neon, ảnh dọc',
  [MuseJobKind.Video]: 'Một chú mèo chạy trên bãi biển lúc hoàng hôn, video dọc 9:16',
};

function jobFormatSummary(job: MuseJob): string {
  const parts = [job.kind === MuseJobKind.Video ? 'Video' : 'Ảnh'];
  if (job.kind === MuseJobKind.Image && (job.boardFrames ?? 0) > 0) {
    parts[0] = `Storyboard ${job.boardFrames} khung`;
  }
  if (job.ratio) parts.push(job.ratio);
  if (job.kind === MuseJobKind.Video && job.duration) parts.push(`${job.duration}s`);
  return parts.join(' · ');
}

function singleLightboxItems(job: MuseJob): MuseLightboxItem[] {
  const meta: string[] = [];
  if (job.ratio) meta.push(job.ratio);
  if (job.kind === MuseJobKind.Video && job.duration) meta.push(`${job.duration} giây`);
  if (job.kind === MuseJobKind.Image && (job.boardFrames ?? 0) > 0) {
    meta.push(`Storyboard ${job.boardFrames} khung`);
  }
  const note = jobNote(job) || undefined;
  return job.files.map((url) => ({
    url,
    kind: job.kind,
    prompt: job.prompt,
    title: job.label,
    meta,
    note,
  }));
}

/** Đơn tools: count, references and file name for the next prompt (each result is its own Muse chat). */
export function MuseSinglePanel({ kind }: { kind: MuseJobKind }) {
  const options = useMuseStore((s) => s.options);
  const { assets } = useMuseStudio();
  const [draft, update] = useMuseDraft(kind, MuseJobMode.Single);
  const { report } = useMuseActions();
  return (
    <MusePanel>
      <QuantityField
        value={draft.variants}
        max={MUSE_SINGLE_MAX_VARIANTS}
        onChange={(variants) => update({ variants })}
        ariaLabel="Số kết quả mỗi lần tạo"
      />
      <ReferencesField
        references={draft.references}
        options={options}
        onChange={(references) => update({ references })}
        onError={report}
      />
      <FormatFields
        kind={kind}
        options={options}
        ratio={draft.ratio}
        duration={draft.duration}
        onChange={update}
      />
      <PromptCheckHint
        kind={kind}
        prompt={draft.prompt}
        project={draft.project}
        assetIds={draft.assetIds}
        boardFrames={kind === MuseJobKind.Video ? draft.boardFrames : 0}
      />
      <ExtrasGroup
        active={activeExtras(draft)}
        summary={referenceFileSummary(assets, draft.assetIds)}
      >
        <ProjectScopeField draft={draft} update={update} />
        <FixedAssetsField draft={draft} update={update} />
        {kind === MuseJobKind.Video && <StoryboardField draft={draft} update={update} />}
        <TemplateField
          onInsert={(text) =>
            update({ prompt: draft.prompt.trim() ? `${draft.prompt}\n${text}` : text })
          }
        />
      </ExtrasGroup>
    </MusePanel>
  );
}

/**
 * Đơn workspace: the results of this kind's single jobs, newest first, and the
 * prompt bar. Each prompt is one job in its own Muse chat, run by the shared queue.
 */
export function MuseSingleWorkspace({ kind }: { kind: MuseJobKind }) {
  const allJobs = useVisibleMuseJobs();
  const loaded = useMuseStore((s) => s.loaded);
  const [draft, update] = useMuseDraft(kind, MuseJobMode.Single);
  const { submit, submitting } = useMuseSubmit(kind, MuseJobMode.Single);
  const { jobActions } = useMuseActions();
  const [filter, setFilter] = useState<SingleFilter>('all');
  const [lightbox, setLightbox] = useState<{ items: MuseLightboxItem[]; index: number } | null>(
    null,
  );
  const [vaultSources, setVaultSources] = useState<VaultImportSource[] | null>(null);
  const jobs = useMemo(() => jobsOf(allJobs, kind, MuseJobMode.Single), [allJobs, kind]);
  const generating = jobs.some(isActive);

  const counts = useMemo(
    () => ({
      all: jobs.length,
      active: jobs.filter(isActive).length,
      succeeded: jobs.filter((j) => j.status === MuseJobStatus.Succeeded).length,
      failed: jobs.filter(isEnded).length,
    }),
    [jobs],
  );

  const shown = useMemo(() => {
    switch (filter) {
      case 'active':
        return jobs.filter(isActive);
      case 'succeeded':
        return jobs.filter((j) => j.status === MuseJobStatus.Succeeded);
      case 'failed':
        return jobs.filter(isEnded);
      default:
        return jobs;
    }
  }, [jobs, filter]);

  const openPreview = useCallback((job: MuseJob, url: string) => {
    const items = singleLightboxItems(job);
    const index = Math.max(
      0,
      items.findIndex((it) => it.url === url),
    );
    setLightbox({ items, index });
  }, []);

  const reusePrompt = useCallback(
    (job: MuseJob) => {
      update({ prompt: job.prompt });
    },
    [update],
  );

  const openSaveToVault = useCallback((job: MuseJob, url: string) => {
    setVaultSources([{ localUrl: url, suggestedPrompt: job.prompt, suggestedName: job.label }]);
  }, []);

  const generate = async () => {
    const prompt = draft.prompt.trim();
    if (!prompt) return;
    if (await submit([prompt], draft.variants, false, draft)) update({ prompt: '' });
  };

  return (
    <div className="muse-workspace">
      <MuseNotices />
      {jobs.length > 0 && (
        <div className="muse-single-toolbar">
          <div className="muse-single-filters" role="tablist" aria-label="Lọc kết quả">
            {(
              [
                ['all', 'Tất cả', counts.all],
                ['active', 'Đang chạy', counts.active],
                ['succeeded', 'Xong', counts.succeeded],
                ['failed', 'Lỗi / Huỷ', counts.failed],
              ] as const
            ).map(([key, label, count]) => (
              <button
                key={key}
                type="button"
                role="tab"
                aria-selected={filter === key}
                className={`muse-filter-pill${filter === key ? ' is-on' : ''}`}
                onClick={() => setFilter(key)}
              >
                <span>{label}</span>
                <span>({count})</span>
              </button>
            ))}
          </div>
          <span className="muse-single-stats">
            {counts.succeeded}/{counts.all} hoàn thành
            {counts.active > 0 ? ` · ${counts.active} đang xử lý` : ''}
          </span>
        </div>
      )}
      <div className="muse-workspace-body">
        {jobs.length === 0 ? (
          <div className="muse-gallery-empty">
            <Icon icon={Sparkles} size="display" />
            <strong>{loaded ? `Tạo ${nouns[kind]} với Muse` : 'Đang tải…'}</strong>
            {loaded && <span>Mô tả {nouns[kind]} bạn muốn ở ô bên dưới rồi nhấn Enter.</span>}
          </div>
        ) : shown.length === 0 ? (
          <div className="muse-gallery-empty">
            <strong>Không có kết quả nào ở mục này</strong>
            <Button size="sm" variant="secondary" onClick={() => setFilter('all')}>
              Hiện tất cả ({jobs.length})
            </Button>
          </div>
        ) : (
          <RevealList
            className="muse-gallery"
            items={shown}
            keyOf={(job) => job.id}
            step={LIST_REVEAL_STEP}
            resetKey={`${kind}:${filter}`}
            render={(job) => (
              <SingleTile
                job={job}
                actions={jobActions}
                onPreview={openPreview}
                onReuse={reusePrompt}
                onSaveToVault={kind === MuseJobKind.Image ? openSaveToVault : undefined}
              />
            )}
          />
        )}
      </div>
      <PromptBar
        prompt={draft.prompt}
        onPromptChange={(prompt) => update({ prompt })}
        onGenerate={() => void generate()}
        placeholder={placeholders[kind]}
        generating={generating}
        submitting={submitting}
      />
      {vaultSources && (
        <SaveToVaultDialog
          sources={vaultSources}
          defaultProject={draft.project}
          onClose={() => setVaultSources(null)}
        />
      )}
      {lightbox && (
        <MuseMediaLightbox
          items={lightbox.items}
          index={lightbox.index}
          onIndexChange={(nextIndex) =>
            setLightbox((prev) => (prev ? { ...prev, index: nextIndex } : null))
          }
          onClose={() => setLightbox(null)}
          onSave={(item) => jobActions.save(item.url)}
          onReveal={(item) => jobActions.reveal(item.url)}
          onReusePrompt={(item) => update({ prompt: item.prompt })}
          onSaveToVault={(item) => {
            if (item.kind !== MuseJobKind.Image) return;
            setLightbox(null);
            setVaultSources([
              { localUrl: item.url, suggestedPrompt: item.prompt, suggestedName: item.title },
            ]);
          }}
        />
      )}
    </div>
  );
}

/** One single job: its files, or its progress or error, with actions cleanly separated. */
export const SingleTile = memo(function SingleTile({
  job,
  actions,
  onPreview,
  onReuse,
  onSaveToVault,
}: {
  job: MuseJob;
  actions: JobActions;
  onPreview: (job: MuseJob, url: string) => void;
  onReuse: (job: MuseJob) => void;
  onSaveToVault?: (job: MuseJob, url: string) => void;
}) {
  const active = isActive(job);
  const ended = isEnded(job);
  const busy = job.status === MuseJobStatus.Processing;
  const waitingBoard = Boolean(job.after && (!job.frames || job.frames.length === 0));
  const note = jobNote(job);
  const format = jobFormatSummary(job);

  const statusText =
    job.status === MuseJobStatus.Queued && job.paused
      ? job.review && (job.frames?.length ?? 0) > 0
        ? job.step || 'Tạm dừng'
        : 'Tạm dừng'
      : active
        ? job.step || jobStatusLabels[job.status]
        : job.error || jobStatusLabels[job.status];

  return (
    <div className={`muse-tile muse-tile-${job.status}`}>
      <div className="muse-tile-media">
        <span className="muse-tile-format-pill">{format}</span>
        {job.files.length > 0 ? (
          <div className={`muse-tile-files muse-tile-files-${Math.min(job.files.length, 4)}`}>
            {job.files.map((url) => (
              <div key={url} className="muse-tile-file">
                <button
                  type="button"
                  className="muse-tile-open"
                  aria-label="Xem lớn"
                  onClick={() => onPreview(job, url)}
                >
                  {job.kind === MuseJobKind.Video ? (
                    <VideoThumbnail src={url} />
                  ) : (
                    <img src={url} alt="" loading="lazy" decoding="async" />
                  )}
                </button>
                <div className="muse-tile-overlay">
                  <IconAction
                    icon={Maximize2}
                    label="Xem lớn"
                    onClick={() => onPreview(job, url)}
                  />
                  {onSaveToVault && job.kind === MuseJobKind.Image && (
                    <IconAction
                      icon={BookmarkPlus}
                      label="Lưu vào Kho Tư liệu"
                      onClick={() => onSaveToVault(job, url)}
                    />
                  )}
                  <IconAction icon={Download} label="Lưu" onClick={() => actions.save(url)} />
                  <IconAction
                    icon={FolderOpen}
                    label="Mở thư mục"
                    onClick={() => actions.reveal(url)}
                  />
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="muse-tile-state" role={ended ? 'alert' : 'status'}>
            {active ? <Spinner /> : <Icon icon={AlertCircle} size="xl" />}
            <span className="muse-tile-step-pill">{jobStatusLabels[job.status]}</span>
            <span>{statusText}</span>
          </div>
        )}
      </div>
      {ended && job.files.length > 0 && job.error && (
        <p className="muse-tile-error" role="alert">
          {job.error}
        </p>
      )}
      <div className="muse-tile-foot">
        <p className="muse-tile-prompt" title={note || job.prompt}>
          {note && <Icon icon={AlertCircle} size="sm" className="muse-tile-note" />}
          {job.prompt}
        </p>
        {note && (
          <div className="muse-tile-note-line" title={note}>
            <Icon icon={AlertCircle} size="sm" />
            <span>{note}</span>
          </div>
        )}
        <div className="muse-tile-foot-row">
          <span className="muse-tile-meta">
            {job.files.length > 1 ? `${job.files.length} kết quả` : format}
          </span>
          <div className="muse-tile-actions">
            {job.held && !waitingBoard && (
              <IconAction
                icon={Play}
                label="Bắt đầu"
                variant="primary"
                onClick={() => actions.run(job.id)}
              />
            )}
            {active && !job.held && (
              <IconAction icon={Square} label="Huỷ" onClick={() => actions.cancel(job.id)} />
            )}
            {ended && (
              <IconAction icon={RotateCcw} label="Chạy lại" onClick={() => actions.retry(job)} />
            )}
            <IconAction icon={CornerUpLeft} label="Dùng lại prompt" onClick={() => onReuse(job)} />
            {!busy && (
              <IconAction icon={Trash2} label="Xoá" onClick={() => actions.remove(job.id)} />
            )}
          </div>
        </div>
      </div>
    </div>
  );
});
