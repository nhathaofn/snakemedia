import React, { useRef } from 'react';
import { useShallow } from 'zustand/react/shallow';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { GenerateVideoRequest } from '../../../../types';
import {
  UNCERTAIN_RETRY_HINT,
  ConversationGallery,
  PromptBar,
  VideoCard,
  beginGenerationAttempt,
  selectedModelOf,
  useGeneration,
  useRestoreJobs,
  useResumeJob,
  useStopGeneration,
  useSaveMedia,
} from '../../shared';
import { errorMessage } from '../../../../utils';
import { useFlowVideoStore } from '../store/flowVideoStore';

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
  const generate = useGeneration(useFlowVideoStore, 'video');
  const saveMedia = useSaveMedia();
  const addToast = useAppStore((s) => s.addToast);
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

  // The prompt box clears only after the request ID is derived (async), so a
  // second Enter in that gap must not send the same prompt twice.
  const submitting = useRef(false);

  const onGenerate = () => {
    if (submitting.current) return;
    const current = useFlowVideoStore.getState();
    const draft = {
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
    const requestData = {
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
    };
    submitting.current = true;
    void beginGenerationAttempt('video', requestData)
      .then((attempt) => {
        submitting.current = false;
        // The turn shares the request ID, so restoring or resuming this job finds it.
        const turn = { ...draft, id: attempt.requestId };
        return generate(
          () =>
            api.generateVideos(
              GenerateVideoRequest.createFrom({ ...requestData, requestId: attempt.requestId }),
            ),
          {
            onStart: () => useFlowVideoStore.getState().beginTurn(turn),
            onSuccess: (results) => {
              attempt.settle(results.some((result) => result.pending));
              useFlowVideoStore.getState().completeTurn(turn.id, results);
            },
            onFailure: (message, promptRestored) => {
              attempt.settle(false);
              const current = useFlowVideoStore.getState();
              if (promptRestored) current.removeTurn(turn.id);
              else current.failTurn(turn.id, message);
            },
            onUncertainFailure: (message, promptRestored) => {
              attempt.settle(true);
              const current = useFlowVideoStore.getState();
              if (promptRestored) current.removeTurn(turn.id);
              else current.failTurn(turn.id, `${message} ${UNCERTAIN_RETRY_HINT}`);
            },
          },
        );
      })
      .catch((error: unknown) => {
        submitting.current = false;
        addToast({ type: 'error', title: 'Không gửi được yêu cầu', message: errorMessage(error) });
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
