import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ImagePlus, Images, X } from 'lucide-react';
import { Button } from '../../../../components/ui';
import { useAppStore } from '../../../../stores';
import { FlowImageProjectMode } from '../../../../types';
import type { FlowImageProject, FlowProjectImage, GeneratedImageItem } from '../../../../types';
import { api } from '../../../../services/api';
import { formatBytes } from '../../../../utils';
import { readReference } from '../../shared/ReferencePicker';
import { useStudioConfig } from '../../shared';
import type { ReferenceImage } from '../../shared/ReferencePicker';
import { useFlowImageStore } from '../store/flowImageStore';
import type { QueuedImageReference } from '../store/flowImageStore';
import './AllWorkspace.css';

/** Queue and per-prompt results for the image All mode. */
export function AllWorkspace() {
  const { config } = useStudioConfig();
  const tasks = useFlowImageStore((state) => state.allPrompts);
  const allProcessing = useFlowImageStore((state) => state.allProcessing);
  const models = useFlowImageStore((state) => state.models);
  const selectedModel = useFlowImageStore((state) => state.selectedModel);
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const [previewImage, setPreviewImage] = useState<GeneratedImageItem | null>(null);
  const [referenceGallery, setReferenceGallery] = useState<{
    taskNumber: number;
    maxImages: number;
    images: QueuedImageReference[];
  } | null>(null);
  const [referenceDialog, setReferenceDialog] = useState<'new' | 'bank' | null>(null);
  const [pendingReferences, setPendingReferences] = useState<ReferenceImage[]>([]);
  const [selectedReferenceIDs, setSelectedReferenceIDs] = useState<string[]>([]);
  const [flowProjects, setFlowProjects] = useState<FlowImageProject[]>([]);
  const [flowProjectImages, setFlowProjectImages] = useState<FlowProjectImage[]>([]);
  const [flowProjectKey, setFlowProjectKey] = useState('');
  const [selectedFlowImageIDs, setSelectedFlowImageIDs] = useState<string[]>([]);
  const [flowProjectsLoading, setFlowProjectsLoading] = useState(false);
  const [flowProjectImagesLoading, setFlowProjectImagesLoading] = useState(false);
  const [flowLibraryError, setFlowLibraryError] = useState('');
  const [flowLibraryRefresh, setFlowLibraryRefresh] = useState(0);
  const [applyingFlowImages, setApplyingFlowImages] = useState(false);
  const referenceInputRef = useRef<HTMLInputElement>(null);
  const addToast = useAppStore((state) => state.addToast);
  const model = models.find((entry) => entry.id === selectedModel);
  const applicableTasks = tasks.filter(
    (task) => task.status === 'queued' || task.status === 'failed',
  );
  const selectableTasks = allProcessing ? [] : tasks.filter((task) => task.status !== 'processing');
  const selectedTasks = selectableTasks.filter((task) => selectedIDs.includes(task.id));
  const selectedFailedTasks = selectedTasks.filter(
    (task) => task.status === 'failed' || task.status === 'save-failed',
  );
  const completedTasks = allProcessing ? [] : tasks.filter((task) => task.status === 'completed');
  const areAllSelectableTasksSelected =
    selectableTasks.length > 0 && selectableTasks.every((task) => selectedIDs.includes(task.id));
  const batchNumbers = new Map<string, number>();
  for (const task of tasks) {
    if (!batchNumbers.has(task.batchId)) batchNumbers.set(task.batchId, batchNumbers.size + 1);
  }
  const selectedFlowProject = flowProjects.find(
    (project) => `${project.accountId}/${project.projectId}` === flowProjectKey,
  );
  const selectedFlowAccountID = selectedFlowProject?.accountId;
  const selectedFlowProjectID = selectedFlowProject?.projectId;
  const selectedFlowProjectMode = selectedFlowProject?.mode;

  const toggleTask = (id: string, checked: boolean) => {
    setSelectedIDs((current) =>
      checked ? [...new Set([...current, id])] : current.filter((selected) => selected !== id),
    );
  };

  const removeTasks = (ids: string[]) => {
    useFlowImageStore.getState().removeAllPrompts(ids);
    setSelectedIDs((current) => current.filter((id) => !ids.includes(id)));
  };

  const discardReferenceDialog = useCallback(() => {
    if (referenceDialog === 'new') {
      pendingReferences.forEach((image) => URL.revokeObjectURL(image.previewUrl));
    }
    setReferenceDialog(null);
    setPendingReferences([]);
    setSelectedReferenceIDs([]);
    setSelectedFlowImageIDs([]);
  }, [pendingReferences, referenceDialog]);

  const referenceCapacity = () => {
    const current = useFlowImageStore.getState();
    const currentModel = current.models.find((entry) => entry.id === current.selectedModel);
    if (!config || !currentModel) return 0;
    let remaining = currentModel.maxReferenceImages - current.referenceImages.length;
    for (const task of current.allPrompts) {
      if (task.status !== 'queued' && task.status !== 'failed') continue;
      const taskModel = current.models.find(
        (entry) => entry.id === (task.modelId || current.selectedModel),
      );
      if (!taskModel) return 0;
      remaining = Math.min(remaining, taskModel.maxReferenceImages - task.references.length);
    }
    return Math.max(0, remaining);
  };

  const addSelectedFiles = async (files: File[]) => {
    const current = useFlowImageStore.getState();
    const currentModel = current.models.find((entry) => entry.id === current.selectedModel);
    if (!config || !currentModel) {
      addToast({
        type: 'error',
        title: 'Chưa sẵn sàng thêm ảnh',
        message: 'Cần tải cấu hình và model trước khi chọn ảnh tham chiếu.',
      });
      return;
    }

    const skipped: string[] = [];
    const accepted: File[] = [];
    const capacity = referenceCapacity();
    for (const file of files) {
      if (!config.referenceMimeTypes.includes(file.type)) {
        skipped.push(`${file.name}: định dạng không hỗ trợ`);
      } else if (file.size > config.maxReferenceBytes) {
        skipped.push(`${file.name}: lớn hơn ${formatBytes(config.maxReferenceBytes, 0)}`);
      } else if (accepted.length >= capacity) {
        skipped.push(`${file.name}: đã đạt giới hạn ảnh tham chiếu`);
      } else {
        accepted.push(file);
      }
    }

    const loaded = await Promise.allSettled(accepted.map(readReference));
    const images: ReferenceImage[] = [];
    for (const result of loaded) {
      if (result.status === 'fulfilled') images.push(result.value);
      else
        skipped.push(
          result.reason instanceof Error ? result.reason.message : String(result.reason),
        );
    }
    if (images.length > 0) {
      setPendingReferences(images);
      setSelectedReferenceIDs(images.map((image) => image.previewUrl));
      setReferenceDialog('new');
    }
    if (skipped.length > 0) {
      addToast({
        type: 'warning',
        title: 'Một số ảnh không được thêm',
        message: skipped.join('; '),
      });
    }
  };

  const openImageBank = () => {
    setPendingReferences([]);
    setSelectedReferenceIDs([]);
    setFlowProjects([]);
    setFlowProjectImages([]);
    setFlowProjectKey('');
    setSelectedFlowImageIDs([]);
    setFlowLibraryError('');
    setFlowProjectsLoading(true);
    setReferenceDialog('bank');
  };

  const selectFlowProject = (projectKey: string) => {
    setFlowProjectKey(projectKey);
    setFlowProjectImages([]);
    setSelectedFlowImageIDs([]);
    setFlowLibraryError('');
    setFlowProjectImagesLoading(Boolean(projectKey));
  };

  const retryFlowLibrary = () => {
    setFlowProjectsLoading(true);
    setFlowProjectImagesLoading(Boolean(flowProjectKey));
    setFlowLibraryError('');
    setFlowLibraryRefresh((value) => value + 1);
  };

  const applyReferencesToPrompts = (references?: ReferenceImage[]): boolean => {
    const current = useFlowImageStore.getState();
    if (current.allProcessing) return false;
    const available =
      references ?? (referenceDialog === 'new' ? pendingReferences : current.referenceImages);
    const selected = references
      ? references
      : available.filter((image) => selectedReferenceIDs.includes(image.previewUrl));
    if (selected.length === 0) return false;

    const additions: QueuedImageReference[] = selected.map(({ base64, mimeType, name }) => ({
      base64,
      mimeType,
      name,
    }));
    const updatedTasks = new Map<string, QueuedImageReference[]>();
    const applicable = current.allPrompts.filter(
      (task) => task.status === 'queued' || task.status === 'failed',
    );
    for (const task of applicable) {
      const nextReferences = [...task.references];
      for (const reference of additions) {
        if (!nextReferences.some((existing) => existing.base64 === reference.base64)) {
          nextReferences.push(reference);
        }
      }
      const taskModel = current.models.find(
        (entry) => entry.id === (task.modelId || current.selectedModel),
      );
      if (!taskModel || nextReferences.length > taskModel.maxReferenceImages) {
        addToast({
          type: 'error',
          title: 'Không thể thêm ảnh cho tất cả prompts',
          message: `Model của tác vụ ${current.allPrompts.indexOf(task) + 1} không đủ sức chứa ảnh tham chiếu đã chọn.`,
        });
        return false;
      }
      updatedTasks.set(task.id, nextReferences);
    }

    const currentReferenceIDs = new Set(current.referenceImages.map((image) => image.base64));
    const newReferences = selected.filter((image) => !currentReferenceIDs.has(image.base64));
    current.addReferenceImages(newReferences);
    for (const [taskID, references] of updatedTasks) {
      current.updateAllPrompt(taskID, { references });
    }

    const selectedURLs = new Set(selected.map((image) => image.previewUrl));
    pendingReferences
      .filter(
        (image) => !selectedURLs.has(image.previewUrl) || currentReferenceIDs.has(image.base64),
      )
      .forEach((image) => URL.revokeObjectURL(image.previewUrl));
    const retainedPreviewURLs = new Set(
      [...current.referenceImages, ...newReferences].map((image) => image.previewUrl),
    );
    selected
      .filter((image) => !retainedPreviewURLs.has(image.previewUrl))
      .forEach((image) => URL.revokeObjectURL(image.previewUrl));
    setPendingReferences([]);
    setSelectedReferenceIDs([]);
    setSelectedFlowImageIDs([]);
    setReferenceDialog(null);
    addToast({
      type: 'success',
      title: 'Đã thêm ảnh tham chiếu',
      message:
        updatedTasks.size > 0
          ? `Đã áp dụng ${selected.length} ảnh cho ${updatedTasks.size} prompts đang chờ xử lý hoặc chạy lại.`
          : `Đã thêm ${selected.length} ảnh vào phần Ảnh tham chiếu.`,
    });
    return true;
  };

  const applyFlowProjectImages = async () => {
    if (!selectedFlowProject || selectedFlowImageIDs.length === 0 || applyingFlowImages) return;
    setApplyingFlowImages(true);
    const selected = flowProjectImages.filter((image) => selectedFlowImageIDs.includes(image.id));
    const loaded = await Promise.allSettled(
      selected.map(async (image) => {
        const response = await api.getFlowProjectImageReference(
          selectedFlowProject.accountId,
          selectedFlowProject.projectId,
          selectedFlowProject.mode,
          image.id,
        );
        if (!response.success || !response.data) {
          throw new Error(response.error ?? `Không tải được ảnh ${image.name} từ Flow.`);
        }
        const bytes = Uint8Array.from(atob(response.data.base64), (character) =>
          character.charCodeAt(0),
        );
        const previewUrl = URL.createObjectURL(new Blob([bytes], { type: response.data.mimeType }));
        return {
          base64: response.data.base64,
          mimeType: response.data.mimeType,
          name: response.data.fileName || image.name,
          previewUrl,
        } satisfies ReferenceImage;
      }),
    );
    setApplyingFlowImages(false);
    const references = loaded.flatMap((result) =>
      result.status === 'fulfilled' ? [result.value] : [],
    );
    const errors = loaded.flatMap((result) =>
      result.status === 'rejected'
        ? [result.reason instanceof Error ? result.reason.message : String(result.reason)]
        : [],
    );
    if (references.length > 0 && !applyReferencesToPrompts(references)) {
      references.forEach((image) => URL.revokeObjectURL(image.previewUrl));
    }
    if (errors.length > 0) {
      addToast({
        type: 'warning',
        title: 'Một số ảnh không tải được từ Flow',
        message: errors.join('; '),
      });
    }
  };

  useEffect(() => {
    if (!previewImage) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setPreviewImage(null);
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [previewImage]);

  useEffect(() => {
    if (!referenceGallery) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setReferenceGallery(null);
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [referenceGallery]);

  useEffect(() => {
    if (!referenceDialog) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') discardReferenceDialog();
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [discardReferenceDialog, referenceDialog]);

  useEffect(() => {
    if (referenceDialog !== 'bank') return;
    let active = true;
    void api.listFlowImageProjects(FlowImageProjectMode.All).then((response) => {
      if (!active) return;
      if (!response.success || !response.data) {
        setFlowLibraryError(response.error ?? 'Không tải được danh sách project Flow.');
        setFlowProjects([]);
        setFlowProjectKey('');
        setFlowProjectImages([]);
        setFlowProjectImagesLoading(false);
        setFlowProjectsLoading(false);
        return;
      }
      setFlowProjects(response.data);
      const target = response.data[0];
      setFlowProjectKey(target ? `${target.accountId}/${target.projectId}` : '');
      setFlowProjectImages([]);
      setSelectedFlowImageIDs([]);
      setFlowProjectImagesLoading(Boolean(target));
      setFlowLibraryError('');
      setFlowProjectsLoading(false);
    });
    return () => {
      active = false;
    };
  }, [flowLibraryRefresh, referenceDialog]);

  useEffect(() => {
    if (
      referenceDialog !== 'bank' ||
      !selectedFlowAccountID ||
      !selectedFlowProjectID ||
      !selectedFlowProjectMode
    )
      return;
    let active = true;
    void api
      .listFlowProjectImages(selectedFlowAccountID, selectedFlowProjectID, selectedFlowProjectMode)
      .then((response) => {
        if (!active) return;
        if (!response.success || !response.data) {
          setFlowLibraryError(response.error ?? 'Không tải được tệp ảnh trong project Flow.');
          setFlowProjectImages([]);
        } else {
          setFlowProjectImages(response.data);
        }
        setFlowProjectImagesLoading(false);
      });
    return () => {
      active = false;
    };
  }, [
    flowLibraryRefresh,
    referenceDialog,
    selectedFlowAccountID,
    selectedFlowProjectID,
    selectedFlowProjectMode,
  ]);

  return (
    <div className="flow-workspace-container flow-image-all-workspace">
      <div className="flow-image-all-scroll">
        <div className="flow-image-all-table" role="table" aria-label="Hàng đợi tạo ảnh">
          <div className="flow-image-all-header" role="row">
            <span className="flow-image-all-index-heading" role="columnheader">
              <input
                type="checkbox"
                aria-label="Chọn tất cả tác vụ"
                checked={areAllSelectableTasksSelected}
                disabled={selectableTasks.length === 0}
                onChange={(event) =>
                  setSelectedIDs(
                    event.currentTarget.checked ? selectableTasks.map((task) => task.id) : [],
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
          <div className="flow-image-all-body" role="rowgroup">
            {tasks.length === 0 ? (
              <div className="flow-image-all-empty-state">
                <strong>Chưa có tác vụ</strong>
                <span>Nhập danh sách prompt ở bên trái để thêm vào hàng đợi.</span>
              </div>
            ) : (
              tasks.map((task, index) => {
                const referenceModel = models.find(
                  (entry) => entry.id === (task.modelId || selectedModel),
                );
                const referenceLimit = Math.max(
                  task.references.length,
                  referenceModel?.maxReferenceImages ?? 0,
                );
                const displayedReferenceCount = Math.min(task.references.length, 2);
                const referenceOverflow = Math.max(referenceLimit - displayedReferenceCount, 0);
                return (
                  <div
                    className={`flow-image-all-card${selectedIDs.includes(task.id) ? ' is-selected' : ''}`}
                    role="row"
                    key={task.id}
                  >
                    <span className="flow-image-all-index" role="cell">
                      {index + 1}
                      <input
                        type="checkbox"
                        aria-label={`Chọn tác vụ ${index + 1}`}
                        checked={selectedIDs.includes(task.id)}
                        disabled={allProcessing || task.status === 'processing'}
                        onChange={(event) => toggleTask(task.id, event.currentTarget.checked)}
                      />
                    </span>

                    <div className="flow-image-all-task-meta" role="cell">
                      <strong>Task {batchNumbers.get(task.batchId)}</strong>
                      <span>
                        {models.find((model) => model.id === (task.modelId || selectedModel))
                          ?.name ?? 'Chưa có'}
                      </span>
                      <span>{task.aspectRatio || 'Chưa có'}</span>
                      <span>{task.count} ảnh / prompt</span>
                    </div>

                    <span className="flow-image-all-prompt" role="cell" title={task.prompt}>
                      {task.prompt}
                    </span>

                    <div className="flow-image-all-references" role="cell">
                      {task.references.length > 0 ? (
                        <>
                          {task.references.slice(0, 2).map((reference, referenceIndex) => (
                            <img
                              className="flow-image-all-reference-thumb"
                              key={`${task.id}-reference-${referenceIndex}`}
                              src={`data:${reference.mimeType};base64,${reference.base64}`}
                              alt={reference.name}
                              title={reference.name}
                              loading="lazy"
                            />
                          ))}
                          {referenceOverflow > 0 && (
                            <button
                              type="button"
                              className="flow-image-all-reference-overflow"
                              aria-label={`Xem ảnh tham chiếu của tác vụ ${index + 1}: ${task.references.length} đã chọn, tối đa ${referenceLimit}`}
                              title={`${task.references.length}/${referenceLimit} ảnh đã chọn`}
                              onClick={() =>
                                setReferenceGallery({
                                  taskNumber: index + 1,
                                  maxImages: referenceLimit,
                                  images: task.references,
                                })
                              }
                            >
                              +{referenceOverflow}
                            </button>
                          )}
                        </>
                      ) : (
                        <span className="flow-image-all-reference-empty">Chưa có</span>
                      )}
                    </div>

                    <div className="flow-image-all-results" role="cell">
                      {task.results.length > 0 ? (
                        <>
                          {task.results.map((result, resultIndex) =>
                            result.localUrl ? (
                              <button
                                type="button"
                                className="flow-image-all-preview-trigger"
                                key={`${task.id}-result-${result.id || resultIndex}`}
                                aria-label="Phóng to ảnh kết quả"
                                title="Xem ảnh lớn"
                                onClick={() => setPreviewImage(result)}
                              >
                                <img
                                  className="flow-image-all-result-thumb"
                                  src={result.localUrl}
                                  alt={result.prompt || task.prompt}
                                  loading="lazy"
                                />
                              </button>
                            ) : null,
                          )}
                          {task.results.every((result) => !result.localUrl) && (
                            <span
                              className="flow-image-all-result-error"
                              title={
                                task.error || task.results.find((result) => result.error)?.error
                              }
                            >
                              {task.error ||
                                task.results.find((result) => result.error)?.error ||
                                'Không có ảnh kết quả'}
                            </span>
                          )}
                        </>
                      ) : (
                        <span className="flow-image-all-empty-value">—</span>
                      )}
                    </div>

                    <div className="flow-image-all-progress" role="cell">
                      <span className={`flow-image-all-status is-${task.status}`}>
                        {task.status === 'queued' && 'Chờ xử lý'}
                        {task.status === 'processing' && (
                          <>
                            <span className="flow-image-all-spinner" aria-hidden="true" />
                            Đang xử lý
                          </>
                        )}
                        {task.status === 'completed' && 'Hoàn tất'}
                        {task.status === 'failed' && 'Thất bại'}
                        {task.status === 'save-failed' && 'Lỗi lưu ảnh'}
                      </span>
                      {(task.status === 'failed' || task.status === 'save-failed') &&
                        task.error && (
                          <span className="flow-image-all-task-error" title={task.error}>
                            {task.error}
                          </span>
                        )}
                      {task.status === 'completed' && task.savedLocalURLs.length > 0 && (
                        <span className="flow-image-all-empty-value">
                          Đã lưu {task.savedLocalURLs.length} ảnh
                        </span>
                      )}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>

      <div className="flow-image-all-toolbar" aria-label="Thao tác hàng đợi">
        <span className="flow-image-all-selection-count">Đã chọn {selectedTasks.length}</span>
        <div className="flow-image-all-toolbar-actions">
          <input
            ref={referenceInputRef}
            className="flow-image-all-reference-input"
            type="file"
            accept={config?.referenceMimeTypes.join(',')}
            multiple
            tabIndex={-1}
            aria-label="Chọn ảnh tham chiếu"
            onChange={(event) => {
              const files = Array.from(event.currentTarget.files ?? []);
              event.currentTarget.value = '';
              void addSelectedFiles(files);
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={allProcessing || !config || !model || referenceCapacity() === 0}
            onClick={() => referenceInputRef.current?.click()}
          >
            <ImagePlus size={14} />
            Thêm ảnh
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={allProcessing}
            onClick={openImageBank}
          >
            <Images size={14} />
            Kho ảnh
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={selectedTasks.length === 0}
            onClick={() => removeTasks(selectedTasks.map((task) => task.id))}
          >
            Xóa đã chọn
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={completedTasks.length === 0}
            onClick={() => removeTasks(completedTasks.map((task) => task.id))}
          >
            Xóa đã xong
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={selectedFailedTasks.length === 0}
            onClick={() => {
              const state = useFlowImageStore.getState();
              for (const task of selectedFailedTasks) {
                state.updateAllPrompt(task.id, {
                  status: task.status === 'save-failed' ? 'save-failed' : 'queued',
                  ...(task.status === 'save-failed'
                    ? {}
                    : { results: [], savedLocalURLs: [], error: undefined }),
                });
              }
              addToast({
                type: 'info',
                title: 'Đã đưa tác vụ lỗi vào hàng chờ',
                message: `Đã đưa ${selectedFailedTasks.length} tác vụ về trạng thái chờ.`,
              });
            }}
          >
            Chờ chạy lại lỗi
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={selectableTasks.length === 0}
            onClick={() => {
              if (!window.confirm('Xóa tất cả tác vụ khỏi hàng đợi?')) return;
              removeTasks(selectableTasks.map((task) => task.id));
            }}
          >
            Xóa queue
          </Button>
        </div>
      </div>

      {referenceDialog &&
        createPortal(
          <div className="flow-image-all-reference-overlay" onMouseDown={discardReferenceDialog}>
            <section
              className="flow-image-all-reference-dialog"
              role="dialog"
              aria-modal="true"
              aria-labelledby="flow-image-all-reference-title"
              onMouseDown={(event) => event.stopPropagation()}
            >
              <header className="flow-image-all-reference-dialog-header">
                <div>
                  <h2 id="flow-image-all-reference-title">
                    {referenceDialog === 'new' ? 'Thêm ảnh tham chiếu' : 'Kho ảnh'}
                  </h2>
                  <p>
                    {referenceDialog === 'new'
                      ? 'Chọn ảnh muốn áp dụng cho các prompts đang chờ hoặc cần chạy lại.'
                      : 'Chọn project để xem ảnh đã tải lên Flow và áp dụng cho các prompts đang chờ hoặc cần chạy lại.'}
                  </p>
                </div>
                <button
                  type="button"
                  className="flow-image-all-reference-dialog-close"
                  aria-label="Đóng"
                  onClick={discardReferenceDialog}
                >
                  <X size={18} />
                </button>
              </header>

              {referenceDialog === 'new' ? (
                pendingReferences.length > 0 ? (
                  <div className="flow-image-all-reference-bank-grid">
                    {pendingReferences.map((image) => (
                      <label className="flow-image-all-reference-bank-item" key={image.previewUrl}>
                        <input
                          type="checkbox"
                          checked={selectedReferenceIDs.includes(image.previewUrl)}
                          onChange={(event) => {
                            const checked = event.currentTarget.checked;
                            setSelectedReferenceIDs((current) =>
                              checked
                                ? [...current, image.previewUrl]
                                : current.filter((id) => id !== image.previewUrl),
                            );
                          }}
                        />
                        <img src={image.previewUrl} alt={image.name} />
                        <span title={image.name}>{image.name}</span>
                      </label>
                    ))}
                  </div>
                ) : (
                  <div className="flow-image-all-reference-bank-empty">
                    <p>Chưa có ảnh được chọn.</p>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => referenceInputRef.current?.click()}
                    >
                      <ImagePlus size={14} />
                      Chọn ảnh
                    </Button>
                  </div>
                )
              ) : flowProjectsLoading ? (
                <div className="flow-image-all-reference-bank-empty" role="status">
                  <p>Đang tải project Flow…</p>
                </div>
              ) : flowProjects.length === 0 ? (
                <div className="flow-image-all-reference-bank-empty">
                  <p>{flowLibraryError || 'Không tìm thấy project Flow nào.'}</p>
                  <Button type="button" variant="outline" size="sm" onClick={retryFlowLibrary}>
                    Tải lại
                  </Button>
                </div>
              ) : (
                <div className="flow-image-all-flow-library">
                  <label className="flow-image-all-flow-project-select">
                    <span>Project Flow</span>
                    <select
                      value={flowProjectKey}
                      onChange={(event) => selectFlowProject(event.currentTarget.value)}
                    >
                      {flowProjects.map((project) => (
                        <option
                          key={`${project.accountId}/${project.projectId}`}
                          value={`${project.accountId}/${project.projectId}`}
                        >
                          {project.accountEmail} · {project.title || project.projectId}
                        </option>
                      ))}
                    </select>
                  </label>
                  {flowProjectImagesLoading ? (
                    <div className="flow-image-all-reference-bank-empty" role="status">
                      <p>Đang tải ảnh của project…</p>
                    </div>
                  ) : flowLibraryError ? (
                    <div className="flow-image-all-reference-bank-empty" role="alert">
                      <p>{flowLibraryError}</p>
                      <Button type="button" variant="outline" size="sm" onClick={retryFlowLibrary}>
                        Tải lại
                      </Button>
                    </div>
                  ) : flowProjectImages.length > 0 ? (
                    <div className="flow-image-all-reference-bank-grid">
                      {flowProjectImages.map((image) => (
                        <label className="flow-image-all-reference-bank-item" key={image.id}>
                          <input
                            type="checkbox"
                            checked={selectedFlowImageIDs.includes(image.id)}
                            onChange={(event) => {
                              const checked = event.currentTarget.checked;
                              setSelectedFlowImageIDs((current) =>
                                checked
                                  ? [...current, image.id]
                                  : current.filter((id) => id !== image.id),
                              );
                            }}
                          />
                          <img src={image.previewUrl} alt={image.name} loading="lazy" />
                          <span title={image.name}>{image.name}</span>
                        </label>
                      ))}
                    </div>
                  ) : (
                    <div className="flow-image-all-reference-bank-empty">
                      <p>Project này chưa có ảnh được Flow trả về.</p>
                    </div>
                  )}
                </div>
              )}

              <footer className="flow-image-all-reference-dialog-footer">
                <span>
                  {referenceDialog === 'bank'
                    ? selectedFlowImageIDs.length
                    : selectedReferenceIDs.length}{' '}
                  ảnh được chọn
                </span>
                <div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={discardReferenceDialog}
                  >
                    Hủy
                  </Button>
                  <Button
                    type="button"
                    variant="primary"
                    size="sm"
                    disabled={
                      (referenceDialog === 'bank'
                        ? selectedFlowImageIDs.length === 0
                        : selectedReferenceIDs.length === 0) ||
                      allProcessing ||
                      applyingFlowImages
                    }
                    onClick={() =>
                      referenceDialog === 'bank'
                        ? void applyFlowProjectImages()
                        : applyReferencesToPrompts()
                    }
                  >
                    {applyingFlowImages
                      ? 'Đang tải ảnh…'
                      : applicableTasks.length > 0
                        ? 'Thêm ảnh cho tất cả prompts'
                        : 'Thêm vào ảnh tham chiếu'}
                  </Button>
                </div>
              </footer>
            </section>
          </div>,
          document.body,
        )}

      {referenceGallery &&
        createPortal(
          <div
            className="flow-image-all-gallery-overlay"
            onMouseDown={() => setReferenceGallery(null)}
          >
            <section
              className="flow-image-all-gallery-dialog"
              role="dialog"
              aria-modal="true"
              aria-labelledby="flow-image-all-gallery-title"
              onMouseDown={(event) => event.stopPropagation()}
            >
              <header className="flow-image-all-gallery-header">
                <div>
                  <h2 id="flow-image-all-gallery-title">
                    Ảnh tham chiếu · Tác vụ {referenceGallery.taskNumber}
                  </h2>
                  <p>
                    {referenceGallery.images.length}/{referenceGallery.maxImages} ảnh đã chọn
                  </p>
                </div>
                <button
                  type="button"
                  className="flow-image-all-gallery-close"
                  aria-label="Đóng"
                  onClick={() => setReferenceGallery(null)}
                >
                  <X size={18} />
                </button>
              </header>
              <div className="flow-image-all-gallery-grid">
                {referenceGallery.images.map((reference, referenceIndex) => (
                  <figure
                    className="flow-image-all-gallery-item"
                    key={`${referenceGallery.taskNumber}-${referenceIndex}`}
                  >
                    <img
                      src={`data:${reference.mimeType};base64,${reference.base64}`}
                      alt={reference.name}
                    />
                    <figcaption title={reference.name}>{reference.name}</figcaption>
                  </figure>
                ))}
              </div>
            </section>
          </div>,
          document.body,
        )}

      {previewImage?.localUrl && (
        <div className="flow-image-all-preview-overlay" onClick={() => setPreviewImage(null)}>
          <div
            className="flow-image-all-preview-dialog"
            role="dialog"
            aria-modal="true"
            aria-label="Ảnh kết quả phóng to"
            onClick={(event) => event.stopPropagation()}
          >
            <button
              type="button"
              className="flow-image-all-preview-close"
              aria-label="Đóng ảnh"
              title="Đóng"
              onClick={() => setPreviewImage(null)}
            >
              <X size={18} />
            </button>
            <img src={previewImage.localUrl} alt={previewImage.prompt} />
          </div>
        </div>
      )}
    </div>
  );
}
