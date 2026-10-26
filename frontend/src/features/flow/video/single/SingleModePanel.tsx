import React from 'react';
import { useShallow } from 'zustand/react/shallow';
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
  const store = useFlowVideoStore(
    useShallow((s) => ({
      addReferenceImages: s.addReferenceImages,
      aspectRatio: s.aspectRatio,
      duration: s.duration,
      quantity: s.quantity,
      referenceImages: s.referenceImages,
      removeReferenceImage: s.removeReferenceImage,
      resolution: s.resolution,
      selectedModel: s.selectedModel,
      setAspectRatio: s.setAspectRatio,
      setDuration: s.setDuration,
      setQuantity: s.setQuantity,
      setResolution: s.setResolution,
      setSelectedModel: s.setSelectedModel,
    })),
  );
  const { loading, error } = useModelCatalog(api.getVideoModels, setModels);
  // Duration 0 is a variant that takes any duration at one price.
  const creditCost = model?.costs?.find(
    (cost) =>
      cost.resolution === store.resolution &&
      (cost.duration === store.duration || cost.duration === 0),
  )?.credits;

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

      {creditCost !== undefined && (
        <p className="flow-field-hint" role="status">
          Ước tính {creditCost * store.quantity} credit ({creditCost} credit/video)
        </p>
      )}

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
