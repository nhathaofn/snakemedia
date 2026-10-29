import { thumbUrl } from '../../../services';
import { memo } from 'react';
import { Play } from 'lucide-react';
import { QueuePrompt, QueueProgress } from '../../../components/batch';
import type { QueueTone } from '../../../components/batch';
import { Icon } from '../../../components/ui';
import { MuseJobKind, MuseJobStatus } from '../../../types';
import type { MuseJob } from '../../../types';
import { isEnded, jobNote, jobStatusLabels } from '../shared/format';
import type { JobActions } from '../shared/useMuseActions';

interface JobRowProps {
  job: MuseJob;
  /** The job's number: its prompt's place in its batch, as in its file names. */
  number: number;
  selected: boolean;
  accountName: string;
  actions: JobActions;
  onToggle: (id: string) => void;
  onPreview: (job: MuseJob, url: string) => void;
}

/** References shown in the row; the rest are counted. */
const SHOWN_REFERENCES = 2;

const tones: Record<MuseJobStatus, QueueTone> = {
  [MuseJobStatus.Queued]: 'queued',
  [MuseJobStatus.Processing]: 'running',
  [MuseJobStatus.Succeeded]: 'succeeded',
  [MuseJobStatus.Failed]: 'failed',
  [MuseJobStatus.Cancelled]: 'cancelled',
};

/** One batch job as a queue table row: number, prompt, references, results, progress. */
export const JobRow = memo(function JobRow({
  job,
  number,
  selected,
  accountName,
  actions,
  onToggle,
  onPreview,
}: JobRowProps) {
  const ended = isEnded(job);
  const busy = job.status === MuseJobStatus.Processing;
  const shownRefs = job.references.slice(0, SHOWN_REFERENCES);
  const moreRefs = job.references.length - shownRefs.length;
  // What the job asked Muse for besides the prompt: "9:16 · 8s".
  const format = [job.ratio, job.duration ? `${job.duration}s` : ''].filter(Boolean).join(' · ');
  const saved = job.savedFiles?.[0] ?? job.files[0];

  return (
    <div className={`queue-table-row${selected ? ' is-selected' : ''}`} role="row">
      <span className="queue-table-index" role="cell">
        {number}
        <input
          type="checkbox"
          aria-label={`Chọn tác vụ ${number}`}
          checked={selected}
          onChange={() => onToggle(job.id)}
        />
      </span>

      <QueuePrompt
        prompt={job.prompt}
        editable={!busy}
        saveLabel={job.status === MuseJobStatus.Queued ? 'Lưu' : 'Lưu & chạy lại'}
        onSave={(prompt) => actions.editPrompt(job.id, prompt)}
      />

      <div className="queue-table-refs" role="cell">
        {shownRefs.length > 0 ? (
          <>
            {shownRefs.map((url) => (
              <img
                key={url}
                className="queue-ref-thumb"
                src={thumbUrl(url)}
                alt=""
                loading="lazy"
                decoding="async"
              />
            ))}
            {moreRefs > 0 && (
              <span className="queue-ref-empty" title={`${job.references.length} ảnh tham chiếu`}>
                +{moreRefs}
              </span>
            )}
          </>
        ) : (
          <span className="queue-ref-empty">Chưa có</span>
        )}
      </div>

      <div className="queue-table-results" role="cell">
        {job.files.length > 0 ? (
          job.files.map((url) => (
            <button
              type="button"
              key={url}
              className="queue-result"
              title="Xem lớn"
              aria-label="Xem kết quả"
              onClick={() => onPreview(job, url)}
            >
              {job.kind === MuseJobKind.Video ? (
                <>
                  <video className="queue-result-thumb" src={url} preload="metadata" muted />
                  <span className="queue-result-play" aria-hidden="true">
                    <Icon icon={Play} size="sm" />
                  </span>
                </>
              ) : (
                <img
                  className="queue-result-thumb"
                  src={thumbUrl(url)}
                  alt=""
                  loading="lazy"
                  decoding="async"
                />
              )}
            </button>
          ))
        ) : (
          <span className="queue-muted">—</span>
        )}
      </div>

      <QueueProgress
        tone={job.held ? 'held' : tones[job.status]}
        label={job.held ? 'Tạm dừng' : jobStatusLabels[job.status]}
        title={[accountName, format].filter(Boolean).join(' · ') || undefined}
        detail={busy ? job.step : format || undefined}
        error={ended ? job.error : undefined}
        warning={jobNote(job) || undefined}
        savedPath={job.status === MuseJobStatus.Succeeded ? saved : undefined}
        onReveal={saved ? () => actions.reveal(saved) : undefined}
        action={
          busy
            ? { label: 'Hủy', onClick: () => actions.cancel(job.id) }
            : ended
              ? { label: 'Chạy lại', onClick: () => actions.retry(job) }
              : undefined
        }
      />
    </div>
  );
});
