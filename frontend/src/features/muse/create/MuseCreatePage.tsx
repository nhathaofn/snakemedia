import React, { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  CheckSquare,
  Clapperboard,
  Download,
  Edit2,
  FolderOpen,
  Image,
  ImagePlus,
  Pause,
  Play,
  RotateCcw,
  Send,
  Square,
  Trash2,
  Video,
  X,
} from 'lucide-react';
import '../shared/Muse.css';
import { Button, Card, Icon, SegmentedControl } from '../../../components/ui';
import { ROUTES } from '../../../routes/registry';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseJobKind, MuseJobRequest, MuseJobStatus } from '../../../types';
import type { MuseJob, MuseReference, StudioData } from '../../../types';
import { accountReady, isVideoUrl, jobStatusLabels, splitPrompts } from '../shared/format';
import { useMuseStore } from '../shared/museStore';
import { useMuseLive } from '../shared/useMuseLive';
import { useNow } from '../shared/useNow';

const kindOptions = [
  { value: MuseJobKind.Video, label: 'Video', icon: Clapperboard },
  { value: MuseJobKind.V2V, label: 'Video sang Video (V2V)', icon: Video },
  { value: MuseJobKind.Image, label: 'Ảnh', icon: Image },
] as const;

type Filter = 'all' | MuseJobStatus | 'held';

const filters: readonly { value: Filter; label: string }[] = [
  { value: 'all', label: 'Tất cả' },
  { value: MuseJobStatus.Processing, label: jobStatusLabels[MuseJobStatus.Processing] },
  { value: MuseJobStatus.Queued, label: jobStatusLabels[MuseJobStatus.Queued] },
  { value: 'held', label: 'Tạm dừng (Held)' },
  { value: MuseJobStatus.Succeeded, label: jobStatusLabels[MuseJobStatus.Succeeded] },
  { value: MuseJobStatus.Failed, label: jobStatusLabels[MuseJobStatus.Failed] },
];

export const MuseCreatePage: React.FC = () => {
  useMuseLive();
  const now = useNow();
  const navigate = useNavigate();
  const { jobs, accounts, options, loaded, loadError, setJob, dropJob, load } = useMuseStore();
  const addToast = useAppStore((s) => s.addToast);
  const [kind, setKind] = useState<MuseJobKind>(MuseJobKind.Video);
  const [ratio, setRatio] = useState('9:16');
  const [count, setCount] = useState(1);
  const [text, setText] = useState('');
  const [references, setReferences] = useState<MuseReference[]>([]);
  const [sourceVideo, setSourceVideo] = useState('');
  const [keepAudio, setKeepAudio] = useState(true);
  const [pairMode, setPairMode] = useState(false);
  const [studioData, setStudioData] = useState<StudioData | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [filter, setFilter] = useState<Filter>('all');
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  React.useEffect(() => {
    void (async () => {
      try {
        const s = unwrap(await api.getMuseStudio());
        setStudioData(s);
      } catch {
        // Studio optional
      }
    })();
  }, []);

  const safeJobs = useMemo(() => (Array.isArray(jobs) ? jobs : []), [jobs]);
  const safeAccounts = useMemo(() => (Array.isArray(accounts) ? accounts : []), [accounts]);

  const prompts = useMemo(() => splitPrompts(text), [text]);
  const readyAccounts = safeAccounts.filter((a) => accountReady(a, now)).length;

  const shown = useMemo(() => {
    if (filter === 'all') return safeJobs;
    if (filter === 'held') return safeJobs.filter((j) => j.held);
    return safeJobs.filter((job) => job.status === filter);
  }, [safeJobs, filter]);

  const accountName = (id?: string) => safeAccounts.find((a) => a.id === id)?.name ?? '';
  const maxReferences = options?.maxReferences ?? 0;

  const report = (cause: unknown) =>
    addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });

  const pickReferences = async () => {
    try {
      const picked = unwrap(await api.pickMuseReferences());
      setReferences((list) => [...list, ...picked].slice(0, maxReferences));
    } catch (cause) {
      report(cause);
    }
  };

  const submitWithHeld = async (held: boolean) => {
    if (submitting || prompts.length === 0) return;
    if (kind === MuseJobKind.V2V) {
      if (!sourceVideo) {
        addToast({ type: 'error', message: 'Tác vụ V2V bắt buộc chọn video nguồn' });
        return;
      }
      if (references.length === 0) {
        addToast({ type: 'error', message: 'Tác vụ V2V bắt buộc có ít nhất 1 ảnh tham chiếu' });
        return;
      }
    }
    setSubmitting(true);
    try {
      const created = unwrap(
        await api.submitMuseJobs(
          MuseJobRequest.createFrom({
            kind,
            prompts,
            ratio,
            count: kind === MuseJobKind.Image ? count : 1,
            references: references.map((r) => r.localUrl),
            sourceVideo: kind === MuseJobKind.V2V ? sourceVideo : '',
            keepAudio: kind === MuseJobKind.V2V ? keepAudio : false,
            pairMode,
            held,
          }),
        ),
      );
      created.forEach(setJob);
      setText('');
      addToast({
        type: 'success',
        message: held
          ? `Đã thêm ${created.length} tác vụ vào hàng đợi (tạm dừng).`
          : `Đã thêm ${created.length} tác vụ vào hàng đợi.`,
      });
    } catch (cause) {
      report(cause);
    } finally {
      setSubmitting(false);
    }
  };

  const act = async (action: () => Promise<void>) => {
    try {
      await action();
    } catch (cause) {
      report(cause);
    }
  };

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectAll = () => {
    if (selectedIds.size === shown.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(shown.map((j) => j.id)));
    }
  };

  const runSelected = async () => {
    const ids = Array.from(selectedIds);
    if (!ids.length) return;
    await act(async () => {
      const n = unwrap(await api.runMuseJobs(ids));
      addToast({ type: 'success', message: `Đã kích hoạt ${n} tác vụ.` });
      setSelectedIds(new Set());
      await load();
    });
  };

  const removeSelected = async () => {
    const ids = Array.from(selectedIds);
    if (!ids.length) return;
    await act(async () => {
      const n = unwrap(await api.removeMuseJobs(ids));
      addToast({ type: 'success', message: `Đã xoá ${n} tác vụ.` });
      setSelectedIds(new Set());
      await load();
    });
  };

  const holdQueued = async () => {
    await act(async () => {
      const n = unwrap(await api.holdMuseQueued());
      addToast({ type: 'success', message: `Đã tạm dừng ${n} tác vụ trong hàng đợi.` });
      await load();
    });
  };

  const retryFailed = async () => {
    await act(async () => {
      const n = unwrap(await api.retryMuseFailed());
      addToast({ type: 'success', message: `Đã chạy lại ${n} tác vụ lỗi.` });
      await load();
    });
  };

  const clearFinished = async () => {
    await act(async () => {
      const n = unwrap(await api.clearMuseFinished());
      addToast({ type: 'success', message: `Đã dọn dẹp ${n} tác vụ đã hoàn thành.` });
      await load();
    });
  };

  const openFolder = async () => {
    await act(async () => {
      unwrap(await api.openMuseOutputFolder());
    });
  };

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <h1 className="muse-title">Tạo với Muse</h1>
      </div>

      {loadError && (
        <div role="alert" className="muse-alert muse-alert-danger">
          {loadError}
        </div>
      )}
      {loaded && readyAccounts === 0 && (
        <div className="muse-alert muse-alert-warning">
          <span>
            Chưa có tài khoản Muse nào sẵn sàng. Tác vụ sẽ chờ tới khi có tài khoản đăng nhập.
          </span>
          <Button size="sm" onClick={() => navigate(ROUTES.muse.accounts)}>
            Mở Tài khoản Muse
          </Button>
        </div>
      )}

      <Card className="muse-panel">
        <form
          className="muse-form"
          onSubmit={(e) => {
            e.preventDefault();
            void submitWithHeld(false);
          }}
        >
          <div className="muse-form-row">
            <SegmentedControl
              options={kindOptions}
              value={kind}
              onChange={setKind}
              aria-label="Loại"
            />
            <label className="muse-inline-field">
              <span>Tỷ lệ</span>
              <select
                className="muse-input"
                value={ratio}
                onChange={(e) => setRatio(e.target.value)}
              >
                {options?.ratios.map((r) => (
                  <option key={r.value} value={r.value}>
                    {r.label}
                  </option>
                ))}
              </select>
            </label>
            {kind === MuseJobKind.Image && (
              <label className="muse-inline-field">
                <span>Số ảnh</span>
                <select
                  className="muse-input"
                  value={count}
                  onChange={(e) => setCount(Number(e.target.value))}
                >
                  {Array.from({ length: options?.maxImagesPerJob ?? 1 }, (_, i) => i + 1).map(
                    (n) => (
                      <option key={n} value={n}>
                        {n}
                      </option>
                    ),
                  )}
                </select>
              </label>
            )}
          </div>

          {kind === MuseJobKind.V2V && (
            <div className="muse-callout">
              <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
                <Button
                  type="button"
                  size="sm"
                  onClick={async () => {
                    try {
                      const res = unwrap(await api.pickVideo());
                      setSourceVideo(res.localUrl);
                    } catch (cause) {
                      report(cause);
                    }
                  }}
                >
                  <Icon icon={Clapperboard} size="sm" />
                  <span>{sourceVideo ? 'Đổi video nguồn' : 'Chọn video nguồn (bắt buộc)'}</span>
                </Button>
                {sourceVideo && (
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '0.5rem',
                      fontSize: '0.875rem',
                    }}
                  >
                    <span>
                      Video đã chọn: <code>{sourceVideo.split(/[\\/]/).pop()}</code>
                    </span>
                    <button
                      type="button"
                      className="muse-thumb-remove"
                      style={{ position: 'static' }}
                      onClick={() => setSourceVideo('')}
                    >
                      <Icon icon={X} size="sm" />
                    </button>
                  </div>
                )}
                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.375rem',
                    cursor: 'pointer',
                    fontSize: '0.875rem',
                  }}
                >
                  <input
                    type="checkbox"
                    checked={keepAudio}
                    onChange={(e) => setKeepAudio(e.target.checked)}
                  />
                  <span>Giữ 100% âm thanh gốc</span>
                </label>
              </div>

              <div
                style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}
              >
                <span style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)' }}>
                  Mẫu V2V:
                </span>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    const tpl = studioData?.promptTemplates?.find((t) => t.id === 'zoom-shirt');
                    if (tpl) setText(tpl.text);
                  }}
                >
                  Zoom áo (Push-in / Pull-out)
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    const tpl = studioData?.promptTemplates?.find((t) => t.id === 'restyle-shirt');
                    if (tpl) setText(tpl.text);
                  }}
                >
                  Thay áo (Restyle)
                </Button>
              </div>
            </div>
          )}

          {studioData && (studioData.assets?.length ?? 0) > 0 && (
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.375rem',
                flexWrap: 'wrap',
                fontSize: '0.8125rem',
              }}
            >
              <span style={{ color: 'var(--text-secondary)' }}>Chèn nhanh @tag:</span>
              {(studioData.assets ?? []).map((a) => (
                <button
                  key={a.id}
                  type="button"
                  className="muse-tag-chip"
                  title={a.name}
                  onClick={() => setText((prev) => (prev ? `${prev} @${a.tag}` : `@${a.tag}`))}
                >
                  @{a.tag}
                </button>
              ))}
            </div>
          )}

          <label className="muse-field">
            <span>Prompt — mỗi dòng là một tác vụ</span>
            <textarea
              className="muse-input muse-textarea"
              rows={6}
              value={text}
              maxLength={(options?.maxPromptLength ?? 8000) * (options?.maxPromptsPerRun ?? 1)}
              placeholder="Một cô gái đi dạo trên bãi biển lúc hoàng hôn"
              onChange={(e) => setText(e.target.value)}
            />
          </label>

          <div className="muse-references">
            {references.map((ref) => (
              <div key={ref.localUrl} className="muse-thumb" title={ref.fileName}>
                <img src={ref.localUrl} alt={ref.fileName} />
                <button
                  type="button"
                  className="muse-thumb-remove"
                  aria-label={`Bỏ ${ref.fileName}`}
                  onClick={() =>
                    setReferences((list) => list.filter((r) => r.localUrl !== ref.localUrl))
                  }
                >
                  <Icon icon={X} size="sm" />
                </button>
              </div>
            ))}
            {references.length < maxReferences && (
              <Button type="button" size="sm" onClick={pickReferences}>
                <Icon icon={ImagePlus} size="sm" />
                <span>Ảnh tham chiếu</span>
              </Button>
            )}
            {references.length > 1 && (
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.375rem',
                  cursor: 'pointer',
                  fontSize: '0.8125rem',
                  marginLeft: '0.5rem',
                }}
              >
                <input
                  type="checkbox"
                  checked={pairMode}
                  onChange={(e) => setPairMode(e.target.checked)}
                />
                <span>Ghép cặp ảnh tương ứng với từng prompt (Pair mode)</span>
              </label>
            )}
          </div>

          <div className="muse-actions">
            <Button
              type="submit"
              variant="primary"
              isLoading={submitting}
              disabled={prompts.length === 0}
            >
              <Icon icon={Send} />
              <span>
                {prompts.length > 1 ? `Chạy ${prompts.length} tác vụ` : 'Thêm vào hàng đợi'}
              </span>
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={submitting || prompts.length === 0}
              onClick={() => void submitWithHeld(true)}
            >
              <Icon icon={Pause} size="sm" />
              <span>Chỉ thêm vào hàng đợi (tạm dừng)</span>
            </Button>
          </div>
        </form>
      </Card>

      <div className="muse-header">
        <h2 className="muse-subtitle">Hàng đợi ({safeJobs.length})</h2>
        <SegmentedControl options={filters} value={filter} onChange={setFilter} aria-label="Lọc" />
      </div>

      {/* Toolbar điều khiển hàng đợi & thao tác hàng loạt */}
      <div
        className="muse-toolbar"
        style={{
          display: 'flex',
          gap: '8px',
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '12px 16px',
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border-subtle)',
          borderRadius: '8px',
        }}
      >
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          {shown.length > 0 && (
            <Button size="sm" variant="ghost" onClick={selectAll}>
              <Icon icon={CheckSquare} size="sm" />
              <span>{selectedIds.size === shown.length ? 'Bỏ chọn tất cả' : 'Chọn tất cả'}</span>
            </Button>
          )}
          {selectedIds.size > 0 && (
            <>
              <Button size="sm" variant="primary" onClick={runSelected}>
                <Icon icon={Play} size="sm" />
                <span>Chạy đã chọn ({selectedIds.size})</span>
              </Button>
              <Button size="sm" variant="danger" onClick={removeSelected}>
                <Icon icon={Trash2} size="sm" />
                <span>Xoá đã chọn ({selectedIds.size})</span>
              </Button>
            </>
          )}
        </div>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }}>
          <Button size="sm" variant="secondary" onClick={holdQueued}>
            <Icon icon={Pause} size="sm" />
            <span>Tạm dừng hàng đợi</span>
          </Button>
          <Button size="sm" variant="secondary" onClick={retryFailed}>
            <Icon icon={RotateCcw} size="sm" />
            <span>Chạy lại lỗi</span>
          </Button>
          <Button size="sm" variant="ghost" onClick={clearFinished}>
            <Icon icon={Trash2} size="sm" />
            <span>Xoá đã xong</span>
          </Button>
          <Button size="sm" variant="ghost" onClick={openFolder}>
            <Icon icon={FolderOpen} size="sm" />
            <span>Mở thư mục lưu</span>
          </Button>
        </div>
      </div>

      {shown.length === 0 ? (
        <div className="muse-empty">{loaded ? 'Không có tác vụ nào.' : 'Đang tải…'}</div>
      ) : (
        <div className="muse-jobs">
          {shown.map((job) => (
            <JobCard
              key={job.id}
              job={job}
              selected={selectedIds.has(job.id)}
              onToggleSelect={() => toggleSelect(job.id)}
              accountName={accountName(job.accountId)}
              onCancel={() => act(async () => setJob(unwrap(await api.cancelMuseJob(job.id))))}
              onRetry={() => act(async () => setJob(unwrap(await api.retryMuseJob(job.id))))}
              onRun={() =>
                act(async () => {
                  unwrap(await api.runMuseJobs([job.id]));
                  await load();
                })
              }
              onEditPrompt={(newPrompt) =>
                act(async () => {
                  const updated = unwrap(await api.editAndRetryMuseJob(job.id, newPrompt));
                  setJob(updated);
                  addToast({ type: 'success', message: 'Đã cập nhật prompt và đưa vào hàng đợi.' });
                })
              }
              onRemove={() =>
                act(async () => {
                  unwrap(await api.removeMuseJob(job.id));
                  dropJob(job.id);
                })
              }
              onSave={(url) =>
                act(async () => {
                  const saved = unwrap(await api.saveMedia(url));
                  if (!saved.canceled)
                    addToast({ type: 'success', message: `Đã lưu ${saved.path}` });
                })
              }
              onReveal={(url) =>
                act(async () => {
                  unwrap(await api.revealMuseFile(url));
                })
              }
            />
          ))}
        </div>
      )}
    </div>
  );
};

interface JobCardProps {
  job: MuseJob;
  selected: boolean;
  onToggleSelect: () => void;
  accountName: string;
  onCancel: () => void;
  onRetry: () => void;
  onRun: () => void;
  onEditPrompt: (newPrompt: string) => void;
  onRemove: () => void;
  onSave: (url: string) => void;
  onReveal: (url: string) => void;
}

const JobCard: React.FC<JobCardProps> = ({
  job,
  selected,
  onToggleSelect,
  accountName,
  onCancel,
  onRetry,
  onRun,
  onEditPrompt,
  onRemove,
  onSave,
  onReveal,
}) => {
  const [editing, setEditing] = useState(false);
  const [promptText, setPromptText] = useState(job.prompt);

  const active = job.status === MuseJobStatus.Queued || job.status === MuseJobStatus.Processing;
  const ended = job.status === MuseJobStatus.Failed || job.status === MuseJobStatus.Cancelled;

  const handleEditSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!promptText.trim()) return;
    onEditPrompt(promptText);
    setEditing(false);
  };

  return (
    <Card className={`muse-job ${job.held ? 'muse-job-held' : ''}`}>
      <div className="muse-job-head">
        <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', marginRight: 4 }}>
          <input
            type="checkbox"
            checked={selected}
            onChange={onToggleSelect}
            style={{ width: 16, height: 16, cursor: 'pointer' }}
          />
        </label>
        <span className={`muse-badge muse-job-${job.status}`}>{jobStatusLabels[job.status]}</span>
        {job.held && (
          <span
            className="muse-tag"
            style={{ color: 'var(--warning)', borderColor: 'var(--warning)' }}
          >
            Tạm dừng (Held)
          </span>
        )}
        <span className="muse-tag">
          {job.kind === MuseJobKind.Image
            ? `Ảnh × ${job.count}`
            : job.kind === MuseJobKind.V2V
              ? 'V2V'
              : 'Video'}
        </span>
        <span className="muse-tag">{job.ratio}</span>
        {accountName && <span className="muse-tag">{accountName}</span>}
        <span className="muse-job-time">{new Date(job.createdAt).toLocaleString('vi-VN')}</span>
      </div>

      {editing ? (
        <form
          onSubmit={handleEditSubmit}
          style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}
        >
          <textarea
            className="muse-input muse-textarea"
            rows={3}
            value={promptText}
            onChange={(e) => setPromptText(e.target.value)}
          />
          <div style={{ display: 'flex', gap: '8px' }}>
            <Button size="sm" variant="primary" type="submit">
              Lưu & Chạy lại
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
      {job.threadError && <div className="muse-note muse-note-danger">{job.threadError}</div>}
      {job.error && (
        <div className={`muse-note ${ended ? 'muse-note-danger' : ''}`}>{job.error}</div>
      )}

      {(job.files?.length ?? 0) > 0 && (
        <div className="muse-results">
          {(job.files ?? []).map((url) => (
            <div key={url} className="muse-result">
              {isVideoUrl(url) ? (
                <video src={url} controls preload="metadata" />
              ) : (
                <img src={url} alt="" />
              )}
              <div style={{ display: 'flex', gap: '4px', marginTop: '4px' }}>
                <Button size="sm" variant="ghost" onClick={() => onSave(url)}>
                  <Icon icon={Download} size="sm" />
                  <span>Lưu</span>
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => onReveal(url)}
                  title="Mở thư mục chứa file"
                >
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
          <Button size="sm" variant="primary" onClick={onRun}>
            <Icon icon={Play} size="sm" />
            <span>Bắt đầu</span>
          </Button>
        )}
        {active && !job.held && (
          <Button size="sm" onClick={onCancel}>
            <Icon icon={Square} size="sm" />
            <span>Huỷ</span>
          </Button>
        )}
        {ended && (
          <Button size="sm" onClick={onRetry}>
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
          <Button size="sm" variant="ghost" onClick={onRemove}>
            <Icon icon={Trash2} size="sm" />
            <span>Xoá</span>
          </Button>
        )}
      </div>
    </Card>
  );
};
