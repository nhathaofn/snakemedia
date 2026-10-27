import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FolderOpen } from 'lucide-react';
import { api } from '../../../../services/api';
import { BatchForm, repeatPrompts } from '../../../../components/batch';
import { Button, Icon } from '../../../../components/ui';
import { ROUTES } from '../../../../routes/registry';
import { useAppStore } from '../../../../stores';
import { FlowImageTaskRequest } from '../../../../types';
import type { FlowImageQuality as FlowImageQualityType } from '../../../../types';
import {
  FlowModePanel,
  SelectField,
  ratioLabel,
  selectedModelOf,
  useModelCatalog,
  useRotationCount,
  useStudioConfig,
} from '../../shared';
import { useFlowImageStore } from '../store/flowImageStore';
import { QueueReferences } from './QueueReferences';
import { countTasks, useImageQueueStore } from './queueStore';
import './AllModePanel.css';

/**
 * All-mode settings and queue controls. The queue itself runs in the backend
 * (threads and spacing in Cài đặt Flow); this panel adds prompts and starts
 * or pauses it.
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
  const count = useFlowImageStore((s) => s.allImageCountPerPrompt);
  const setCount = useFlowImageStore((s) => s.setAllImageCountPerPrompt);
  const allImageQuality = useFlowImageStore((s) => s.allImageQuality);
  const setAllImageQuality = useFlowImageStore((s) => s.setAllImageQuality);
  const outputDirectory = useFlowImageStore((s) => s.imageOutputDirectory);
  const model = useFlowImageStore(selectedModelOf);
  const tasks = useImageQueueStore((s) => s.tasks);
  const addToast = useAppStore((state) => state.addToast);
  const { loading, error } = useModelCatalog(api.getImageModels, setModels);
  const readyAccountCount = useRotationCount();
  const [expandedConfig, setExpandedConfig] = useState<'basic' | 'advanced' | null>('basic');
  const counts = countTasks(tasks);
  // Qualities come from the model's live catalogue; Flow lists its default (1K) first.
  const availableImageQualities = useMemo(() => model?.availableQualities ?? [], [model]);

  useEffect(() => {
    if (outputDirectory) return;
    let active = true;
    void api.getDefaultImageOutputDirectory().then((response) => {
      if (!active) return;
      if (response.success && response.data) {
        useFlowImageStore.getState().setImageOutputDirectory(response.data);
        return;
      }
      addToast({
        type: 'error',
        title: 'Không tải được thư mục lưu mặc định',
        message: response.error ?? 'Không rõ lỗi.',
      });
    });
    return () => {
      active = false;
    };
  }, [addToast, outputDirectory]);

  useEffect(() => {
    if (availableImageQualities.length && !availableImageQualities.includes(allImageQuality)) {
      setAllImageQuality(availableImageQualities[0] as FlowImageQualityType);
    }
  }, [allImageQuality, availableImageQualities, setAllImageQuality]);

  const addPrompts = async (prompts: string[], repeat: number, held: boolean) => {
    const current = useFlowImageStore.getState();
    const response = await api.submitFlowImageTasks(
      FlowImageTaskRequest.createFrom({
        prompts: repeatPrompts(prompts, repeat),
        modelId: current.selectedModel,
        aspectRatio: current.allAspectRatio,
        quality: current.allImageQuality,
        count: current.allImageCountPerPrompt,
        references: current.queueReferences,
        outputDir: current.imageOutputDirectory,
        hold: held,
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
      title: 'Đã thêm vào hàng đợi',
      message: held
        ? `Đã thêm ${response.data.length} tác vụ (tạm giữ). Bấm "Chạy tác vụ tạm giữ" khi muốn chạy.`
        : `Đã thêm ${response.data.length} tác vụ, hàng đợi đang chạy.`,
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
    <FlowModePanel showRotationStatus={false} className="flow-image-all-mode-panel">
      <div className="flow-image-all-summary" aria-label="Thống kê hàng đợi">
        <div className="flow-image-all-stat is-processing">
          <strong>{counts.running}</strong>
          <span>Đang chạy</span>
        </div>
        <div className="flow-image-all-stat is-queued">
          <strong>{counts.queued + counts.held}</strong>
          <span>Đang chờ</span>
        </div>
        <div className="flow-image-all-stat is-completed">
          <strong>{counts.succeeded}</strong>
          <span>Hoàn tất</span>
        </div>
        <div className="flow-image-all-stat is-failed">
          <strong>{counts.failed}</strong>
          <span>Lỗi</span>
        </div>
        <div className="flow-image-all-stat is-accounts">
          <strong className={readyAccountCount === 0 ? 'is-missing' : undefined}>
            {readyAccountCount}
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
              id="flow-image-all-count"
              label="Số lượng ảnh / prompt"
              value={count}
              options={Array.from({ length: config?.maxImages ?? 0 }, (_, index) => {
                const value = index + 1;
                return { value, label: String(value) };
              })}
              onChange={setCount}
              loading={!config && !configError}
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
            <OutputFolderField />
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
        </div>
      </details>

      <details className="flow-image-all-config-group" open={expandedConfig === 'advanced'}>
        <summary
          onClick={(event) => {
            event.preventDefault();
            setExpandedConfig((current) => (current === 'advanced' ? null : 'advanced'));
          }}
        >
          Ảnh tham chiếu
        </summary>
        <div className="flow-image-all-config-content">
          <QueueReferences limit={model?.maxReferenceImages} />
        </div>
      </details>

      <BatchForm
        id="flow-image-batch"
        placeholder={'Một chú mèo ngủ trên bậu cửa sổ\nCon đường mùa thu, ảnh dọc'}
        repeatLabel="Số lần mỗi prompt"
        blockedReason={
          selectedModel ? undefined : 'Chưa có model ảnh: đăng nhập một tài khoản Flow trước.'
        }
        onSubmit={addPrompts}
      />

      <div className="flow-image-all-action-dock">
        <p className="flow-image-all-queue-count">
          Hàng đợi có <strong>{tasks.length}</strong> tác vụ
        </p>
        <div className="flow-image-all-control-row">
          {counts.queued > 0 && (
            <Button
              type="button"
              variant="outline"
              onClick={() => void runAll(true)}
              title="Tác vụ đang chạy sẽ hoàn tất; các tác vụ chờ được giữ lại."
            >
              Tạm dừng
            </Button>
          )}
          {counts.held > 0 && (
            <Button type="button" variant="primary" onClick={() => void runAll(false)}>
              Chạy {counts.held} tác vụ tạm giữ
            </Button>
          )}
        </div>
      </div>
    </FlowModePanel>
  );
};

/** Folder the queue saves new tasks' images into. */
function OutputFolderField() {
  const outputDirectory = useFlowImageStore((s) => s.imageOutputDirectory);
  const addToast = useAppStore((state) => state.addToast);
  return (
    <div className="flow-image-all-folder-field">
      <span className="flow-image-all-value-label">Thư mục lưu</span>
      <div className="flow-image-all-folder-control">
        <output className={outputDirectory ? undefined : 'is-empty'} title={outputDirectory}>
          {outputDirectory || 'Chưa có'}
        </output>
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-label="Chọn thư mục lưu ảnh"
          title="Chọn thư mục lưu ảnh"
          onClick={async () => {
            const response = await api.chooseImageOutputDirectory(
              useFlowImageStore.getState().imageOutputDirectory,
            );
            if (!response.success || !response.data) {
              addToast({
                type: 'error',
                title: 'Không chọn được thư mục lưu',
                message: response.error ?? 'Không rõ lỗi.',
              });
              return;
            }
            if (!response.data.canceled && response.data.path) {
              useFlowImageStore.getState().setImageOutputDirectory(response.data.path);
            }
          }}
        >
          <Icon icon={FolderOpen} size="sm" />
          <span>Chọn</span>
        </Button>
      </div>
    </div>
  );
}
