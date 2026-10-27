import React from 'react';
import { useShallow } from 'zustand/react/shallow';
import { api } from '../../../../services/api';
import { GenerateVideoRequest } from '../../../../types';
import {
  ConversationGallery,
  VideoCard,
  selectedModelOf,
  useRestoreJobs,
  useResumeJob,
  useStopGeneration,
  useSaveMedia,
  useStudioSubmit,
} from '../../shared';
import { useFlowVideoStore } from '../store/flowVideoStore';
import { PromptBar } from '../../../../components/studio';

export const FlowVideoWorkspace: React.FC = () => {
  const store = useFlowVideoStore(
    useShallow((s) => ({
      prompt: s.prompt,
      referenceImages: s.referenceImages,
      items: s.items,
      turns: s.turns,
      setPrompt: s.setPrompt,
      isGenerating: s.isGenerating,
    })),
  );
  const model = useFlowVideoStore(selectedModelOf);
  const submit = useStudioSubmit(useFlowVideoStore, 'video', 'video', (request) =>
    api.generateVideos(GenerateVideoRequest.createFrom(request)),
  );
  const saveMedia = useSaveMedia();
  const { prompt, items, referenceImages, turns } = store;

  useRestoreJobs(useFlowVideoStore, api.listPendingVideoGenerations, 'Đang đối soát job tạo video');
  const { resuming, resumeJob } = useResumeJob(useFlowVideoStore, api.resumeVideoGeneration);
  const stopGeneration = useStopGeneration();

  const referenceLimit = model?.maxReferenceImages ?? 0;
  const blockedReason = !model
    ? 'Chưa có model: kiểm tra tài khoản ở mục Tài khoản.'
    : referenceImages.length > referenceLimit
      ? `${model.name} nhận tối đa ${referenceLimit} ảnh tham chiếu.`
      : undefined;

  const onGenerate = () => {
    const current = useFlowVideoStore.getState();
    submit({
      turn: {
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
      },
      request: {
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
      },
    });
  };

  return (
    <div className="flow-workspace-container">
      <ConversationGallery
        turns={turns}
        items={items}
        emptyTitle="Tạo video với Google Flow"
        onStopTurn={stopGeneration}
        emptyText="Mô tả video bạn muốn ở ô bên dưới rồi nhấn Enter. Flow cần vài phút để dựng video."
        renderItem={(video) => (
          <VideoCard
            video={video}
            showPrompt={false}
            onSave={(url) => void saveMedia(url)}
            onResume={video.jobId ? () => void resumeJob(video.jobId!) : undefined}
            busy={!!video.jobId && resuming.has(video.jobId)}
          />
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
