import React, { useCallback, useMemo, useState } from 'react';
import { CheckSquare, FolderOpen, Pause, Play, RotateCcw, Trash2 } from 'lucide-react';
import './Muse.css';
import { Button, Card, Icon, RevealList, SegmentedControl } from '../../../components/ui';
import { LIST_REVEAL_STEP } from '../../../constants';
import { api, unwrap } from '../../../services';
import { useAccountsStore, useAppStore } from '../../../stores';
import { AccountProvider, MuseJobKind, MuseJobStatus } from '../../../types';
import { jobStatusLabels, selectedShown } from './format';
import { JobCard } from './JobCard';
import type { JobActions } from './JobCard';
import { MuseBatchForm } from './MuseBatchForm';
import { JournalWarning, NoReadyAccountWarning } from './MuseNotices';
import { useMuseStore } from './museStore';
import { useMuseLive } from './useMuseLive';

type Filter = 'all' | MuseJobStatus | 'held';

const filters: readonly { value: Filter; label: string }[] = [
  { value: 'all', label: 'Tất cả' },
  { value: MuseJobStatus.Processing, label: jobStatusLabels[MuseJobStatus.Processing] },
  { value: MuseJobStatus.Queued, label: jobStatusLabels[MuseJobStatus.Queued] },
  { value: 'held', label: 'Tạm dừng' },
  { value: MuseJobStatus.Succeeded, label: jobStatusLabels[MuseJobStatus.Succeeded] },
  { value: MuseJobStatus.Failed, label: jobStatusLabels[MuseJobStatus.Failed] },
];

const titles: Record<MuseJobKind, string> = {
  [MuseJobKind.Image]: 'Tạo ảnh với Muse',
  [MuseJobKind.Video]: 'Tạo video với Muse',
};

/**
 * One Muse kind's workspace: the batch form (MuseBatchForm) and that kind's
 * queue.
 */
export const MuseWorkspace: React.FC<{ kind: MuseJobKind }> = ({ kind }) => {
  useMuseLive();
  const jobs = useMuseStore((s) => s.jobs);
  const options = useMuseStore((s) => s.options);
  const loaded = useMuseStore((s) => s.loaded);
  const loadError = useMuseStore((s) => s.loadError);
  const setJob = useMuseStore((s) => s.setJob);
  const dropJob = useMuseStore((s) => s.dropJob);
  const accounts = useAccountsStore((s) => s.accounts);
  const addToast = useAppStore((s) => s.addToast);
  const [filter, setFilter] = useState<Filter>('all');
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  const museAccounts = useMemo(
    () => accounts.filter((a) => a.provider === AccountProvider.Muse),
    [accounts],
  );
  const kindJobs = useMemo(() => jobs.filter((j) => j.kind === kind), [jobs, kind]);
  const shown = useMemo(() => {
    if (filter === 'all') return kindJobs;
    if (filter === 'held') return kindJobs.filter((j) => j.held);
    return kindJobs.filter((job) => job.status === filter);
  }, [kindJobs, filter]);
  const selected = useMemo(() => selectedShown(shown, selectedIds), [shown, selectedIds]);
  const allShownSelected = shown.length > 0 && selected.length === shown.length;
  const finishedCount = kindJobs.filter((j) => j.status === MuseJobStatus.Succeeded).length;
  const changeFilter = (next: Filter) => {
    setFilter(next);
    setSelectedIds(new Set());
  };
  const accountNames = useMemo(
    () => new Map(museAccounts.map((a) => [a.id, a.name] as const)),
    [museAccounts],
  );

  const report = useCallback(
    (cause: unknown) =>
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) }),
    [addToast],
  );

  const act = useCallback(
    async (action: () => Promise<void>) => {
      try {
        await action();
      } catch (cause) {
        report(cause);
      }
    },
    [report],
  );

  // One stable set of card actions, so a job event re-renders only its card.
  const jobActions = useMemo<JobActions>(
    () => ({
      toggleSelect: (id) =>
        setSelectedIds((prev) => {
          const next = new Set(prev);
          if (next.has(id)) next.delete(id);
          else next.add(id);
          return next;
        }),
      cancel: (id) => void act(async () => setJob(unwrap(await api.cancelMuseJob(id)))),
      retry: (id) => void act(async () => setJob(unwrap(await api.retryMuseJob(id)))),
      // The backend reports the started job through MuseJobUpdated.
      run: (id) => void act(async () => void unwrap(await api.runMuseJobs([id]))),
      editPrompt: (id, prompt) =>
        void act(async () => setJob(unwrap(await api.editAndRetryMuseJob(id, prompt)))),
      remove: (id) =>
        void act(async () => {
          unwrap(await api.removeMuseJob(id));
          dropJob(id);
        }),
      save: (url) =>
        void act(async () => {
          const saved = unwrap(await api.saveMedia(url));
          if (!saved.canceled) addToast({ type: 'success', message: `Đã lưu ${saved.path}` });
        }),
      reveal: (url) => void act(async () => void unwrap(await api.revealMuseFile(url))),
    }),
    [act, addToast, dropJob, setJob],
  );

  // The backend reports bulk changes itself (MuseJobUpdated per job, or
  // MuseJobsChanged), so no reload here.
  const bulk = (run: () => Promise<number>, done: (n: number) => string) =>
    act(async () => {
      const n = await run();
      addToast({ type: 'success', message: done(n) });
      setSelectedIds(new Set());
    });

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <h1 className="muse-title">{titles[kind]}</h1>
      </div>

      {loadError && (
        <div role="alert" className="muse-alert muse-alert-danger">
          {loadError}
        </div>
      )}
      {options?.journalWarning && <JournalWarning message={options.journalWarning} />}
      {loaded && <NoReadyAccountWarning accounts={museAccounts} />}

      <Card className="muse-panel">
        <MuseBatchForm kind={kind} options={options} />
      </Card>

      <div className="muse-header">
        <h2 className="muse-subtitle">Hàng đợi ({kindJobs.length})</h2>
        <SegmentedControl
          options={filters}
          value={filter}
          onChange={changeFilter}
          aria-label="Lọc"
        />
      </div>

      <div className="muse-toolbar">
        <div className="muse-actions">
          {shown.length > 0 && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() =>
                setSelectedIds(allShownSelected ? new Set() : new Set(shown.map((j) => j.id)))
              }
            >
              <Icon icon={CheckSquare} size="sm" />
              <span>{allShownSelected ? 'Bỏ chọn tất cả' : 'Chọn tất cả'}</span>
            </Button>
          )}
          {selected.length > 0 && (
            <>
              <Button
                size="sm"
                variant="primary"
                onClick={() =>
                  void bulk(
                    async () => unwrap(await api.runMuseJobs(selected)),
                    (n) => `Đã chạy ${n} tác vụ.`,
                  )
                }
              >
                <Icon icon={Play} size="sm" />
                <span>Chạy đã chọn ({selected.length})</span>
              </Button>
              <Button
                size="sm"
                variant="danger"
                onClick={() => {
                  if (
                    !window.confirm(
                      `Xoá ${selected.length} tác vụ đã chọn khỏi hàng đợi? Tác vụ đang chạy được giữ lại; file đã tạo vẫn còn trên đĩa.`,
                    )
                  )
                    return;
                  void bulk(
                    async () => unwrap(await api.removeMuseJobs(selected)),
                    (n) => `Đã xoá ${n} tác vụ.`,
                  );
                }}
              >
                <Icon icon={Trash2} size="sm" />
                <span>Xoá đã chọn ({selected.length})</span>
              </Button>
            </>
          )}
        </div>
        <div className="muse-actions">
          <Button
            size="sm"
            variant="secondary"
            onClick={() =>
              void bulk(
                async () => unwrap(await api.holdMuseQueued(kind)),
                (n) => `Đã tạm dừng ${n} tác vụ.`,
              )
            }
          >
            <Icon icon={Pause} size="sm" />
            <span>Tạm dừng hàng đợi</span>
          </Button>
          <Button
            size="sm"
            variant="secondary"
            onClick={() =>
              void bulk(
                async () => unwrap(await api.retryMuseFailed(kind)),
                (n) => `Đã chạy lại ${n} tác vụ lỗi.`,
              )
            }
          >
            <Icon icon={RotateCcw} size="sm" />
            <span>Chạy lại lỗi</span>
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={finishedCount === 0}
            onClick={() => {
              if (
                !window.confirm(
                  `Xoá ${finishedCount} tác vụ đã xong khỏi hàng đợi? Thư viện Muse vẫn giữ các file đã tạo.`,
                )
              )
                return;
              void bulk(
                async () => unwrap(await api.clearMuseFinished(kind)),
                (n) => `Đã dọn ${n} tác vụ xong.`,
              );
            }}
          >
            <Icon icon={Trash2} size="sm" />
            <span>Xoá đã xong</span>
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => void act(async () => void unwrap(await api.openMuseOutputFolder()))}
          >
            <Icon icon={FolderOpen} size="sm" />
            <span>Mở thư mục lưu</span>
          </Button>
        </div>
      </div>

      {shown.length === 0 ? (
        <div className="muse-empty">{loaded ? 'Không có tác vụ nào.' : 'Đang tải…'}</div>
      ) : (
        <RevealList
          className="muse-jobs"
          items={shown}
          keyOf={(job) => job.id}
          step={LIST_REVEAL_STEP}
          resetKey={`${kind}:${filter}`}
          render={(job) => (
            <JobCard
              job={job}
              selected={selectedIds.has(job.id)}
              accountName={accountNames.get(job.accountId ?? '') ?? ''}
              actions={jobActions}
            />
          )}
        />
      )}
    </div>
  );
};
