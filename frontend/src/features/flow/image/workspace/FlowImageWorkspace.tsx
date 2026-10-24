import React from 'react';
import { AlertCircle, Copy, Download, Maximize2, X } from 'lucide-react';
import './FlowImageWorkspace.css';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { GenerateImageRequest } from '../../../../types';
import {
  MediaGallery,
  PromptBar,
  cssAspectRatio,
  selectedModelOf,
  useGeneration,
  useSaveMedia,
} from '../../shared';
import { useFlowImageStore } from '../store/flowImageStore';

export const FlowImageWorkspace: React.FC = () => {
  const store = useFlowImageStore();
  const model = useFlowImageStore(selectedModelOf);
  const addToast = useAppStore((s) => s.addToast);
  const generate = useGeneration(useFlowImageStore, 'ảnh');
  const saveMedia = useSaveMedia();
  const { prompt, aspectRatio, quantity, referenceImages, items, previewImage } = store;

  const referenceLimit = model?.maxReferenceImages ?? 0;
  const blockedReason = !model
    ? 'Chưa có model: kiểm tra tài khoản ở mục Account.'
    : referenceImages.length > referenceLimit
      ? `${model.name} nhận tối đa ${referenceLimit} ảnh tham chiếu.`
      : undefined;

  const onGenerate = () =>
    void generate(() =>
      api.generateImages(
        // createFrom builds the generated class, which converts the nested references.
        GenerateImageRequest.createFrom({
          prompt: prompt.trim(),
          modelId: store.selectedModel,
          aspectRatio,
          count: quantity,
          references: referenceImages.map((img) => ({
            base64: img.base64,
            mimeType: img.mimeType,
            fileName: img.name,
          })),
        }),
      ),
    );

  const copyPrompt = (text: string) => {
    void navigator.clipboard.writeText(text);
    addToast({ type: 'info', title: 'Đã sao chép prompt', message: text, durationMs: 2000 });
  };

  return (
    <div className="flow-workspace-container">
      <MediaGallery
        isEmpty={items.length === 0}
        emptyTitle="Tạo hình ảnh với Google Flow"
        emptyText="Mô tả hình ảnh bạn muốn ở ô bên dưới rồi nhấn Enter."
        pending={store.isGenerating ? quantity : 0}
        pendingRatio={aspectRatio}
        pendingLabel="Đang tạo ảnh"
      >
        {items.map((img) => (
          <div
            key={img.id}
            className="flow-media-card"
            style={{ aspectRatio: cssAspectRatio(img.aspectRatio) }}
          >
            {img.localUrl ? (
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
              <p className="flow-image-prompt-caption" title={img.prompt}>
                {img.prompt}
              </p>
            </div>
          </div>
        ))}
      </MediaGallery>

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
