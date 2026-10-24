import React from 'react';
import { api } from '../../../../services/api';
import { GenerateVideoRequest } from '../../../../types';
import {
  ConversationGallery,
  PromptBar,
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
  const { prompt, items, referenceImages, turns } = store;

  const referenceLimit = model?.maxReferenceImages ?? 0;
  const blockedReason = !model
    ? 'Chưa có model: kiểm tra tài khoản ở mục Account.'
    : referenceImages.length > referenceLimit
      ? `${model.name} nhận tối đa ${referenceLimit} ảnh tham chiếu.`
      : undefined;

  const onGenerate = () => {
    const current = useFlowVideoStore.getState();
    const turn = {
      id: crypto.randomUUID(),
      prompt: current.prompt.trim(),
      attachments: current.referenceImages.map((image) => ({
        id: image.previewUrl,
        kind: 'image' as const,
        name: image.name,
        url: image.previewUrl,
      })),
      pendingCount: current.quantity,
      pendingRatio: current.aspectRatio,
      pendingLabel: 'Đang tạo video',
    };
    void generate(
      () =>
        api.generateVideos(
          // createFrom builds the generated class, which converts the nested references.
          GenerateVideoRequest.createFrom({
            prompt: current.prompt.trim(),
            modelId: current.selectedModel,
            aspectRatio: current.aspectRatio,
            duration: current.duration,
            resolution: current.resolution,
            count: current.quantity,
            references: current.referenceImages.map((img) => ({
              base64: img.base64,
              mimeType: img.mimeType,
              fileName: img.name,
            })),
          }),
        ),
      {
        onStart: () => useFlowVideoStore.getState().beginTurn(turn),
        onSuccess: (results) => useFlowVideoStore.getState().completeTurn(turn.id, results),
        onFailure: (message, promptRestored) => {
          const current = useFlowVideoStore.getState();
          if (promptRestored) current.removeTurn(turn.id);
          else current.failTurn(turn.id, message);
        },
      },
    );
  };

  return (
    <div className="flow-workspace-container">
      <ConversationGallery
        turns={turns}
        items={items}
        emptyTitle="Tạo video với Google Flow"
        emptyText="Mô tả video bạn muốn ở ô bên dưới rồi nhấn Enter. Flow cần vài phút để dựng video."
        renderItem={(video) => (
          <VideoCard video={video} showPrompt={false} onSave={(url) => void saveMedia(url)} />
        )}
      />

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
