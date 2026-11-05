import { thumbUrl } from '../../../../services';
import { useCallback, useMemo, useState } from 'react';
import { X } from 'lucide-react';
import { QueueFilterBar, QueueTable } from '../../../../components/batch';
import { PreviousRunToggle } from '../../../../components/feedback';
import { Icon, Modal } from '../../../../components/ui';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { FlowVideoTaskStatus } from '../../../../types';
import type { FlowImageTaskReference, FlowVideoTask, GeneratedVideoItem } from '../../../../types';
import { useFlowVideoStore } from '../store/flowVideoStore';
import { QueueActions } from './QueueActions';
import { TaskRow } from './TaskRow';
import type { TaskRowActions } from './TaskRow';
import {
  TASK_FILTERS,
  matchesTaskFilter,
  useVideoQueueStore,
  useVisibleVideoTasks,
} from './queueStore';
import type { TaskFilter } from './queueStore';
import { applyToTasks, overCapacity, referenceTargets } from './references';
import { useVideoQueueLive } from './useVideoQueueLive';
import './AllWorkspace.css';

/** The video queue (Video → Hàng loạt) as the backend runs it, with its results. */
export function AllWorkspace() {
  useVideoQueueLive();
  const { tasks, offered } = useVisibleVideoTasks();
  const warning = useVideoQueueStore((s) => s.warning);
  const loadError = useVideoQueueStore((s) => s.loadError);
  const dismissWarning = useVideoQueueStore((s) => s.dismissWarning);
  const models = useFlowVideoStore((s) => s.models);
  const addToast = useAppStore((state) => state.addToast);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [filter, setFilter] = useState<TaskFilter>('all');
  const [previewVideo, setPreviewVideo] = useState<GeneratedVideoItem | null>(null);
  const [gallery, setGallery] = useState<{ task: FlowVideoTask; number: number } | null>(null);

  // A task's number is its prompt's place in its batch, as in its file names;
  // tasks queued before batches were numbered use their place in the queue.
  const numbers = useMemo(
    () => new Map(tasks.map((task, index) => [task.id, task.index || index + 1])),
    [tasks],
  );
  const modelLimits = useMemo(
    () => new Map(models.map((m) => [m.id, m.maxReferenceImages])),
    [models],
  );

  const shown = useMemo(
    () => tasks.filter((task) => matchesTaskFilter(task, filter)),
    [tasks, filter],
  );
  const filterOptions = useMemo(() => {
    const counts = TASK_FILTERS.map(() => 0);
    for (const task of tasks) {
      TASK_FILTERS.forEach(({ value }, i) => {
        if (matchesTaskFilter(task, value)) counts[i]++;
      });
    }
    return TASK_FILTERS.map(({ value, label }, i) => ({ value, label, count: counts[i] }));
  }, [tasks]);

  // Selection and bulk actions cover only the tasks the filter shows.
  const { notRunning, selectedTasks, retryable, failed, busy, removable, allSelected, targets } =
    useMemo(() => {
      const notRunning = shown.filter((task) => task.status !== FlowVideoTaskStatus.Running);
      const selectedTasks = notRunning.filter((task) => selected.has(task.id));
      return {
        notRunning,
        selectedTasks,
        retryable: selectedTasks.filter(
          (task) =>
            task.status === FlowVideoTaskStatus.Failed ||
            task.status === FlowVideoTaskStatus.Cancelled,
        ),
        failed: tasks.filter((task) => task.status === FlowVideoTaskStatus.Failed),
        busy: tasks.some(
          (task) =>
            task.status === FlowVideoTaskStatus.Running ||
            (task.status === FlowVideoTaskStatus.Queued && !task.held),
        ),
        removable: tasks.filter(
          (task) => task.status !== FlowVideoTaskStatus.Running && !task.requestId,
        ),
        allSelected: notRunning.length > 0 && notRunning.every((task) => selected.has(task.id)),
        // References go to the selected tasks, or to every task that can take them.
        targets: referenceTargets(selectedTasks.length > 0 ? selectedTasks : tasks),
      };
    }, [shown, tasks, selected]);

  const report = useCallback(
    (title: string, message?: string) =>
      addToast({ type: 'error', title, message: message ?? 'Không rõ lỗi.' }),
    [addToast],
  );

  const actions = useMemo<TaskRowActions>(
    () => ({
      toggle: (id, checked) =>
        setSelected((current) => {
          const next = new Set(current);
          if (checked) next.add(id);
          else next.delete(id);
          return next;
        }),
      preview: setPreviewVideo,
      showReferences: (task, number) => setGallery({ task, number }),
      cancel: (id) =>
        void api.cancelFlowVideoTask(id).then((res) => {
          if (!res.success) report('Không hủy được tác vụ', res.error);
        }),
      retry: (id) =>
        void api.retryFlowVideoTasks([id]).then((res) => {
          if (!res.success) report('Không chạy lại được tác vụ', res.error);
        }),
      edit: (id, prompt) =>
        void api.editFlowVideoTask(id, prompt).then((res) => {
          if (!res.success) report('Không sửa được prompt', res.error);
        }),
      reveal: (path) =>
        void api.revealFile(path).then((res) => {
          if (!res.success) report('Không mở được thư mục', res.error);
        }),
    }),
    [report],
  );

  const applyReferences = async (refs: FlowImageTaskReference[], errors: string[] = []) => {
    if (refs.length > 0) {
      const tooMany = overCapacity(targets, refs, models);
      if (tooMany) {
        report(
          'Không thể thêm ảnh cho tất cả prompt',
          `Model của tác vụ ${numbers.get(tooMany.id)} không đủ chỗ cho ảnh tham chiếu đã chọn.`,
        );
        return false;
      }
      useFlowVideoStore.getState().addQueueReferences(refs);
      const failed = await applyToTasks(targets, refs);
      if (failed) {
        report('Không thêm được ảnh tham chiếu', failed);
        return false;
      }
      addToast({
        type: 'success',
        title: 'Đã thêm ảnh tham chiếu',
        message:
          targets.length > 0
            ? `Đã áp dụng ${refs.length} ảnh cho ${targets.length} prompt chưa gửi yêu cầu.`
            : `Đã thêm ${refs.length} ảnh cho các prompt sắp thêm.`,
      });
    }
    if (errors.length > 0) {
      addToast({
        type: 'warning',
        title: 'Một số ảnh không tải được',
        message: errors.join('; '),
      });
    }
    return true;
  };

  const pickReferences = async () => {
    const response = await api.pickFlowVideoReferences();
    if (!response.success) {
      report('Không thêm được ảnh tham chiếu', response.error);
      return;
    }
    await applyReferences(response.data ?? []);
  };

  const bulk = async (call: Promise<{ success: boolean; error?: string }>, title: string) => {
    const response = await call;
    if (!response.success) report(title, response.error);
    setSelected(new Set());
  };

  return (
    <div className="flow-workspace-container flow-video-all-workspace">
      {warning && (
        <div className="flow-video-all-warning" role="alert">
          <span>{warning}</span>
          <button type="button" onClick={dismissWarning} aria-label="Ẩn cảnh báo">
            <Icon icon={X} size="sm" />
          </button>
        </div>
      )}
      <QueueFilterBar
        options={filterOptions}
        value={filter}
        onChange={(next) => {
          setFilter(next);
          setSelected(new Set());
        }}
        actions={
          <>
            <PreviousRunToggle count={offered} />
            <QueueActions
              selectedCount={selectedTasks.length}
              retryableCount={retryable.length}
              failedCount={failed.length}
              busy={busy}
              removableCount={removable.length}
              canAddReferences={models.length > 0}
              onPickReferences={() => void pickReferences()}
              onRetrySelected={() =>
                void bulk(
                  api.retryFlowVideoTasks(retryable.map((task) => task.id)),
                  'Không chạy lại được tác vụ',
                )
              }
              onRemoveSelected={() =>
                void bulk(
                  api.removeFlowVideoTasks(
                    selectedTasks.filter((task) => !task.requestId).map((task) => task.id),
                  ),
                  'Không xóa được tác vụ',
                )
              }
              onRetryFailed={() =>
                void bulk(
                  api.retryFlowVideoTasks(failed.map((task) => task.id)),
                  'Không chạy lại được tác vụ lỗi',
                )
              }
              onRemoveAll={() =>
                void bulk(
                  api.removeFlowVideoTasks(removable.map((task) => task.id)),
                  'Không xóa được hàng đợi',
                )
              }
            />
          </>
        }
      />
      <QueueTable
        label="Hàng đợi tạo video"
        selectAll={
          <input
            type="checkbox"
            aria-label="Chọn tất cả tác vụ"
            checked={allSelected}
            disabled={notRunning.length === 0}
            onChange={(event) =>
              setSelected(
                event.currentTarget.checked
                  ? new Set(notRunning.map((task) => task.id))
                  : new Set(),
              )
            }
          />
        }
        headings={['Prompt', 'Ảnh tham chiếu', 'Kết quả', 'Tiến độ']}
        items={shown}
        keyOf={(task) => task.id}
        resetKey={filter}
        empty={
          <>
            <strong>
              {loadError
                ? 'Không tải được hàng đợi'
                : tasks.length > 0
                  ? 'Không có tác vụ nào ở mục này'
                  : 'Chưa có tác vụ'}
            </strong>
            {(loadError || tasks.length === 0) && (
              <span>
                {loadError ?? 'Nhập prompt hoặc tệp .txt ở cột bên trái, rồi bấm "Chạy".'}
              </span>
            )}
          </>
        }
        render={(task) => (
          <TaskRow
            task={task}
            number={numbers.get(task.id) ?? 0}
            referenceLimit={Math.max(task.references.length, modelLimits.get(task.modelId) ?? 0)}
            selected={selected.has(task.id)}
            actions={actions}
          />
        )}
      />

      {gallery && (
        <Modal
          title={`Ảnh tham chiếu · Tác vụ ${gallery.number}`}
          subtitle={`${gallery.task.references.length} ảnh đã chọn`}
          onClose={() => setGallery(null)}
        >
          <div className="flow-video-all-gallery-grid">
            {gallery.task.references.map((reference) => (
              <figure className="flow-video-all-gallery-item" key={reference.localUrl}>
                <img
                  src={thumbUrl(reference.localUrl)}
                  alt={reference.fileName}
                  loading="lazy"
                  decoding="async"
                />
                <figcaption title={reference.fileName}>{reference.fileName}</figcaption>
              </figure>
            ))}
          </div>
        </Modal>
      )}

      {previewVideo?.localUrl && (
        <Modal
          variant="lightbox"
          label="Video kết quả phóng to"
          onClose={() => setPreviewVideo(null)}
        >
          <div className="flow-video-preview-modal">
            <video controls autoPlay src={previewVideo.localUrl} />
          </div>
        </Modal>
      )}
    </div>
  );
}
