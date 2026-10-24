import React from 'react';
import { api } from '../../../../services/api';
import {
  FlowModePanel,
  QuantityField,
  ReferencePicker,
  SelectField,
  durationLabel,
  ratioLabel,
  selectedModelOf,
  useModelCatalog,
  useStudioConfig,
} from '../../shared';
import { useFlowVideoStore } from '../store/flowVideoStore';

/**
 * SingleModePanel - Video settings with the same fields as Image. Ratios,
 * durations and qualities are those Flow lists for the selected model.
 */
export const SingleModePanel: React.FC = () => {
  const { config } = useStudioConfig();
  const models = useFlowVideoStore((s) => s.models);
  const setModels = useFlowVideoStore((s) => s.setModels);
  const model = useFlowVideoStore(selectedModelOf);
  const store = useFlowVideoStore();
  const { loading, error } = useModelCatalog(api.getVideoModels, setModels);

  return (
    <FlowModePanel>
      <SelectField
        id="flow-video-model"
        label="Model"
        value={store.selectedModel}
        options={models.map((m) => ({ value: m.id, label: m.name }))}
        onChange={store.setSelectedModel}
        loading={loading}
        error={error ? { title: 'Không tải được danh sách model', message: error } : null}
      />

      <SelectField
        id="flow-video-ratio"
        label="Tỉ lệ"
        value={store.aspectRatio}
        options={(model?.aspectRatios ?? []).map((r) => ({ value: r, label: ratioLabel(r) }))}
        onChange={store.setAspectRatio}
        loading={loading}
      />

      <SelectField
        id="flow-video-duration"
        label="Thời lượng"
        value={store.duration}
        options={(model?.durations ?? []).map((d) => ({ value: d, label: durationLabel(d) }))}
        onChange={store.setDuration}
        loading={loading}
      />

      <SelectField
        id="flow-video-quality"
        label="Chất lượng"
        value={store.resolution}
        options={(model?.resolutions ?? []).map((r) => ({ value: r, label: r }))}
        onChange={store.setResolution}
        loading={loading}
      />

      {config && (
        <>
          <QuantityField
            value={store.quantity}
            max={config.maxVideos}
            onChange={store.setQuantity}
            ariaLabel="Số video mỗi lần tạo"
          />
          <ReferencePicker
            config={config}
            limit={model?.maxReferenceImages ?? 0}
            images={store.referenceImages}
            onAdd={store.addReferenceImages}
            onRemove={store.removeReferenceImage}
          />
        </>
      )}
    </FlowModePanel>
  );
};
