import React, { useEffect, useState } from 'react';
import { FolderOpen } from 'lucide-react';
import { api } from '../../../../services/api';
import { Button } from '../../../../components/ui';
import { useAppStore } from '../../../../stores';
import { FlowImageProjectMode, FlowImageQuality, GenerateImageRequest } from '../../../../types';
import type { FlowImageQuality as FlowImageQualityType } from '../../../../types';
import {
  FlowModePanel,
  ReferencePicker,
  SelectField,
  ratioLabel,
  selectedModelOf,
  useModelCatalog,
  useStudioConfig,
} from '../../shared';
import {
  ALL_IMAGE_REFERENCE_MODES,
  ALL_IMAGE_WAIT_MODES,
  MAX_ALL_IMAGE_THREADS,
  useFlowImageStore,
} from '../store/flowImageStore';
import type { AllImageReferenceMode, AllImageWaitMode } from '../store/flowImageStore';
import { PromptListModal } from './PromptListModal';
import './AllModePanel.css';

const DEFAULT_IMAGE_QUALITIES = [FlowImageQuality.OneK];

/** All-mode image settings; shared model and reference choices, with its own ratio and thread count. */
export const AllModePanel: React.FC = () => {
  const { config, error: configError } = useStudioConfig();
  const store = useFlowImageStore();
  const allImageQuality = useFlowImageStore((state) => state.allImageQuality);
  const setAllImageQuality = useFlowImageStore((state) => state.setAllImageQuality);
  const model = useFlowImageStore(selectedModelOf);
  const addToast = useAppStore((state) => state.addToast);
  const { loading, error } = useModelCatalog(api.getImageModels, store.setModels);
  const [promptListOpen, setPromptListOpen] = useState(false);
  const [readyAccountCount, setReadyAccountCount] = useState<number | null>(null);
  const [expandedConfig, setExpandedConfig] = useState<'basic' | 'advanced' | null>('basic');
  const hasTasksToProcess = store.allPrompts.some(
    (task) => task.status === 'queued' || task.status === 'failed' || task.status === 'save-failed',
  );
  const taskCounts = {
    processing: store.allPrompts.filter((task) => task.status === 'processing').length,
    queued: store.allPrompts.filter((task) => task.status === 'queued').length,
    completed: store.allPrompts.filter((task) => task.status === 'completed').length,
    failed: store.allPrompts.filter(
      (task) => task.status === 'failed' || task.status === 'save-failed',
    ).length,
  };
  const referenceLimit = model?.maxReferenceImages;
  const availableImageQualities = model?.availableQualities?.length
    ? model.availableQualities
    : DEFAULT_IMAGE_QUALITIES;

  useEffect(() => {
    let active = true;
    void api.getActiveRotationCount().then((response) => {
      if (active && response.success && typeof response.data === 'number') {
        setReadyAccountCount(response.data);
      }
    });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (store.imageOutputDirectory) return;
    let active = true;
    void api.getDefaultImageOutputDirectory().then((response) => {
      if (!active) return;
      if (response.success && response.data) {
        useFlowImageStore.getState().setImageOutputDirectory(response.data);
        return;
      }
      addToast({
        type: 'error',
        title: 'Không tải được folder lưu mặc định',
        message: response.error ?? 'Không rõ lỗi.',
      });
    });
    return () => {
      active = false;
    };
  }, [addToast, store.imageOutputDirectory]);

  useEffect(() => {
    if (!availableImageQualities.includes(allImageQuality)) {
      setAllImageQuality(
        availableImageQualities.includes(FlowImageQuality.OneK)
          ? FlowImageQuality.OneK
          : (availableImageQualities[0] as FlowImageQualityType),
      );
    }
  }, [allImageQuality, availableImageQualities, setAllImageQuality]);

  const startProcessing = async () => {
    const current = useFlowImageStore.getState();
    const pendingTasks = current.allPrompts.filter(
      (task) =>
        task.status === 'queued' || task.status === 'failed' || task.status === 'save-failed',
    );
    if (current.allProcessing || pendingTasks.length === 0) return;
    if (pendingTasks.some((task) => !task.modelId && !current.selectedModel)) {
      addToast({
        type: 'error',
        title: 'Chưa có model ảnh',
        message: 'Kiểm tra tài khoản ở mục Account rồi thử lại.',
      });
      return;
    }

    let defaultOutputDirectory = current.imageOutputDirectory;
    if (!defaultOutputDirectory) {
      const response = await api.getDefaultImageOutputDirectory();
      if (!response.success || !response.data) {
        addToast({
          type: 'error',
          title: 'Không tải được folder lưu mặc định',
          message: response.error ?? 'Không rõ lỗi.',
        });
        return;
      }
      defaultOutputDirectory = response.data;
      current.setImageOutputDirectory(defaultOutputDirectory);
    }

    const batchOutputDirectories = new Map<string, string>();
    for (const task of pendingTasks) {
      if (batchOutputDirectories.has(task.batchId)) continue;
      const previousBatchTask = current.allPrompts.find(
        (candidate) =>
          candidate.batchId === task.batchId && candidate.outputDirectory !== undefined,
      );
      batchOutputDirectories.set(
        task.batchId,
        previousBatchTask?.outputDirectory ?? defaultOutputDirectory,
      );
    }
    const queue = pendingTasks.map((task) => {
      const outputDirectory = batchOutputDirectories.get(task.batchId) ?? '';
      if (task.outputDirectory === undefined) {
        useFlowImageStore.getState().updateAllPrompt(task.id, { outputDirectory });
      }
      return { ...task, outputDirectory };
    });

    const referenceMode = current.allReferenceMode;
    const threadCount =
      referenceMode === 'sequential' ? 1 : Math.min(current.allThreads, queue.length);
    const waitMode = current.allWaitMode;
    let nextIndex = 0;
    // Reserve each launch in one shared schedule so workers cannot start together.
    let nextLaunchAt = 0;
    let completedCount = 0;
    const failed: { prompt: string; message: string }[] = [];

    const waitForLaunchSlot = async () => {
      const waitMs = randomWaitMilliseconds(waitMode);
      if (waitMs === null) return false;
      const launchAt = Math.max(Date.now(), nextLaunchAt);
      nextLaunchAt = launchAt + waitMs;
      return waitUntil(launchAt);
    };

    current.setAllProcessing(true);
    current.setAllPaused(false);
    current.setAllStopRequested(false);
    addToast({
      type: 'info',
      title: 'Đang xử lý Queue',
      message: `${queue.length} prompt với ${threadCount} luồng.`,
    });

    const runWorker = async () => {
      while (nextIndex < queue.length) {
        const controls = useFlowImageStore.getState();
        if (controls.allPaused || controls.allStopRequested) return;
        if (!(await waitForLaunchSlot())) return;
        const launchControls = useFlowImageStore.getState();
        if (launchControls.allPaused || launchControls.allStopRequested) return;
        if (nextIndex >= queue.length) return;
        const task = queue[nextIndex++];
        const isSaveRetry = task.status === 'save-failed' && task.results.length > 0;
        const outputDirectory = task.outputDirectory;
        useFlowImageStore.getState().updateAllPrompt(task.id, {
          status: 'processing',
          ...(isSaveRetry
            ? { saveError: undefined, error: undefined }
            : {
                results: [],
                savedLocalURLs: [],
                outputDirectory,
                error: undefined,
                saveError: undefined,
              }),
        });
        try {
          if (isSaveRetry) {
            const saveResult = await saveTaskImages(
              task,
              task.results,
              outputDirectory,
              task.savedLocalURLs,
            );
            if (saveResult.errors.length > 0) {
              const message = saveResult.errors[0];
              useFlowImageStore.getState().updateAllPrompt(task.id, {
                status: 'save-failed',
                outputDirectory,
                savedLocalURLs: saveResult.savedLocalURLs,
                saveError: message,
                error: message,
              });
              failed.push({ prompt: task.prompt, message });
              continue;
            }
            useFlowImageStore.getState().updateAllPrompt(task.id, {
              status: 'completed',
              outputDirectory,
              savedLocalURLs: saveResult.savedLocalURLs,
              saveError: undefined,
              error: undefined,
            });
            completedCount += 1;
            continue;
          }

          const response = await api.generateImages(
            GenerateImageRequest.createFrom({
              prompt: task.prompt,
              modelId: task.modelId || current.selectedModel,
              aspectRatio: task.aspectRatio || current.allAspectRatio,
              quality: task.quality,
              projectMode: FlowImageProjectMode.All,
              count: task.count,
              references: task.references.map((image) => ({
                base64: image.base64,
                mimeType: image.mimeType,
                fileName: image.name,
              })),
            }),
          );
          if (!response.success || !response.data) {
            const message = response.error ?? 'Không rõ lỗi.';
            useFlowImageStore
              .getState()
              .updateAllPrompt(task.id, { status: 'failed', error: message, outputDirectory });
            failed.push({ prompt: task.prompt, message });
            continue;
          }
          current.addItems(response.data);
          const firstError = response.data.find((item) => item.error)?.error;
          if (response.data.length === 0 || response.data.every((item) => item.error)) {
            const message = firstError ?? 'Không tạo được ảnh.';
            useFlowImageStore.getState().updateAllPrompt(task.id, {
              status: 'failed',
              results: response.data,
              error: message,
              outputDirectory,
            });
            failed.push({ prompt: task.prompt, message });
            continue;
          }
          const saveResult = await saveTaskImages(task, response.data, outputDirectory, []);
          if (saveResult.errors.length > 0) {
            const message = saveResult.errors[0];
            useFlowImageStore.getState().updateAllPrompt(task.id, {
              status: 'save-failed',
              results: response.data,
              outputDirectory,
              savedLocalURLs: saveResult.savedLocalURLs,
              saveError: message,
              error: message,
            });
            failed.push({ prompt: task.prompt, message });
            continue;
          }
          useFlowImageStore.getState().updateAllPrompt(task.id, {
            status: 'completed',
            results: response.data,
            outputDirectory,
            savedLocalURLs: saveResult.savedLocalURLs,
            error: firstError,
            saveError: undefined,
          });
          completedCount += 1;
        } catch (cause) {
          const message = cause instanceof Error ? cause.message : String(cause);
          useFlowImageStore
            .getState()
            .updateAllPrompt(task.id, { status: 'failed', error: message, outputDirectory });
          failed.push({ prompt: task.prompt, message });
        }
      }
    };

    try {
      await Promise.all(Array.from({ length: threadCount }, () => runWorker()));
    } finally {
      useFlowImageStore.getState().setAllProcessing(false);
    }

    const controls = useFlowImageStore.getState();
    if (controls.allPaused) {
      addToast({
        type: 'info',
        title: 'Đã tạm dừng Queue',
        message: `${completedCount} prompt hoàn tất; các tác vụ còn lại đang chờ.`,
      });
    } else if (controls.allStopRequested) {
      addToast({
        type: 'info',
        title: 'Đã dừng Queue',
        message: `${completedCount} prompt hoàn tất; các tác vụ chưa chạy vẫn được giữ lại.`,
      });
    } else if (failed.length > 0) {
      addToast({
        type: 'warning',
        title: 'Queue xử lý chưa hoàn tất',
        message: `${failed.length} prompt vẫn ở trong Queue. ${failed[0].message}`,
      });
    } else {
      addToast({
        type: 'success',
        title: 'Đã xử lý xong Queue',
        message: `Đã tạo ${completedCount} prompt.`,
      });
    }
  };

  return (
    <FlowModePanel showRotationStatus={false} className="flow-image-all-mode-panel">
      <div className="flow-image-all-summary" aria-label="Thống kê hàng đợi">
        <div className="flow-image-all-stat is-processing">
          <strong>{taskCounts.processing}</strong>
          <span>Đang chạy</span>
        </div>
        <div className="flow-image-all-stat is-queued">
          <strong>{taskCounts.queued}</strong>
          <span>Đang chờ</span>
        </div>
        <div className="flow-image-all-stat is-completed">
          <strong>{taskCounts.completed}</strong>
          <span>Hoàn tất</span>
        </div>
        <div className="flow-image-all-stat is-failed">
          <strong>{taskCounts.failed}</strong>
          <span>Lỗi</span>
        </div>
        <div className="flow-image-all-stat is-accounts">
          <strong className={readyAccountCount === null ? 'is-missing' : undefined}>
            {readyAccountCount ?? 'Chưa có'}
          </strong>
          <span>Tài khoản</span>
        </div>
      </div>

      <details className="flow-image-all-config-group" open={expandedConfig === 'basic'}>
        <summary
          onClick={(event) => {
            event.preventDefault();
            setExpandedConfig((current) => (current === 'basic' ? null : 'basic'));
          }}
        >
          Cấu hình cơ bản
        </summary>
        <div className="flow-image-all-config-content">
          <div className="flow-image-all-basic-grid">
            <SelectField
              id="flow-image-all-model"
              label="Model"
              value={store.selectedModel}
              options={store.models.map((m) => ({ value: m.id, label: m.name }))}
              onChange={store.setSelectedModel}
              loading={loading}
              error={error ? { title: 'Không tải được danh sách model', message: error } : null}
              emptyLabel="Chưa có"
            />

            <SelectField
              id="flow-image-all-ratio"
              label="Tỉ lệ"
              value={store.allAspectRatio}
              options={(config?.imageAspectRatios ?? []).map((ratio) => ({
                value: ratio,
                label: ratioLabel(ratio),
              }))}
              onChange={store.setAllAspectRatio}
              loading={!config && !configError}
              error={
                configError ? { title: 'Không tải được cấu hình', message: configError } : null
              }
              emptyLabel="Chưa có"
            />

            <SelectField
              id="flow-image-all-threads"
              label="Số luồng"
              value={store.allThreads}
              options={Array.from({ length: MAX_ALL_IMAGE_THREADS }, (_, index) => {
                const value = index + 1;
                return { value, label: String(value) };
              })}
              onChange={store.setAllThreads}
            />

            <SelectField
              id="flow-image-all-wait-mode"
              label="Thời gian chờ"
              value={store.allWaitMode}
              options={ALL_IMAGE_WAIT_MODES.map(({ value, label }) => ({ value, label }))}
              onChange={store.setAllWaitMode}
            />

            <div className="flow-image-all-folder-field">
              <span className="flow-image-all-value-label">Folder lưu</span>
              <div className="flow-image-all-folder-control">
                <output
                  className={store.imageOutputDirectory ? undefined : 'is-empty'}
                  title={store.imageOutputDirectory || 'Chưa có'}
                >
                  {store.imageOutputDirectory || 'Chưa có'}
                </output>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label="Chọn folder lưu ảnh"
                  title="Chọn folder lưu ảnh"
                  onClick={async () => {
                    const response = await api.chooseImageOutputDirectory(
                      useFlowImageStore.getState().imageOutputDirectory,
                    );
                    if (!response.success || !response.data) {
                      addToast({
                        type: 'error',
                        title: 'Không chọn được folder lưu',
                        message: response.error ?? 'Không rõ lỗi.',
                      });
                      return;
                    }
                    if (!response.data.canceled && response.data.path) {
                      useFlowImageStore.getState().setImageOutputDirectory(response.data.path);
                    }
                  }}
                >
                  <FolderOpen size={15} />
                  <span>Chọn</span>
                </Button>
              </div>
            </div>
            <div className="flow-image-all-switch-field">
              <span className="flow-image-all-value-label">Ẩn logo gemini</span>
              <button
                type="button"
                role="switch"
                className={`flow-image-all-switch${store.hideGeminiLogo ? ' is-on' : ''}`}
                aria-label="Ẩn logo gemini"
                aria-checked={store.hideGeminiLogo}
                onClick={() => store.setHideGeminiLogo(!store.hideGeminiLogo)}
                title="Tùy chọn này chưa được hỗ trợ trong request tạo ảnh."
              >
                <span />
              </button>
            </div>
          </div>
        </div>
      </details>

      <details className="flow-image-all-config-group" open={expandedConfig === 'advanced'}>
        <summary
          onClick={(event) => {
            event.preventDefault();
            setExpandedConfig((current) => (current === 'advanced' ? null : 'advanced'));
          }}
        >
          Cấu hình nâng cao / Ảnh tham chiếu
        </summary>
        <div className="flow-image-all-config-content">
          <div className="flow-image-all-basic-grid">
            <SelectField
              id="flow-image-all-quality"
              label="Chất lượng"
              value={allImageQuality}
              options={availableImageQualities.map((quality) => ({
                value: quality,
                label: quality,
              }))}
              onChange={(quality) => setAllImageQuality(quality as FlowImageQualityType)}
            />
            <ConfigValueField label="Seed" />
            <SelectField
              id="flow-image-all-reference-mode"
              label="Chế độ tham chiếu"
              value={store.allReferenceMode}
              options={ALL_IMAGE_REFERENCE_MODES.map((mode) => ({
                value: mode.value,
                label: mode.label,
              }))}
              onChange={(mode) => store.setAllReferenceMode(mode as AllImageReferenceMode)}
            />
            <SelectField
              id="flow-image-all-count"
              label="Số lượng ảnh / prompt"
              value={store.allImageCountPerPrompt}
              options={Array.from({ length: config?.maxImages ?? 0 }, (_, index) => {
                const value = index + 1;
                return { value, label: String(value) };
              })}
              onChange={store.setAllImageCountPerPrompt}
              loading={!config && !configError}
              error={
                configError ? { title: 'Không tải được cấu hình', message: configError } : null
              }
              emptyLabel="Chưa có"
            />
          </div>
          {config &&
          model &&
          referenceLimit !== undefined &&
          (referenceLimit > 0 || store.referenceImages.length > 0) ? (
            <ReferencePicker
              config={config}
              limit={referenceLimit}
              images={store.referenceImages}
              onAdd={store.addReferenceImages}
              onRemove={store.removeReferenceImage}
            />
          ) : (
            <p className="flow-image-all-config-empty" role={configError ? 'alert' : undefined}>
              {configError ||
                (!model
                  ? 'Chưa có dữ liệu model để xác định ảnh tham chiếu.'
                  : !config
                    ? 'Chưa có cấu hình ảnh tham chiếu.'
                    : 'Model hiện tại không hỗ trợ ảnh tham chiếu.')}
            </p>
          )}
        </div>
      </details>

      <div className="flow-image-all-action-dock">
        <p className="flow-image-all-queue-count">
          Queue hiện có <strong>{store.allPrompts.length}</strong> tác vụ
        </p>

        {store.allProcessing && (
          <div className="flow-image-all-control-row">
            <Button
              type="button"
              variant="outline"
              disabled={store.allPaused || store.allStopRequested}
              onClick={() => useFlowImageStore.getState().setAllPaused(true)}
            >
              Tạm dừng
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={store.allStopRequested}
              onClick={() => {
                const current = useFlowImageStore.getState();
                current.setAllPaused(false);
                current.setAllStopRequested(true);
              }}
              title="Các request đang chạy sẽ hoàn tất trước khi dừng."
            >
              Dừng
            </Button>
          </div>
        )}

        <Button
          type="button"
          variant="outline"
          className="flow-prompt-list-trigger"
          onClick={() => setPromptListOpen(true)}
        >
          Nhập danh sách prompt
        </Button>

        <Button
          type="button"
          variant="primary"
          className="flow-all-start-processing"
          disabled={store.allProcessing || !hasTasksToProcess}
          onClick={() => void startProcessing()}
        >
          {store.allProcessing
            ? store.allStopRequested
              ? 'Đang dừng…'
              : store.allPaused
                ? 'Đang tạm dừng…'
                : 'Đang xử lý…'
            : store.allPaused || store.allStopRequested
              ? 'Tiếp tục xử lý'
              : 'Bắt đầu xử lý'}
        </Button>
      </div>

      {promptListOpen && (
        <PromptListModal
          onClose={() => setPromptListOpen(false)}
          onAdd={(prompts) => {
            const current = useFlowImageStore.getState();
            current.addAllPrompts(
              prompts,
              current.selectedModel,
              current.allAspectRatio,
              current.allImageQuality,
              current.allImageCountPerPrompt,
              current.referenceImages.map(({ base64, mimeType, name }) => ({
                base64,
                mimeType,
                name,
              })),
            );
            setPromptListOpen(false);
            addToast({
              type: 'success',
              title: 'Đã thêm vào Queue',
              message: `Đã thêm ${prompts.length} prompt.`,
            });
          }}
        />
      )}
    </FlowModePanel>
  );
};

function ConfigValueField({
  label,
  value,
  className,
}: {
  label: string;
  value?: string | number | null;
  className?: string;
}) {
  const hasValue = value !== undefined && value !== null && String(value).trim() !== '';
  const displayValue = hasValue ? String(value) : 'Chưa có';

  return (
    <div className={`flow-image-all-value-field${className ? ` ${className}` : ''}`}>
      <span className="flow-image-all-value-label">{label}</span>
      <output className={hasValue ? undefined : 'is-empty'}>{displayValue}</output>
    </div>
  );
}

async function saveTaskImages(
  task: { batchId: string },
  results: { localUrl?: string; error?: string }[],
  outputDirectory: string,
  alreadySaved: string[],
): Promise<{ savedLocalURLs: string[]; errors: string[] }> {
  const savedLocalURLs = new Set(alreadySaved);
  const errors: string[] = [];
  if (!outputDirectory) return { savedLocalURLs: [...savedLocalURLs], errors };

  for (const result of results) {
    const localUrl = result.localUrl;
    if (!localUrl || result.error || savedLocalURLs.has(localUrl)) continue;
    try {
      const response = await api.saveImageToTaskFolder(localUrl, outputDirectory, task.batchId);
      if (!response.success || !response.data?.path) {
        errors.push(response.error ?? 'Không lưu được ảnh vào folder của tác vụ.');
        continue;
      }
      savedLocalURLs.add(localUrl);
    } catch (cause) {
      errors.push(cause instanceof Error ? cause.message : String(cause));
    }
  }

  return { savedLocalURLs: [...savedLocalURLs], errors };
}

function randomWaitMilliseconds(mode: AllImageWaitMode): number | null {
  const range = ALL_IMAGE_WAIT_MODES.find((option) => option.value === mode);
  if (!range) return null;

  const waitSeconds =
    range.minSeconds + Math.floor(Math.random() * (range.maxSeconds - range.minSeconds + 1));
  return waitSeconds * 1000;
}

async function waitUntil(deadline: number): Promise<boolean> {
  while (true) {
    const controls = useFlowImageStore.getState();
    if (controls.allPaused || controls.allStopRequested) return false;

    const remainingMs = deadline - Date.now();
    if (remainingMs <= 0) return true;

    await new Promise<void>((resolve) => {
      window.setTimeout(resolve, Math.min(250, remainingMs));
    });
  }
}
