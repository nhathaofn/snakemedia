import React, { useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../../../../services/api';
import { BatchForm, ConfigGroup, OutputFolderField, RunDock } from '../../../../components/batch';
import { Button } from '../../../../components/ui';
import { useConfigSection } from '../../../../hooks';
import { ROUTES } from '../../../../routes/registry';
import { useAppStore } from '../../../../stores';
import { AccountProvider, FlowVideoTaskRequest, FlowVideoTaskStatus } from '../../../../types';
import {
  FlowModePanel,
  SelectField,
  durationLabel,
  ratioLabel,
  selectedModelOf,
  useModelCatalog,
} from '../../shared';
import { useFlowVideoStore } from '../store/flowVideoStore';
import { QueueReferences } from './QueueReferences';
import { countTasks, useVisibleVideoTasks } from './queueStore';
import './AllModePanel.css';

interface GapPreset {
  id: string;
  label: string;
  min: number;
  max: number;
}

const GAP_PRESETS: readonly GapPreset[] = [
  { id: 'immediate', label: 'Ngay lập tức (0s)', min: 0, max: 0 },
  { id: '5-10s', label: '5 - 10s (Khuyến nghị)', min: 5, max: 10 },
  { id: '5-15s', label: '5 - 15s', min: 5, max: 15 },
  { id: '10-15s', label: '10 - 15s', min: 10, max: 15 },
  { id: '10-20s', label: '10 - 20s', min: 10, max: 20 },
];

/**
 * All-mode settings and queue controls for video. The queue runs in the backend
 * (threads and spacing in Cài đặt Flow); this panel holds its settings, adds
 * prompts and starts or pauses it.
 */
export const AllModePanel: React.FC = () => {
  const navigate = useNavigate();
  const models = useFlowVideoStore((s) => s.models);
  const selectedModel = useFlowVideoStore((s) => s.selectedModel);
  const setSelectedModel = useFlowVideoStore((s) => s.setSelectedModel);
  const setModels = useFlowVideoStore((s) => s.setModels);
  const allAspectRatio = useFlowVideoStore((s) => s.allAspectRatio);
  const setAllAspectRatio = useFlowVideoStore((s) => s.setAllAspectRatio);
  const allDuration = useFlowVideoStore((s) => s.allDuration);
  const setAllDuration = useFlowVideoStore((s) => s.setAllDuration);
  const allResolution = useFlowVideoStore((s) => s.allResolution);
  const setAllResolution = useFlowVideoStore((s) => s.setAllResolution);
  const outputDirectory = useFlowVideoStore((s) => s.videoOutputDirectory);
  const setVideoOutputDirectory = useFlowVideoStore((s) => s.setVideoOutputDirectory);
  const model = useFlowVideoStore(selectedModelOf);
  const { tasks } = useVisibleVideoTasks();
  const addToast = useAppStore((state) => state.addToast);
  const { loading, error } = useModelCatalog(api.getVideoModels, setModels);
  const counts = countTasks(tasks);

  // Sync batchOutputDir on mount.
  useEffect(() => {
    let active = true;
    void api.getBatchOutputDirectory(AccountProvider.Flow).then((res) => {
      if (active && res.success && res.data) {
        setVideoOutputDirectory(res.data);
      }
    });
    return () => {
      active = false;
    };
  }, [setVideoOutputDirectory]);

  // Sync presets with flow config.
  const { section: flowConfig, patch: patchFlowConfig } = useConfigSection('flow');
  const minGap = flowConfig?.videoQueueGapMinSeconds;
  const maxGap = flowConfig?.videoQueueGapMaxSeconds;
  const activePreset = GAP_PRESETS.find((p) => p.min === minGap && p.max === maxGap);
  const currentGapValue = !flowConfig ? '' : activePreset ? activePreset.id : 'custom';
  const gapOptions = useMemo(() => {
    const options = GAP_PRESETS.map((p) => ({ value: p.id, label: p.label }));
    if (!activePreset && minGap !== undefined && maxGap !== undefined) {
      options.push({ value: 'custom', label: `Tùy chỉnh (${minGap} - ${maxGap}s)` });
    }
    return options;
  }, [activePreset, minGap, maxGap]);

  const handleGapChange = (value: string) => {
    const preset = GAP_PRESETS.find((p) => p.id === value);
    if (preset) {
      patchFlowConfig({
        videoQueueGapMinSeconds: preset.min,
        videoQueueGapMaxSeconds: preset.max,
      });
    }
  };

  // Prompts still waiting or running: the same text is not queued twice.
  const pending = useMemo(
    () =>
      tasks
        .filter(
          (t) =>
            t.status === FlowVideoTaskStatus.Queued || t.status === FlowVideoTaskStatus.Running,
        )
        .map((t) => t.prompt),
    [tasks],
  );

  const queueReferences = useFlowVideoStore((s) => s.queueReferences);
  const creditCost = model?.costs?.find(
    (cost) =>
      cost.resolution === allResolution && (cost.duration === allDuration || cost.duration === 0),
  )?.credits;

  const blockedReason = !flowConfig
    ? 'Đang tải cài đặt Flow.'
    : !selectedModel
      ? 'Chưa có model video: đăng nhập một tài khoản Flow trước.'
      : model?.maxReferenceImages !== undefined && queueReferences.length > model.maxReferenceImages
        ? `Model đã chọn nhận tối đa ${model.maxReferenceImages} ảnh tham chiếu (đang chọn ${queueReferences.length}).`
        : undefined;

  // Prompts are added on hold; "Chạy" starts them.
  const addPrompts = async (prompts: string[]) => {
    const current = useFlowVideoStore.getState();
    const response = await api.submitFlowVideoTasks(
      FlowVideoTaskRequest.createFrom({
        prompts,
        modelId: current.selectedModel,
        aspectRatio: current.allAspectRatio,
        duration: current.allDuration,
        resolution: current.allResolution,
        references: current.queueReferences,
        outputDir: current.videoOutputDirectory,
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
    const response = hold ? await api.holdFlowVideoTasks([]) : await api.runFlowVideoTasks([]);
    if (!response.success) {
      addToast({
        type: 'error',
        title: hold ? 'Không tạm dừng được hàng đợi' : 'Không chạy được hàng đợi',
        message: response.error ?? 'Không rõ lỗi.',
      });
    }
  };

  return (
    <FlowModePanel showRotationStatus={false} className="is-docked flow-video-all-mode-panel">
      <div className="studio-panel-scroll">
        <ConfigGroup title="Cấu hình cơ bản">
          <div className="flow-video-all-basic-grid">
            <SelectField
              id="flow-video-all-model"
              label="Model"
              value={selectedModel}
              options={models.map((m) => ({ value: m.id, label: m.name }))}
              onChange={setSelectedModel}
              loading={loading}
              error={error ? { title: 'Không tải được danh sách model', message: error } : null}
              emptyLabel="Chưa có"
            />
            <SelectField
              id="flow-video-all-ratio"
              label="Tỉ lệ"
              value={allAspectRatio}
              options={(model?.aspectRatios ?? []).map((r) => ({
                value: r,
                label: ratioLabel(r),
              }))}
              onChange={setAllAspectRatio}
              loading={loading}
              emptyLabel="Chưa có"
            />
            <SelectField
              id="flow-video-all-duration"
              label="Thời lượng"
              value={allDuration}
              options={(model?.durations ?? []).map((d) => ({
                value: d,
                label: durationLabel(d),
              }))}
              onChange={setAllDuration}
              loading={loading}
              emptyLabel="Chưa có"
            />
            <SelectField
              id="flow-video-all-quality"
              label="Độ phân giải"
              value={allResolution}
              options={(model?.resolutions ?? []).map((r) => ({
                value: r,
                label: r,
              }))}
              onChange={setAllResolution}
              loading={loading}
              emptyLabel="Chưa có"
            />
            <SelectField
              id="flow-video-all-gap"
              label="Khoảng cách gửi video"
              value={currentGapValue}
              options={gapOptions}
              onChange={handleGapChange}
              loading={!flowConfig}
            />
            {creditCost !== undefined && (
              <p className="flow-video-all-credit-hint" role="status">
                Ước tính {creditCost} credit / video
              </p>
            )}
            <OutputFolderField
              provider={AccountProvider.Flow}
              value={outputDirectory}
              onChange={setVideoOutputDirectory}
            />
          </div>
          <p className="flow-video-all-config-empty">
            Số luồng và khoảng chờ chi tiết chỉnh ở{' '}
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
          id="flow-video-batch"
          rows={4}
          placeholder={'Một chú mèo đi dạo trên mái nhà\nKhung cảnh hoàng hôn biển rực rỡ'}
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
