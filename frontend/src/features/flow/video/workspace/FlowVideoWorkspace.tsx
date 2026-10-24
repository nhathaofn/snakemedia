import React from 'react';
import { api } from '../../../../services/api';
import { GenerateVideoRequest } from '../../../../types';
import {
  MediaGallery,
  PromptBar,
  VIDEO_GRID_CLASS,
  VideoCard,
  selectedModelOf,
  useGeneration,
  useSaveMedia,
} from '../../shared';
import { useFlowVideoStore } from '../store/flowVideoStore';

export const FlowVideoWorkspace: React.FC = () => {
  const store = useFlowVideoStore();
  const model = useFlowVideoStore(selectedModelOf);
  const generate = useGeneration(useFlowVideoStore, 'video');
  const saveMedia = useSaveMedia();
  const { prompt, aspectRatio, duration, resolution, quantity, items, referenceImages } = store;

  const referenceLimit = model?.maxReferenceImages ?? 0;
  const blockedReason = !model
    ? 'Chưa có model: kiểm tra tài khoản ở mục Account.'
    : referenceImages.length > referenceLimit
      ? `${model.name} nhận tối đa ${referenceLimit} ảnh tham chiếu.`
      : undefined;

  const onGenerate = () =>
    void generate(() =>
      api.generateVideos(
        // createFrom builds the generated class, which converts the nested references.
        GenerateVideoRequest.createFrom({
          prompt: prompt.trim(),
          modelId: store.selectedModel,
          aspectRatio,
          duration,
          resolution,
          count: quantity,
          references: referenceImages.map((img) => ({
            base64: img.base64,
            mimeType: img.mimeType,
            fileName: img.name,
          })),
        }),
      ),
    );

  return (
    <div className="flow-workspace-container">
      <MediaGallery
        isEmpty={items.length === 0}
        emptyTitle="Tạo video với Google Flow"
        emptyText="Mô tả video bạn muốn ở ô bên dưới rồi nhấn Enter. Flow cần vài phút để dựng video."
        pending={store.isGenerating ? quantity : 0}
        pendingRatio={aspectRatio}
        pendingLabel="Đang tạo video"
        gridClassName={VIDEO_GRID_CLASS}
      >
        {items.map((video) => (
          <VideoCard key={video.id} video={video} onSave={(url) => void saveMedia(url)} />
        ))}
      </MediaGallery>

      <PromptBar
        prompt={prompt}
        onPromptChange={store.setPrompt}
        onGenerate={onGenerate}
        placeholder="Mô tả video bạn muốn tạo"
        generating={store.isGenerating}
        blockedReason={blockedReason}
      />
    </div>
  );
};
