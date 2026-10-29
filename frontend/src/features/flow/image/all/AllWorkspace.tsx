import { thumbUrl } from '../../../../services';
import { useMemo, useState } from 'react';
import { X } from 'lucide-react';
import { QueueFilterBar, QueueTable } from '../../../../components/batch';
import { PreviousRunToggle } from '../../../../components/feedback';
import { Icon, Modal } from '../../../../components/ui';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { FlowImageTaskStatus } from '../../../../types';
import type { FlowImageTask, FlowImageTaskReference, GeneratedImageItem } from '../../../../types';
import { useFlowImageStore } from '../store/flowImageStore';
import { QueueActions } from './QueueActions';
import { ReferenceBankDialog } from './ReferenceBankDialog';
import { TaskRow } from './TaskRow';
import type { TaskRowActions } from './TaskRow';
import {
  TASK_FILTERS,
  matchesTaskFilter,
  useImageQueueStore,
  useVisibleImageTasks,
} from './queueStore';
import type { TaskFilter } from './queueStore';
import { applyToTasks, overCapacity, referenceTargets } from './references';
import { useImageQueueLive } from './useImageQueueLive';
import './AllWorkspace.css';

/** The image queue (Ảnh → Hàng loạt) as the backend runs it, with its results. */
export function AllWorkspace() {
  useImageQueueLive();
  const { tasks, offered } = useVisibleImageTasks();
  const warning = useImageQueueStore((s) => s.warning);
  const loadError = useImageQueueStore((s) => s.loadError);
  const dismissWarning = useImageQueueStore((s) => s.dismissWarning);
  const models = useFlowImageStore((s) => s.models);
  const addToast = useAppStore((state) => state.addToast);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [filter, setFilter] = useState<TaskFilter>('all');
  const [previewImage, setPreviewImage] = useState<GeneratedImageItem | null>(null);
  const [gallery, setGallery] = useState<{ task: FlowImageTask; number: number } | null>(null);
  const [bankOpen, setBankOpen] = useState(false);

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
  const filterOptions = useMemo(
    () =>
      TASK_FILTERS.map(({ value, label }) => ({
        value,
        label,
        count: tasks.filter((task) => matchesTaskFilter(task, value)).length,
      })),
    [tasks],
  );
  // Selection and bulk actions cover only the tasks the filter shows.
  const notRunning = shown.filter((task) => task.status !== FlowImageTaskStatus.Running);
  const selectedTasks = notRunning.filter((task) => selected.has(task.id));
  const retryable = selectedTasks.filter(
    (task) =>
      task.status === FlowImageTaskStatus.Failed || task.status === FlowImageTaskStatus.Cancelled,
  );
  const failed = tasks.filter((task) => task.status === FlowImageTaskStatus.Failed);
  const busy = tasks.some(
    (task) =>
      task.status === FlowImageTaskStatus.Running ||
      (task.status === FlowImageTaskStatus.Queued && !task.held),
  );
  const removable = tasks.filter((task) => task.status !== FlowImageTaskStatus.Running);
  const allSelected = notRunning.length > 0 && notRunning.every((task) => selected.has(task.id));
  // References go to the selected tasks, or to every task that can take them.
  const targets = referenceTargets(selectedTasks.length > 0 ? selectedTasks : tasks);

  const report = (title: string, message?: string) =>
    addToast({ type: 'error', title, message: message ?? 'Không rõ lỗi.' });

  const actions = useMemo<TaskRowActions>(
    () => ({
      toggle: (id, checked) =>
        setSelected((current) => {
          const next = new Set(current);
          if (checked) next.add(id);
          else next.delete(id);
          return next;
        }),
      preview: setPreviewImage,
      showReferences: (task, number) => setGallery({ task, number }),
      cancel: (id) =>
        void api.cancelFlowImageTask(id).then((res) => {
          if (!res.success) report('Không hủy được tác vụ', res.error);
        }),
      retry: (id) =>
        void api.retryFlowImageTasks([id]).then((res) => {
          if (!res.success) report('Không chạy lại được tác vụ', res.error);
        }),
      edit: (id, prompt) =>
        void api.editFlowImageTask(id, prompt).then((res) => {
          if (!res.success) report('Không sửa được prompt', res.error);
        }),
      reveal: (path) =>
        void api.revealFile(path).then((res) => {
          if (!res.success) report('Không mở được thư mục', res.error);
        }),
    }),
    // report only wraps the stable addToast.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [addToast],
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
      useFlowImageStore.getState().addQueueReferences(refs);
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
        title: 'Một số ảnh không tải được từ Flow',
        message: errors.join('; '),
      });
    }
    return true;
  };

  const pickReferences = async () => {
    const response = await api.pickFlowImageReferences();
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
    <div className="flow-workspace-container flow-image-all-workspace">
      {warning && (
        <div className="flow-image-all-warning" role="alert">
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
              onOpenBank={() => setBankOpen(true)}
              onRetrySelected={() =>
                void bulk(
                  api.retryFlowImageTasks(retryable.map((task) => task.id)),
                  'Không chạy lại được tác vụ',
                )
              }
              onRemoveSelected={() =>
                void bulk(
                  api.removeFlowImageTasks(selectedTasks.map((task) => task.id)),
                  'Không xóa được tác vụ',
                )
              }
              onRetryFailed={() =>
                void bulk(
                  api.retryFlowImageTasks(failed.map((task) => task.id)),
                  'Không chạy lại được tác vụ lỗi',
                )
              }
              onRemoveAll={() =>
                void bulk(
                  api.removeFlowImageTasks(removable.map((task) => task.id)),
                  'Không xóa được hàng đợi',
                )
              }
            />
          </>
        }
      />
      <QueueTable
        label="Hàng đợi tạo ảnh"
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
              <span>{loadError ?? 'Dán prompt vào ô ở cột bên trái rồi bấm "Thêm & chạy".'}</span>
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

      {bankOpen && (
        <ReferenceBankDialog
          applyLabel={
            targets.length > 0 ? 'Thêm ảnh cho các prompt chưa gửi' : 'Thêm vào ảnh tham chiếu'
          }
          onClose={() => setBankOpen(false)}
          onApply={(refs, errors) =>
            void applyReferences(refs, errors).then((done) => {
              if (done) setBankOpen(false);
            })
          }
        />
      )}

      {gallery && (
        <Modal
          title={`Ảnh tham chiếu · Tác vụ ${gallery.number}`}
          subtitle={`${gallery.task.references.length} ảnh đã chọn`}
          onClose={() => setGallery(null)}
        >
          <div className="flow-image-all-gallery-grid">
            {gallery.task.references.map((reference) => (
              <figure className="flow-image-all-gallery-item" key={reference.localUrl}>
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

      {previewImage?.localUrl && (
        <Modal
          variant="lightbox"
          label="Ảnh kết quả phóng to"
          onClose={() => setPreviewImage(null)}
        >
          <img src={previewImage.localUrl} alt={previewImage.prompt} decoding="async" />
        </Modal>
      )}
    </div>
  );
}
