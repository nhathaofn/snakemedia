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
import { useAccountsStore, useAppStore, usePreviousRunStore } from '../../../stores';
import { AccountProvider, MuseJobKind, MuseJobMode, MuseJobStatus } from '../../../types';
import type { MuseJob } from '../../../types';
import {
  isEnded,
  jobStatusLabels,
  jobsOf,
  matchesFilter,
  mayBePaid,
  selectedShown,
} from '../shared/format';
import type { QueueFilter } from '../shared/format';
import { useMuseDraft } from '../shared/museDraft';
import { FormatFields, ReferencesField } from '../shared/MusePanel';
import { MuseNotices } from '../shared/MuseNotices';
import {
  FixedAssetsField,
  RefModeField,
  SourceVideoField,
  StoryboardField,
  TemplateField,
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
  const submit = useMuseSubmit(kind, MuseJobMode.Batch);
  const addToast = useAppStore((s) => s.addToast);
  const { act, report } = useMuseActions();
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
          />
          <OutputFolderField
            provider={AccountProvider.Muse}
            value={outputDir}
            onChange={setOutputDir}
          />
        </ConfigGroup>
        <ConfigGroup title="Ảnh tham chiếu">
          <ReferencesField
            label="Đã chọn"
            references={draft.references}
            options={options}
            onChange={(references) => update({ references })}
            onError={report}
          />
          <RefModeField draft={draft} update={update} />
        </ConfigGroup>
        <ConfigGroup title="Đồng nhất nhân vật">
          <FixedAssetsField draft={draft} update={update} />
          {kind === MuseJobKind.Video && <StoryboardField draft={draft} update={update} />}
          <SourceVideoField draft={draft} update={update} />
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
  const showPrevious = usePreviousRunStore((s) => s.showPrevious);
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
  const selected = useMemo(() => selectedShown(shown, selectedIds), [shown, selectedIds]);
  const allShownSelected = shown.length > 0 && selected.length === shown.length;
  const counts = useMemo(() => {
    const byFilter = new Map<QueueFilter, number>();
    for (const { value } of filters) {
      byFilter.set(value, jobs.filter((job) => matchesFilter(job, value)).length);
    }
    return byFilter;
  }, [jobs]);
  const queuedCount = jobs.filter((j) => j.status === MuseJobStatus.Queued && !j.held).length;
  const endedCount = jobs.filter(isEnded).length;
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
                  <span>Chạy ({selected.length})</span>
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
                  disabled={endedCount === 0}
                  onClick={() =>
                    void bulk(
                      async () => {
                        if (showPrevious) {
                          return unwrap(await api.retryMuseFailed(kind, MuseJobMode.Batch));
                        }
                        // Earlier runs are hidden: retry only the failed jobs shown, never
                        // ones Muse may already have taken (the backend's bulk rule).
                        // A refused retry does not stop the others; the count says how many went.
                        const shownFailed = jobs.filter((j) => isEnded(j) && !mayBePaid(j));
                        let retried = 0;
                        for (const j of shownFailed) {
                          if ((await api.retryMuseJob(j.id)).success) retried++;
                        }
                        return retried;
                      },
                      (n) => `Đã chạy lại ${n} tác vụ lỗi.`,
                    )
                  }
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
                        // Earlier runs are hidden: clear only the finished jobs shown.
                        showPrevious
                          ? unwrap(await api.clearMuseFinished(kind, MuseJobMode.Batch))
                          : unwrap(
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
            number={job.index || jobs.indexOf(job) + 1}
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
