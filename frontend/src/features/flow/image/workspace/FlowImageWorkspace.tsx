import React, { useEffect, useRef } from 'react';
import { useShallow } from 'zustand/react/shallow';
import { AlertCircle, Copy, Download, Maximize2, RefreshCw, X } from 'lucide-react';
import './FlowImageWorkspace.css';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { FlowImageProjectMode, GenerateImageRequest } from '../../../../types';
import {
  UNCERTAIN_RETRY_HINT,
  ConversationGallery,
  PromptBar,
  cssAspectRatio,
  beginGenerationAttempt,
  selectedModelOf,
  useGeneration,
  useRestoreJobs,
  useResumeJob,
  useStopGeneration,
  useSaveMedia,
} from '../../shared';
import { errorMessage } from '../../../../utils';
import { useFlowImageStore } from '../store/flowImageStore';

/** Single studio's saved jobs; All-mode jobs belong to the queue, not this conversation. */
const listPendingSingleImages = () => api.listPendingImageGenerations(FlowImageProjectMode.Single);

export const FlowImageWorkspace: React.FC = () => {
  const store = useFlowImageStore(
    useShallow((s) => ({
      prompt: s.prompt,
      referenceImages: s.referenceImages,
      items: s.items,
      turns: s.turns,
      previewImage: s.previewImage,
      setPrompt: s.setPrompt,
      isGenerating: s.isGenerating,
      setPreviewImage: s.setPreviewImage,
    })),
  );
  const model = useFlowImageStore(selectedModelOf);
  const addToast = useAppStore((s) => s.addToast);
  const generate = useGeneration(useFlowImageStore, 'ảnh');
  const saveMedia = useSaveMedia();
  const { prompt, referenceImages, items, turns, previewImage } = store;

  useRestoreJobs(useFlowImageStore, listPendingSingleImages, 'Đang đối soát job tạo ảnh');

  useEffect(() => {
    if (!previewImage) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') useFlowImageStore.getState().setPreviewImage(null);
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [previewImage]);
  const { resuming, resumeJob } = useResumeJob(useFlowImageStore, api.resumeImageGeneration);
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
    const current = useFlowImageStore.getState();
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
      pendingLabel: 'Đang tạo ảnh',
    };
    const requestData = {
      prompt: current.prompt.trim(),
      modelId: current.selectedModel,
      aspectRatio: current.aspectRatio,
      projectMode: FlowImageProjectMode.Single,
      count: current.quantity,
      references: current.referenceImages.map((img) => ({
        base64: img.base64,
        mimeType: img.mimeType,
        fileName: img.name,
      })),
    };
    submitting.current = true;
    void beginGenerationAttempt('image', requestData)
      .then((attempt) => {
        submitting.current = false;
        // The turn shares the request ID, so restoring or resuming this job finds it.
        const turn = { ...draft, id: attempt.requestId };
        return generate(
          () =>
            api.generateImages(
              GenerateImageRequest.createFrom({ ...requestData, requestId: attempt.requestId }),
            ),
          {
            onStart: () => useFlowImageStore.getState().beginTurn(turn),
            onSuccess: (results) => {
              attempt.settle(results.some((result) => result.pending));
              useFlowImageStore.getState().completeTurn(turn.id, results);
            },
            onFailure: (message, promptRestored) => {
              attempt.settle(false);
              const current = useFlowImageStore.getState();
              if (promptRestored) current.removeTurn(turn.id);
              else current.failTurn(turn.id, message);
            },
            onUncertainFailure: (message, promptRestored) => {
              attempt.settle(true);
              const current = useFlowImageStore.getState();
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

  const copyPrompt = (text: string) => {
    void navigator.clipboard.writeText(text);
    addToast({ type: 'info', title: 'Đã sao chép prompt', message: text, durationMs: 2000 });
  };

  return (
    <div className="flow-workspace-container">
      <ConversationGallery
        turns={turns}
        items={items}
        emptyTitle="Tạo hình ảnh với Google Flow"
        onStopTurn={stopGeneration}
        emptyText="Mô tả hình ảnh bạn muốn ở ô bên dưới rồi nhấn Enter."
        renderItem={(img) => (
          <div className="flow-media-card" style={{ aspectRatio: cssAspectRatio(img.aspectRatio) }}>
            {img.pending ? (
              <div className="flow-media-error flow-media-pending" role="status">
                <RefreshCw size={20} />
                <span>{img.error}</span>
                <button
                  type="button"
                  className="flow-card-action-btn flow-card-text-btn"
                  disabled={!!img.jobId && resuming.has(img.jobId)}
                  onClick={() => img.jobId && void resumeJob(img.jobId)}
                >
                  Kiểm tra tiếp
                </button>
              </div>
            ) : img.localUrl ? (
              <img src={img.localUrl} alt={img.prompt} loading="lazy" />
            ) : (
              <div className="flow-media-error" role="alert">
                <AlertCircle size={20} />
                <span>{img.error}</span>
              </div>
            )}
            <div className="flow-image-overlay">
              <span className="flow-media-badge">{img.modelName}</span>
              <div className="flow-image-actions">
                <button
                  type="button"
                  className="flow-card-action-btn"
                  title="Xem lớn"
                  disabled={!img.localUrl}
                  onClick={() => store.setPreviewImage(img)}
                >
                  <Maximize2 size={14} />
                </button>
                <button
                  type="button"
                  className="flow-card-action-btn"
                  title="Sao chép prompt"
                  onClick={() => copyPrompt(img.prompt)}
                >
                  <Copy size={14} />
                </button>
                <button
                  type="button"
                  className="flow-card-action-btn"
                  title="Lưu vào thư mục…"
                  disabled={!img.localUrl}
                  onClick={() => img.localUrl && void saveMedia(img.localUrl)}
                >
                  <Download size={14} />
                </button>
              </div>
            </div>
          </div>
        )}
      />

      <PromptBar
        prompt={prompt}
        onPromptChange={store.setPrompt}
        onGenerate={onGenerate}
        placeholder="Mô tả hình ảnh bạn muốn tạo"
        generating={store.isGenerating}
        blockedReason={blockedReason}
      />

      {previewImage && (
        <div className="flow-lightbox-overlay" onClick={() => store.setPreviewImage(null)}>
          <div className="flow-lightbox-content" onClick={(e) => e.stopPropagation()}>
            <button
              type="button"
              className="flow-lightbox-close"
              title="Đóng"
              onClick={() => store.setPreviewImage(null)}
            >
              <X size={18} />
            </button>
            <img src={previewImage.localUrl} alt={previewImage.prompt} />
            <div className="flow-lightbox-footer">
              <p>{previewImage.prompt}</p>
              <div className="flow-lightbox-meta">
                <span>{previewImage.modelName}</span>
                <span>{previewImage.aspectRatio}</span>
                <span>{new Date(previewImage.createdAt).toLocaleTimeString()}</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
