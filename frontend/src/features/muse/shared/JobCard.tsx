import { memo, useState } from 'react';
import { Download, Edit2, FolderOpen, Play, RotateCcw, Square, Trash2 } from 'lucide-react';
import { Button, Card, Icon } from '../../../components/ui';
import { MuseJobKind, MuseJobStatus } from '../../../types';
import type { MuseJob } from '../../../types';
import { jobStatusLabels } from './format';

/** What a job card can do; one stable object shared by every card. */
export interface JobActions {
  toggleSelect: (id: string) => void;
  cancel: (id: string) => void;
  retry: (id: string) => void;
  run: (id: string) => void;
  editPrompt: (id: string, prompt: string) => void;
  remove: (id: string) => void;
  save: (url: string) => void;
  reveal: (url: string) => void;
}

interface JobCardProps {
  job: MuseJob;
  selected: boolean;
  accountName: string;
  actions: JobActions;
}

/** One Muse job: status, prompt (editable when idle), its files and actions. */
export const JobCard = memo(function JobCard({
  job,
  selected,
  accountName,
  actions,
}: JobCardProps) {
  const [editing, setEditing] = useState(false);
  const [prompt, setPrompt] = useState(job.prompt);
  const active = job.status === MuseJobStatus.Queued || job.status === MuseJobStatus.Processing;
  const ended = job.status === MuseJobStatus.Failed || job.status === MuseJobStatus.Cancelled;

  return (
    <Card className={`muse-job ${job.held ? 'muse-job-held' : ''}`}>
      <div className="muse-job-head">
        <label className="muse-check">
          <input type="checkbox" checked={selected} onChange={() => actions.toggleSelect(job.id)} />
        </label>
        <span className={`muse-badge muse-job-${job.status}`}>{jobStatusLabels[job.status]}</span>
        {job.held && <span className="muse-tag muse-tag-warning">Tạm dừng</span>}
        {job.label && <span className="muse-tag">{job.label}</span>}
        {job.references.length > 0 && (
          <span className="muse-tag">{job.references.length} ảnh tham chiếu</span>
        )}
        {accountName && <span className="muse-tag">{accountName}</span>}
        <span className="muse-job-time">{new Date(job.createdAt).toLocaleString('vi-VN')}</span>
      </div>

      {editing ? (
        <form
          className="muse-edit"
          onSubmit={(e) => {
            e.preventDefault();
            if (!prompt.trim()) return;
            actions.editPrompt(job.id, prompt);
            setEditing(false);
          }}
        >
          <textarea
            className="muse-input muse-textarea"
            rows={3}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
          <div className="muse-actions">
            <Button size="sm" variant="primary" type="submit">
              Lưu & chạy lại
            </Button>
            <Button size="sm" variant="ghost" type="button" onClick={() => setEditing(false)}>
              Huỷ
            </Button>
          </div>
        </form>
      ) : (
        <div className="muse-job-prompt" title={job.prompt}>
          {job.prompt}
        </div>
      )}

      {job.step && active && <div className="muse-note">{job.step}</div>}
      {job.error && (
        <div className={`muse-note ${ended ? 'muse-note-danger' : ''}`}>{job.error}</div>
      )}

      {job.files.length > 0 && (
        <div className="muse-results">
          {job.files.map((url) => (
            <div key={url} className="muse-result">
              {job.kind === MuseJobKind.Video ? (
                <video src={url} controls preload="none" />
              ) : (
                <img src={url} alt="" loading="lazy" decoding="async" />
              )}
              <div className="muse-actions">
                <Button size="sm" variant="ghost" onClick={() => actions.save(url)}>
                  <Icon icon={Download} size="sm" />
                  <span>Lưu</span>
                </Button>
                <Button size="sm" variant="ghost" onClick={() => actions.reveal(url)}>
                  <Icon icon={FolderOpen} size="sm" />
                  <span>Mở thư mục</span>
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="muse-actions muse-card-actions">
        {job.held && (
          <Button size="sm" variant="primary" onClick={() => actions.run(job.id)}>
            <Icon icon={Play} size="sm" />
            <span>Bắt đầu</span>
          </Button>
        )}
        {active && !job.held && (
          <Button size="sm" onClick={() => actions.cancel(job.id)}>
            <Icon icon={Square} size="sm" />
            <span>Huỷ</span>
          </Button>
        )}
        {ended && (
          <Button size="sm" onClick={() => actions.retry(job.id)}>
            <Icon icon={RotateCcw} size="sm" />
            <span>Chạy lại</span>
          </Button>
        )}
        {job.status !== MuseJobStatus.Processing && !editing && (
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
            <Icon icon={Edit2} size="sm" />
            <span>Sửa prompt</span>
          </Button>
        )}
        {job.status !== MuseJobStatus.Processing && (
          <Button size="sm" variant="ghost" onClick={() => actions.remove(job.id)}>
            <Icon icon={Trash2} size="sm" />
            <span>Xoá</span>
          </Button>
        )}
      </div>
    </Card>
  );
});
