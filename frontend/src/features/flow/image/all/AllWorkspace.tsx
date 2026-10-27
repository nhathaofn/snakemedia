import { useMemo, useState } from 'react';
import { X } from 'lucide-react';
import { Icon, Modal, RevealList } from '../../../../components/ui';
import { LIST_REVEAL_STEP } from '../../../../constants/ui';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { FlowImageTaskStatus } from '../../../../types';
import type { FlowImageTask, FlowImageTaskReference, GeneratedImageItem } from '../../../../types';
import { useFlowImageStore } from '../store/flowImageStore';
import { QueueToolbar } from './QueueToolbar';
import { ReferenceBankDialog } from './ReferenceBankDialog';
import { TaskRow } from './TaskRow';
import type { TaskRowActions } from './TaskRow';
import { useImageQueueStore } from './queueStore';
import { applyToTasks, overCapacity, referenceTargets } from './references';
import { useImageQueueLive } from './useImageQueueLive';
import './AllWorkspace.css';

/** The image queue (Ảnh → Hàng loạt) as the backend runs it, with its results. */
export function AllWorkspace() {
  useImageQueueLive();
  const tasks = useImageQueueStore((s) => s.tasks);
  const warning = useImageQueueStore((s) => s.warning);
  const loadError = useImageQueueStore((s) => s.loadError);
  const dismissWarning = useImageQueueStore((s) => s.dismissWarning);
  const models = useFlowImageStore((s) => s.models);
  const addToast = useAppStore((state) => state.addToast);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [previewImage, setPreviewImage] = useState<GeneratedImageItem | null>(null);
  const [gallery, setGallery] = useState<{ task: FlowImageTask; number: number } | null>(null);
  const [bankOpen, setBankOpen] = useState(false);

  const numbers = useMemo(() => {
    const byTask = new Map<string, number>();
    const byBatch = new Map<string, number>();
    tasks.forEach((task, index) => {
      byTask.set(task.id, index + 1);
      if (!byBatch.has(task.batchId)) byBatch.set(task.batchId, byBatch.size + 1);
    });
    return { byTask, byBatch };
  }, [tasks]);
  const modelNames = useMemo(() => new Map(models.map((m) => [m.id, m.name])), [models]);
  const modelLimits = useMemo(
    () => new Map(models.map((m) => [m.id, m.maxReferenceImages])),
    [models],
  );

  const notRunning = tasks.filter((task) => task.status !== FlowImageTaskStatus.Running);
  const selectedTasks = notRunning.filter((task) => selected.has(task.id));
  const retryable = selectedTasks.filter(
    (task) =>
      task.status === FlowImageTaskStatus.Failed || task.status === FlowImageTaskStatus.Cancelled,
  );
  const finished = tasks.filter((task) => task.status === FlowImageTaskStatus.Succeeded);
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
          `Model của tác vụ ${numbers.byTask.get(tooMany.id)} không đủ chỗ cho ảnh tham chiếu đã chọn.`,
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
      <div className="flow-image-all-scroll">
        <div className="flow-image-all-table" role="table" aria-label="Hàng đợi tạo ảnh">
          <div className="flow-image-all-header" role="row">
            <span className="flow-image-all-index-heading" role="columnheader">
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
            </span>
            <span role="columnheader">Tác vụ</span>
            <span role="columnheader">Prompt</span>
            <span role="columnheader">Ảnh tham chiếu</span>
            <span role="columnheader">Kết quả</span>
            <span role="columnheader">Tiến độ</span>
          </div>
          {tasks.length === 0 ? (
            <div className="flow-image-all-body" role="rowgroup">
              <div className="flow-image-all-empty-state">
                <strong>{loadError ? 'Không tải được hàng đợi' : 'Chưa có tác vụ'}</strong>
                <span>{loadError ?? 'Dán prompt vào ô ở cột bên trái rồi bấm "Thêm & chạy".'}</span>
              </div>
            </div>
          ) : (
            <RevealList
              className="flow-image-all-body"
              items={tasks}
              keyOf={(task) => task.id}
              step={LIST_REVEAL_STEP}
              render={(task) => (
                <TaskRow
                  task={task}
                  number={numbers.byTask.get(task.id) ?? 0}
                  batchNumber={numbers.byBatch.get(task.batchId) ?? 0}
                  modelName={modelNames.get(task.modelId) ?? 'Chưa có'}
                  referenceLimit={Math.max(
                    task.references.length,
                    modelLimits.get(task.modelId) ?? 0,
                  )}
                  selected={selected.has(task.id)}
                  actions={actions}
                />
              )}
            />
          )}
        </div>
      </div>

      <QueueToolbar
        selectedCount={selectedTasks.length}
        retryableCount={retryable.length}
        finishedCount={finished.length}
        removableCount={notRunning.length}
        canAddReferences={models.length > 0}
        onPickReferences={() => void pickReferences()}
        onOpenBank={() => setBankOpen(true)}
        onRemoveSelected={() =>
          void bulk(
            api.removeFlowImageTasks(selectedTasks.map((task) => task.id)),
            'Không xóa được tác vụ',
          )
        }
        onClearFinished={() =>
          void bulk(api.clearFinishedFlowImageTasks(), 'Không xóa được tác vụ đã xong')
        }
        onRetrySelected={() =>
          void bulk(
            api.retryFlowImageTasks(retryable.map((task) => task.id)),
            'Không chạy lại được tác vụ lỗi',
          )
        }
        onRemoveAll={() =>
          void bulk(
            api.removeFlowImageTasks(notRunning.map((task) => task.id)),
            'Không xóa được hàng đợi',
          )
        }
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
                <img src={reference.localUrl} alt={reference.fileName} loading="lazy" />
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
          <img src={previewImage.localUrl} alt={previewImage.prompt} />
        </Modal>
      )}
    </div>
  );
}
