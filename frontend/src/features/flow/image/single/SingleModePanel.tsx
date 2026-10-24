import React, { useEffect } from 'react';
import { api } from '../../../../services/api';
import {
  FlowModePanel,
  QuantityField,
  ReferencePicker,
  SelectField,
  ratioLabel,
  selectedModelOf,
  useModelCatalog,
  useStudioConfig,
} from '../../shared';
import { useFlowImageStore } from '../store/flowImageStore';

/** SingleModePanel - Image settings; every choice and limit comes from the backend. */
export const SingleModePanel: React.FC = () => {
  const { config, error: configError } = useStudioConfig();
  const store = useFlowImageStore();
  const model = useFlowImageStore(selectedModelOf);
  const { loading, error } = useModelCatalog(api.getImageModels, store.setModels);
  const { aspectRatio, setAspectRatio } = store;

  useEffect(() => {
    if (config && !aspectRatio) setAspectRatio(config.defaultImageRatio);
  }, [config, aspectRatio, setAspectRatio]);

  return (
    <FlowModePanel>
      <SelectField
        id="flow-image-model"
        label="Model"
        value={store.selectedModel}
        options={store.models.map((m) => ({ value: m.id, label: m.name }))}
        onChange={store.setSelectedModel}
        loading={loading}
        error={error ? { title: 'Không tải được danh sách model', message: error } : null}
      />

      <SelectField
        id="flow-image-ratio"
        label="Tỉ lệ"
        value={aspectRatio}
        options={(config?.imageAspectRatios ?? []).map((r) => ({ value: r, label: ratioLabel(r) }))}
        onChange={setAspectRatio}
        loading={!config && !configError}
        error={configError ? { title: 'Không tải được cấu hình', message: configError } : null}
      />

      {config && (
        <>
          <QuantityField
            value={store.quantity}
            max={config.maxImages}
            onChange={store.setQuantity}
            ariaLabel="Số ảnh mỗi lần tạo"
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
