import React, { useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../../../../services/api';
import { BatchForm, ConfigGroup, OutputFolderField, RunDock } from '../../../../components/batch';
import { Button } from '../../../../components/ui';
import { ROUTES } from '../../../../routes/registry';
import { useAppStore } from '../../../../stores';
import { AccountProvider, FlowImageTaskRequest, FlowImageTaskStatus } from '../../../../types';
import type { FlowImageQuality as FlowImageQualityType } from '../../../../types';
import {
  FlowModePanel,
  SelectField,
  ratioLabel,
  selectedModelOf,
  useModelCatalog,
  useStudioConfig,
} from '../../shared';
import { useFlowImageStore } from '../store/flowImageStore';
import { QueueReferences } from './QueueReferences';
import { countTasks, useImageQueueStore } from './queueStore';
import './AllModePanel.css';

/**
 * All-mode settings and queue controls. The queue itself runs in the backend
 * (threads and spacing in Cài đặt Flow); this panel holds its settings, adds
 * prompts and starts or pauses it; the right side only shows the queue.
 * Settings scroll; the run bar stays at the bottom. Prompts typed (Enter) or
 * imported go to the queue on hold; "Chạy" starts them, "Dừng" holds the rest.
 */
export const AllModePanel: React.FC = () => {
  const { config, error: configError } = useStudioConfig();
  const navigate = useNavigate();
  useEffect(() => {
    if (config && !useFlowImageStore.getState().allAspectRatio) {
      useFlowImageStore.getState().setAllAspectRatio(config.defaultBatchRatio);
    }
  }, [config]);
  const models = useFlowImageStore((s) => s.models);
  const selectedModel = useFlowImageStore((s) => s.selectedModel);
  const setSelectedModel = useFlowImageStore((s) => s.setSelectedModel);
  const setModels = useFlowImageStore((s) => s.setModels);
  const allAspectRatio = useFlowImageStore((s) => s.allAspectRatio);
  const setAllAspectRatio = useFlowImageStore((s) => s.setAllAspectRatio);
  const allImageQuality = useFlowImageStore((s) => s.allImageQuality);
  const setAllImageQuality = useFlowImageStore((s) => s.setAllImageQuality);
  const outputDirectory = useFlowImageStore((s) => s.imageOutputDirectory);
  const setImageOutputDirectory = useFlowImageStore((s) => s.setImageOutputDirectory);
  const model = useFlowImageStore(selectedModelOf);
  const tasks = useImageQueueStore((s) => s.tasks);
  const addToast = useAppStore((state) => state.addToast);
  const { loading, error } = useModelCatalog(api.getImageModels, setModels);
  const counts = countTasks(tasks);
  // Prompts still waiting or running: the same text is not queued twice.
  const pending = useMemo(
    () =>
      tasks
        .filter(
          (t) =>
            t.status === FlowImageTaskStatus.Queued || t.status === FlowImageTaskStatus.Running,
        )
        .map((t) => t.prompt),
    [tasks],
  );
  // Qualities come from the model's live catalogue; Flow lists its default (1K) first.
  const availableImageQualities = useMemo(() => model?.availableQualities ?? [], [model]);

  useEffect(() => {
    if (availableImageQualities.length && !availableImageQualities.includes(allImageQuality)) {
      setAllImageQuality(availableImageQualities[0] as FlowImageQualityType);
    }
  }, [allImageQuality, availableImageQualities, setAllImageQuality]);

  const blockedReason = selectedModel
    ? undefined
    : 'Chưa có model ảnh: đăng nhập một tài khoản Flow trước.';

  // Prompts are added on hold; "Chạy" starts them.
  const addPrompts = async (prompts: string[]) => {
    const current = useFlowImageStore.getState();
    const response = await api.submitFlowImageTasks(
      FlowImageTaskRequest.createFrom({
        prompts,
        modelId: current.selectedModel,
        aspectRatio: current.allAspectRatio,
        quality: current.allImageQuality,
        references: current.queueReferences,
        outputDir: current.imageOutputDirectory,
        hold: true,
      }),
    );
    if (!response.success || !response.data) {
      addToast({
        type: 'error',
        title: 'Không thêm được vào hàng đợi',
        message: response.error ?? 'Không rõ lỗi.',
      });
      return false;
    }
    addToast({
      type: 'success',
      message: `Đã thêm ${response.data.length} prompt vào hàng chờ. Bấm "Chạy" để bắt đầu.`,
    });
    return true;
  };

  const runAll = async (hold: boolean) => {
    const response = hold ? await api.holdFlowImageTasks([]) : await api.runFlowImageTasks([]);
    if (!response.success) {
      addToast({
        type: 'error',
        title: hold ? 'Không tạm dừng được hàng đợi' : 'Không chạy được hàng đợi',
        message: response.error ?? 'Không rõ lỗi.',
      });
    }
  };

  return (
    <FlowModePanel showRotationStatus={false} className="is-docked flow-image-all-mode-panel">
      <div className="studio-panel-scroll">
        <ConfigGroup title="Cấu hình cơ bản">
          <div className="flow-image-all-basic-grid">
            <SelectField
              id="flow-image-all-model"
              label="Model"
              value={selectedModel}
              options={models.map((m) => ({ value: m.id, label: m.name }))}
              onChange={setSelectedModel}
              loading={loading}
              error={error ? { title: 'Không tải được danh sách model', message: error } : null}
              emptyLabel="Chưa có"
            />
            <SelectField
              id="flow-image-all-ratio"
              label="Tỉ lệ"
              value={allAspectRatio}
              options={(config?.imageAspectRatios ?? []).map((ratio) => ({
                value: ratio,
                label: ratioLabel(ratio),
              }))}
              onChange={setAllAspectRatio}
              loading={!config && !configError}
              error={
                configError ? { title: 'Không tải được cấu hình', message: configError } : null
              }
              emptyLabel="Chưa có"
            />
            <SelectField
              id="flow-image-all-quality"
              label="Chất lượng"
              value={allImageQuality}
              options={availableImageQualities.map((quality) => ({
                value: quality,
                label: quality,
              }))}
              onChange={(quality) => setAllImageQuality(quality as FlowImageQualityType)}
              emptyLabel="Chưa có"
            />
            <OutputFolderField
              provider={AccountProvider.Flow}
              value={outputDirectory}
              onChange={setImageOutputDirectory}
            />
          </div>
          <p className="flow-image-all-config-empty">
            Số luồng và thời gian chờ giữa hai lần gửi chỉnh ở{' '}
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => navigate(ROUTES.flow.settings)}
            >
              Cài đặt Flow
            </Button>
          </p>
        </ConfigGroup>

        <ConfigGroup title="Ảnh tham chiếu">
          <QueueReferences limit={model?.maxReferenceImages} />
        </ConfigGroup>

        <BatchForm
          id="flow-image-batch"
          rows={4}
          placeholder={'Một chú mèo ngủ trên bậu cửa sổ\nCon đường mùa thu, ảnh dọc'}
          promptMode="line"
          blockedReason={blockedReason}
          pending={pending}
          onAdd={addPrompts}
        />
      </div>

      <RunDock
        heldCount={counts.held}
        waitingCount={counts.queued}
        onRun={() => void runAll(false)}
        onStop={() => void runAll(true)}
      />
    </FlowModePanel>
  );
};
