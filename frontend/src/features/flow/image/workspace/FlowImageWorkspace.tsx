import React from 'react';
import { useShallow } from 'zustand/react/shallow';
import { AlertCircle, Copy, Download, Maximize2, RefreshCw } from 'lucide-react';
import { Icon, Modal } from '../../../../components/ui';
import './FlowImageWorkspace.css';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { FlowImageProjectMode, GenerateImageRequest } from '../../../../types';
import {
  ConversationGallery,
  cssAspectRatio,
  selectedModelOf,
  useRestoreJobs,
  useResumeJob,
  useStopGeneration,
  useSaveMedia,
  useStudioSubmit,
} from '../../shared';
import { useFlowImageStore } from '../store/flowImageStore';
import { PromptBar } from '../../../../components/studio';

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
  const submit = useStudioSubmit(useFlowImageStore, 'image', 'ảnh', (request) =>
    api.generateImages(GenerateImageRequest.createFrom(request)),
  );
  const saveMedia = useSaveMedia();
  const { prompt, referenceImages, items, turns, previewImage } = store;

  useRestoreJobs(useFlowImageStore, listPendingSingleImages, 'Đang đối soát job tạo ảnh');

  const { resuming, resumeJob } = useResumeJob(useFlowImageStore, api.resumeImageGeneration);
  const stopGeneration = useStopGeneration();

  const referenceLimit = model?.maxReferenceImages ?? 0;
  const blockedReason = !model
    ? 'Chưa có model: kiểm tra tài khoản ở mục Tài khoản.'
    : referenceImages.length > referenceLimit
      ? `${model.name} nhận tối đa ${referenceLimit} ảnh tham chiếu.`
      : undefined;

  const onGenerate = () => {
    const current = useFlowImageStore.getState();
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
        pendingLabel: 'Đang tạo ảnh',
      },
      request: {
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
      },
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
                <Icon icon={RefreshCw} size="xl" />
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
                <Icon icon={AlertCircle} size="xl" />
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
                  <Icon icon={Maximize2} size="sm" />
                </button>
                <button
                  type="button"
                  className="flow-card-action-btn"
                  title="Sao chép prompt"
                  onClick={() => copyPrompt(img.prompt)}
                >
                  <Icon icon={Copy} size="sm" />
                </button>
                <button
                  type="button"
                  className="flow-card-action-btn"
                  title="Lưu vào thư mục…"
                  disabled={!img.localUrl}
                  onClick={() => img.localUrl && void saveMedia(img.localUrl)}
                >
                  <Icon icon={Download} size="sm" />
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
        <Modal variant="lightbox" label="Ảnh phóng to" onClose={() => store.setPreviewImage(null)}>
          <img src={previewImage.localUrl} alt={previewImage.prompt} />
          <div className="flow-lightbox-footer">
            <p>{previewImage.prompt}</p>
            <div className="flow-lightbox-meta">
              <span>{previewImage.modelName}</span>
              <span>{previewImage.aspectRatio}</span>
              <span>{new Date(previewImage.createdAt).toLocaleTimeString()}</span>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
};
