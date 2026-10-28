import { memo, useMemo, useState } from 'react';
import {
  AlertCircle,
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
import { Icon, IconAction, Modal, RevealList, Spinner } from '../../../components/ui';
import { LIST_REVEAL_STEP, MUSE_SINGLE_MAX_VARIANTS } from '../../../constants';
import { MuseJobKind, MuseJobMode, MuseJobStatus } from '../../../types';
import type { MuseJob } from '../../../types';
import { isActive, isEnded, jobNote, jobStatusLabels, jobsOf } from '../shared/format';
import { useMuseDraft } from '../shared/museDraft';
import { FormatFields, MusePanel, ReferencesField } from '../shared/MusePanel';
import { MuseNotices } from '../shared/MuseNotices';
import {
  activeExtras,
  ExtrasGroup,
  FixedAssetsField,
  PromptCheckHint,
  SourceVideoField,
  StoryboardField,
  TemplateField,
} from '../shared/StudioExtras';
import { useMuseStore, useVisibleMuseJobs } from '../shared/museStore';
import { useMuseActions, useMuseSubmit } from '../shared/useMuseActions';
import type { JobActions } from '../shared/useMuseActions';

const nouns: Record<MuseJobKind, string> = {
  [MuseJobKind.Image]: 'ảnh',
  [MuseJobKind.Video]: 'video',
};

const placeholders: Record<MuseJobKind, string> = {
  [MuseJobKind.Image]: 'Chân dung cô gái dưới mưa đèn neon, ảnh dọc',
  [MuseJobKind.Video]: 'Một chú mèo chạy trên bãi biển lúc hoàng hôn, video dọc 9:16',
};

/** Đơn tools: count, references and file name for the next prompt (each result is its own Muse chat). */
export function MuseSinglePanel({ kind }: { kind: MuseJobKind }) {
  const options = useMuseStore((s) => s.options);
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
      <ExtrasGroup active={activeExtras(draft)}>
        <FixedAssetsField draft={draft} update={update} />
        <PromptCheckHint prompt={draft.prompt} assetIds={draft.assetIds} />
        {kind === MuseJobKind.Video && <StoryboardField draft={draft} update={update} />}
        <SourceVideoField draft={draft} update={update} />
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
  const submit = useMuseSubmit(kind, MuseJobMode.Single);
  const { jobActions } = useMuseActions();
  const [preview, setPreview] = useState<{
    url: string;
    prompt: string;
    kind: MuseJobKind;
  } | null>(null);
  const jobs = useMemo(() => jobsOf(allJobs, kind, MuseJobMode.Single), [allJobs, kind]);
  const generating = jobs.some(isActive);

  const generate = async () => {
    const prompt = draft.prompt.trim();
    if (!prompt) return;
    if (await submit([prompt], draft.variants, false, draft)) update({ prompt: '' });
  };

  return (
    <div className="muse-workspace">
      <MuseNotices />
      <div className="muse-workspace-body">
        {jobs.length === 0 ? (
          <div className="muse-gallery-empty">
            <Icon icon={Sparkles} size="display" />
            <strong>{loaded ? `Tạo ${nouns[kind]} với Muse` : 'Đang tải…'}</strong>
            {loaded && <span>Mô tả {nouns[kind]} bạn muốn ở ô bên dưới rồi nhấn Enter.</span>}
          </div>
        ) : (
          <RevealList
            className="muse-gallery"
            items={jobs}
            keyOf={(job) => job.id}
            step={LIST_REVEAL_STEP}
            resetKey={kind}
            render={(job) => (
              <SingleTile
                job={job}
                actions={jobActions}
                // A storyboard's frames are images even on the Video screen.
                onPreview={(url) => setPreview({ url, prompt: job.prompt, kind: job.kind })}
                onReuse={() => update({ prompt: job.prompt })}
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
      />
      {preview && (
        <Modal variant="lightbox" label={preview.prompt} onClose={() => setPreview(null)}>
          {preview.kind === MuseJobKind.Video ? (
            <video src={preview.url} controls autoPlay preload="metadata" />
          ) : (
            <img src={preview.url} alt={preview.prompt} />
          )}
          <span className="modal-caption">{preview.prompt}</span>
        </Modal>
      )}
    </div>
  );
}

/** One single job: its files, or its progress or error, with actions on hover. */
const SingleTile = memo(function SingleTile({
  job,
  actions,
  onPreview,
  onReuse,
}: {
  job: MuseJob;
  actions: JobActions;
  onPreview: (url: string) => void;
  onReuse: () => void;
}) {
  const active = isActive(job);
  const ended = isEnded(job);
  const busy = job.status === MuseJobStatus.Processing;
  return (
    <div className={`muse-tile muse-tile-${job.status}`}>
      <div className="muse-tile-media">
        {job.files.length > 0 ? (
          <div className={`muse-tile-files muse-tile-files-${Math.min(job.files.length, 4)}`}>
            {job.files.map((url) => (
              <div key={url} className="muse-tile-file">
                <button
                  type="button"
                  className="muse-tile-open"
                  aria-label="Xem lớn"
                  onClick={() => onPreview(url)}
                >
                  {job.kind === MuseJobKind.Video ? (
                    <video src={url} preload="metadata" muted />
                  ) : (
                    <img src={url} alt="" loading="lazy" decoding="async" />
                  )}
                </button>
                <div className="muse-tile-overlay">
                  <IconAction icon={Maximize2} label="Xem lớn" onClick={() => onPreview(url)} />
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
            <span>
              {job.held
                ? 'Tạm dừng'
                : active
                  ? job.step || jobStatusLabels[job.status]
                  : job.error || jobStatusLabels[job.status]}
            </span>
          </div>
        )}
      </div>
      <div className="muse-tile-foot">
        <p className="muse-tile-prompt" title={jobNote(job) || job.prompt}>
          {jobNote(job) && <Icon icon={AlertCircle} size="sm" className="muse-tile-note" />}
          {job.prompt}
        </p>
        <div className="muse-tile-actions">
          {job.held && (
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
          <IconAction icon={CornerUpLeft} label="Dùng lại prompt" onClick={onReuse} />
          {!busy && <IconAction icon={Trash2} label="Xoá" onClick={() => actions.remove(job.id)} />}
        </div>
      </div>
    </div>
  );
});
