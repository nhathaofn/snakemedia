import { useCallback, useMemo, useState } from 'react';
import { FolderOpen, Pause, Play, RotateCcw, Trash2 } from 'lucide-react';
import {
  BatchForm,
  ConfigGroup,
  OutputFolderField,
  QueueFilterBar,
  QueueTable,
  RunDock,
} from '../../../components/batch';
import { Button, Icon, IconAction, Modal } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAccountsStore, useAppStore } from '../../../stores';
import {
  AccountProvider,
  MuseJobKind,
  MuseJobMode,
  MuseJobStatus,
  MuseJobsFormatRequest,
} from '../../../types';
import type { MuseJob } from '../../../types';
import { isEnded, jobStatusLabels, jobsOf, matchesFilter, mayBePaid } from '../shared/format';
import type { QueueFilter } from '../shared/format';
import { useMuseDraft } from '../shared/museDraft';
import { FormatFields } from '../shared/MusePanel';
import { MuseNotices } from '../shared/MuseNotices';
import {
  activeExtras,
  FixedAssetsField,
  referenceFileSummary,
  StoryboardField,
  TemplateField,
  useMuseStudio,
} from '../shared/StudioExtras';
import { useMuseStore, useVisibleMuseJobs } from '../shared/museStore';
import { useMuseActions, useMuseSubmit } from '../shared/useMuseActions';
import { JobRow } from './JobRow';

const placeholders: Record<MuseJobKind, string> = {
  [MuseJobKind.Image]: 'Chân dung cô gái dưới mưa đèn neon, ảnh dọc',
  [MuseJobKind.Video]: 'Một chú mèo chạy trên bãi biển lúc hoàng hôn, video dọc 9:16',
};

const filters: readonly { value: QueueFilter; label: string }[] = [
  { value: 'all', label: 'Tất cả' },
  { value: MuseJobStatus.Processing, label: jobStatusLabels[MuseJobStatus.Processing] },
  { value: MuseJobStatus.Queued, label: jobStatusLabels[MuseJobStatus.Queued] },
  { value: 'held', label: 'Tạm dừng' },
  { value: MuseJobStatus.Succeeded, label: jobStatusLabels[MuseJobStatus.Succeeded] },
  { value: MuseJobStatus.Failed, label: jobStatusLabels[MuseJobStatus.Failed] },
];

/**
 * Hàng loạt tools laid out like Flow's: settings groups (ratio, duration and
 * folder; references), then the prompt box. Prompts typed (Enter) or imported
 * go to the queue on hold; "Chạy" starts them, "Dừng" holds the rest.
 */
export function MuseBatchPanel({ kind }: { kind: MuseJobKind }) {
  const formId = `muse-${kind}-batch`;
  const options = useMuseStore((s) => s.options);
  const { assets } = useMuseStudio();
  const allJobs = useVisibleMuseJobs();
  const jobs = useMemo(() => jobsOf(allJobs, kind, MuseJobMode.Batch), [allJobs, kind]);
  // Held jobs wait for "Chạy"; prompts still waiting or running are not added twice.
  const held = jobs.filter((j) => j.status === MuseJobStatus.Queued && j.held);
  const waiting = jobs.filter((j) => j.status === MuseJobStatus.Queued && !j.held).length;
  const pending = useMemo(
    () =>
      jobs
        .filter((j) => j.status === MuseJobStatus.Queued || j.status === MuseJobStatus.Processing)
        .map((j) => j.prompt),
    [jobs],
  );
  const [draft, update] = useMuseDraft(kind, MuseJobMode.Batch);
  const [outputDir, setOutputDir] = useState('');
  const { submit } = useMuseSubmit(kind, MuseJobMode.Batch);
  const addToast = useAppStore((s) => s.addToast);
  const { act } = useMuseActions();

  const syncFormat = (change: { ratio?: string; duration?: number }) => {
    if (!jobs.some((j) => j.status === MuseJobStatus.Queued && !mayBePaid(j))) return;
    void act(async () => {
      const updated = unwrap(
        await api.updateMuseJobsFormat(
          MuseJobsFormatRequest.createFrom({
            kind,
            mode: MuseJobMode.Batch,
            ...change,
          }),
        ),
      );
      if (updated > 0) {
        addToast({ type: 'success', message: `Đã cập nhật cho ${updated} tác vụ đang chờ.` });
      }
    });
  };

  return (
    <div className="studio-mode-panel is-docked">
      <div className="studio-panel-scroll">
        <ConfigGroup title="Cấu hình cơ bản">
          <FormatFields
            kind={kind}
            options={options}
            ratio={draft.ratio}
            duration={draft.duration}
            onChange={update}
            onUserChange={syncFormat}
          />
          <OutputFolderField
            provider={AccountProvider.Muse}
            value={outputDir}
            onChange={setOutputDir}
          />
        </ConfigGroup>
        <ConfigGroup
          title="Đồng nhất nhân vật"
          collapsible
          badge={activeExtras(draft)}
          summary={referenceFileSummary(assets, draft.assetIds)}
        >
          <FixedAssetsField draft={draft} update={update} />
          {kind === MuseJobKind.Video && <StoryboardField draft={draft} update={update} />}
          <TemplateField />
        </ConfigGroup>
        <BatchForm
          id={formId}
          rows={4}
          placeholder={placeholders[kind]}
          pending={pending}
          onAdd={async (prompts) => {
            const added = await submit(prompts, 1, true, draft, outputDir);
            if (added) {
              addToast({
                type: 'success',
                message: `Đã thêm ${prompts.length} prompt vào hàng chờ. Bấm "Chạy" để bắt đầu.`,
              });
            }
            return added;
          }}
        />
      </div>
      <RunDock
        heldCount={held.length}
        waitingCount={waiting}
        onRun={() =>
          void act(async () => void unwrap(await api.runMuseJobs(held.map((j) => j.id))))
        }
        onStop={() =>
          void act(async () => void unwrap(await api.holdMuseQueued(kind, MuseJobMode.Batch)))
        }
      />
    </div>
  );
}

/** Hàng loạt workspace: this kind's batch jobs as a compact queue. */
export function MuseQueueWorkspace({ kind }: { kind: MuseJobKind }) {
  const allJobs = useVisibleMuseJobs();
  const loaded = useMuseStore((s) => s.loaded);
  const accounts = useAccountsStore((s) => s.accounts);
  const addToast = useAppStore((s) => s.addToast);
  const { act, jobActions } = useMuseActions();
  const [filter, setFilter] = useState<QueueFilter>('all');
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(new Set());
  const [preview, setPreview] = useState<{ job: MuseJob; url: string } | null>(null);

  // Oldest first, like Flow's queue: a batch reads 1..n from the top.
  const jobs = useMemo(() => jobsOf(allJobs, kind, MuseJobMode.Batch).reverse(), [allJobs, kind]);
  const openPreview = useCallback((job: MuseJob, url: string) => setPreview({ job, url }), []);
  const shown = useMemo(() => jobs.filter((job) => matchesFilter(job, filter)), [jobs, filter]);
  const selectedJobs = useMemo(
    () => shown.filter((job) => selectedIds.has(job.id)),
    [shown, selectedIds],
  );
  const selected = useMemo(() => selectedJobs.map((j) => j.id), [selectedJobs]);
  const runnableSelected = useMemo(
    () => selectedJobs.filter((j) => j.status === MuseJobStatus.Queued && j.held).map((j) => j.id),
    [selectedJobs],
  );
  const retryableSelected = useMemo(() => selectedJobs.filter(isEnded), [selectedJobs]);
  const allShownSelected = shown.length > 0 && selected.length === shown.length;
  const counts = useMemo(() => {
    const byFilter = new Map<QueueFilter, number>();
    for (const { value } of filters) {
      byFilter.set(value, jobs.filter((job) => matchesFilter(job, value)).length);
    }
    return byFilter;
  }, [jobs]);
  const queuedCount = useMemo(
    () => jobs.filter((j) => j.status === MuseJobStatus.Queued && !j.held).length,
    [jobs],
  );
  const failedJobs = useMemo(() => jobs.filter((j) => j.status === MuseJobStatus.Failed), [jobs]);
  // A job's number is its prompt's place in its batch, else its place in the queue.
  const numbers = useMemo(
    () => new Map(jobs.map((job, index) => [job.id, job.index || index + 1])),
    [jobs],
  );
  const finishedCount = counts.get(MuseJobStatus.Succeeded) ?? 0;
  const accountNames = useMemo(
    () =>
      new Map(
        accounts.filter((a) => a.provider === AccountProvider.Muse).map((a) => [a.id, a.name]),
      ),
    [accounts],
  );

  const filterOptions = useMemo(
    () => filters.map(({ value, label }) => ({ value, label, count: counts.get(value) ?? 0 })),
    [counts],
  );

  const toggle = useMemo(
    () => (id: string) =>
      setSelectedIds((prev) => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      }),
    [],
  );

  const changeFilter = (next: QueueFilter) => {
    setFilter(next);
    setSelectedIds(new Set());
  };

  // The backend reports bulk changes itself (MuseJobUpdated per job, or
  // MuseJobsRemoved), so no reload here.
  const bulk = (run: () => Promise<number>, done: (n: number) => string) =>
    act(async () => {
      const n = await run();
      addToast({ type: 'success', message: done(n) });
      setSelectedIds(new Set());
    });

  const retryBatch = (targets: readonly MuseJob[]) => {
    if (targets.length === 0) return;
    const paidCount = targets.filter(mayBePaid).length;
    if (
      paidCount > 0 &&
      !window.confirm(
        `Có ${paidCount}/${targets.length} tác vụ mà Muse có thể đã nhận. Chạy lại sẽ gửi chat mới và có thể tốn thêm lượt. Tiếp tục?`,
      )
    ) {
      return;
    }
    void bulk(
      async () => unwrap(await api.retryMuseJobs(targets.map((j) => j.id))),
      (n) => `Đã chuyển ${n} tác vụ sang Chờ chạy.`,
    );
  };

  return (
    <div className="muse-workspace">
      <MuseNotices />
      <QueueFilterBar
        options={filterOptions}
        value={filter}
        onChange={changeFilter}
        actions={
          <>
            {selected.length > 0 ? (
              <>
                {runnableSelected.length > 0 && (
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={() =>
                      void bulk(
                        async () => unwrap(await api.runMuseJobs(runnableSelected)),
                        (n) => `Đã chạy ${n} tác vụ.`,
                      )
                    }
                  >
                    <Icon icon={Play} size="sm" />
                    <span>Chạy ({runnableSelected.length})</span>
                  </Button>
                )}
                {retryableSelected.length > 0 && (
                  <Button size="sm" variant="primary" onClick={() => retryBatch(retryableSelected)}>
                    <Icon icon={RotateCcw} size="sm" />
                    <span>Chạy lại ({retryableSelected.length})</span>
                  </Button>
                )}
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
                  <span>Xoá ({selected.length})</span>
                </Button>
              </>
            ) : (
              <>
                <IconAction
                  icon={Pause}
                  label="Tạm dừng các tác vụ đang chờ"
                  disabled={queuedCount === 0}
                  onClick={() =>
                    void bulk(
                      async () => unwrap(await api.holdMuseQueued(kind, MuseJobMode.Batch)),
                      (n) => `Đã tạm dừng ${n} tác vụ.`,
                    )
                  }
                />
                <IconAction
                  icon={RotateCcw}
                  label="Chạy lại các tác vụ lỗi"
                  disabled={failedJobs.length === 0}
                  onClick={() => retryBatch(failedJobs)}
                />
                <IconAction
                  icon={Trash2}
                  label="Xoá các tác vụ đã xong (Thư viện vẫn giữ file)"
                  disabled={finishedCount === 0}
                  onClick={() => {
                    if (
                      !window.confirm(
                        `Xoá ${finishedCount} tác vụ đã xong khỏi hàng đợi? Thư viện Muse vẫn giữ các file đã tạo.`,
                      )
                    )
                      return;
                    void bulk(
                      async () =>
                        // Older runs are always hidden: clear only the finished jobs shown.
                        unwrap(
                          await api.removeMuseJobs(
                            jobs
                              .filter((j) => j.status === MuseJobStatus.Succeeded)
                              .map((j) => j.id),
                          ),
                        ),
                      (n) => `Đã dọn ${n} tác vụ xong.`,
                    );
                  }}
                />
                <IconAction
                  icon={FolderOpen}
                  label="Mở thư mục lưu"
                  onClick={() =>
                    void act(async () => void unwrap(await api.openMuseOutputFolder()))
                  }
                />
              </>
            )}
          </>
        }
      />

      <QueueTable
        label="Hàng đợi Muse"
        selectAll={
          <input
            type="checkbox"
            aria-label="Chọn tất cả"
            checked={allShownSelected}
            disabled={shown.length === 0}
            onChange={() =>
              setSelectedIds(allShownSelected ? new Set() : new Set(shown.map((j) => j.id)))
            }
          />
        }
        headings={['Prompt', 'Ảnh tham chiếu', 'Kết quả', 'Tiến độ']}
        items={shown}
        keyOf={(job) => job.id}
        resetKey={`${kind}:${filter}`}
        empty={
          <span>
            {!loaded
              ? 'Đang tải…'
              : jobs.length === 0
                ? 'Hàng đợi trống. Nhập prompt ở bên trái rồi bấm Thêm.'
                : 'Không có tác vụ nào ở mục này.'}
          </span>
        }
        render={(job) => (
          <JobRow
            job={job}
            number={numbers.get(job.id) ?? 0}
            selected={selectedIds.has(job.id)}
            accountName={accountNames.get(job.accountId ?? '') ?? ''}
            actions={jobActions}
            onToggle={toggle}
            onPreview={openPreview}
          />
        )}
      />

      {preview && (
        <Modal variant="lightbox" label="Kết quả phóng to" onClose={() => setPreview(null)}>
          {preview.job.kind === MuseJobKind.Video ? (
            <video src={preview.url} controls autoPlay />
          ) : (
            <img src={preview.url} alt={preview.job.prompt} decoding="async" />
          )}
        </Modal>
      )}
    </div>
  );
}
