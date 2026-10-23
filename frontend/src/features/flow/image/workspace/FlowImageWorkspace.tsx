import React, { useCallback } from 'react';
import { Copy, Download, Maximize2, RefreshCw, Sparkles, X } from 'lucide-react';
import './FlowImageWorkspace.css';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { useFlowImageStore } from '../store/flowImageStore';
import type { GeneratedImageItem } from '../store/flowImageStore';

export const FlowImageWorkspace: React.FC = () => {
  const prompt = useFlowImageStore((s) => s.prompt);
  const setPrompt = useFlowImageStore((s) => s.setPrompt);
  const selectedModel = useFlowImageStore((s) => s.selectedModel);
  const aspectRatio = useFlowImageStore((s) => s.aspectRatio);
  const quantity = useFlowImageStore((s) => s.quantity);
  const isGenerating = useFlowImageStore((s) => s.isGenerating);
  const setIsGenerating = useFlowImageStore((s) => s.setIsGenerating);
  const images = useFlowImageStore((s) => s.images);
  const addImages = useFlowImageStore((s) => s.addImages);
  const previewImage = useFlowImageStore((s) => s.previewImage);
  const setPreviewImage = useFlowImageStore((s) => s.setPreviewImage);

  const addToast = useAppStore((s) => s.addToast);

  const handleGenerate = useCallback(async () => {
    const trimmed = prompt.trim();
    if (!trimmed || isGenerating) return;

    setIsGenerating(true);

    try {
      const res = await api.generateImages({
        prompt: trimmed,
        modelId: selectedModel || 'NARWHAL',
        aspectRatio,
        count: quantity,
      });

      if (res.success && res.data && res.data.length > 0) {
        addImages(res.data as GeneratedImageItem[]);
        addToast({
          type: 'success',
          title: 'Tạo ảnh thành công',
          message: `Đã hoàn tất tạo ${res.data.length} ảnh với model ${selectedModel || 'Nano 2'}.`,
        });
      } else {
        addToast({
          type: 'error',
          title: 'Không thể tạo ảnh',
          message: res.error ?? 'Lỗi phiên làm việc hoặc chưa có tài khoản Google Flow hợp lệ.',
        });
      }
    } catch (err) {
      addToast({
        type: 'error',
        title: 'Lỗi phát sinh',
        message: err instanceof Error ? err.message : 'Lỗi không xác định khi tạo ảnh.',
      });
    } finally {
      setIsGenerating(false);
    }
  }, [
    prompt,
    isGenerating,
    selectedModel,
    aspectRatio,
    quantity,
    setIsGenerating,
    addImages,
    addToast,
  ]);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      void handleGenerate();
    }
  };

  const copyPrompt = (text: string) => {
    void navigator.clipboard.writeText(text);
    addToast({
      type: 'info',
      title: 'Đã sao chép prompt',
      message: text,
      durationMs: 2000,
    });
  };

  const downloadImage = (url: string, id: string) => {
    const a = document.createElement('a');
    a.href = url;
    a.download = `${id}.png`;
    a.target = '_blank';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  };

  const isEmpty = images.length === 0;
  const currentRatioClass = `aspect-${aspectRatio.replace(':', '-')}`;

  return (
    <div className="flow-workspace-container">
      {/* 1. Main Scrollable Gallery Area */}
      <div className="flow-workspace-gallery">
        {isEmpty && !isGenerating ? (
          <div className="flow-gallery-empty">
            <Sparkles size={40} className="flow-empty-icon" />
            <h3>Khởi tạo hình ảnh AI</h3>
            <p>
              Nhập mô tả ý tưởng ở thanh bên dưới và bấm Generate (hoặc Ctrl+Enter) để bắt đầu sáng
              tạo với Google Flow.
            </p>
          </div>
        ) : (
          <div className="flow-image-grid">
            {/* Loading Skeleton Cards */}
            {isGenerating &&
              Array.from({ length: quantity }).map((_, idx) => (
                <div
                  key={`skeleton-${idx}`}
                  className={`flow-image-card skeleton ${currentRatioClass}`}
                >
                  <div className="skeleton-content">
                    <RefreshCw size={20} className="skeleton-spinner" />
                    <span>
                      Đang tạo ảnh {idx + 1}/{quantity}...
                    </span>
                  </div>
                </div>
              ))}

            {/* Rendered Generated Images */}
            {images.map((img) => {
              const cardRatioClass = `aspect-${img.aspectRatio.replace(':', '-')}`;
              return (
                <div key={img.id} className={`flow-image-card ${cardRatioClass}`}>
                  <img src={img.url} alt={img.prompt} loading="lazy" />
                  <div className="flow-image-overlay">
                    <span className="flow-image-model-badge">{img.model}</span>
                    <div className="flow-image-actions">
                      <button
                        type="button"
                        className="flow-card-action-btn"
                        title="Xem phóng to"
                        onClick={() => setPreviewImage(img)}
                      >
                        <Maximize2 size={14} />
                      </button>
                      <button
                        type="button"
                        className="flow-card-action-btn"
                        title="Sao chép Prompt"
                        onClick={() => copyPrompt(img.prompt)}
                      >
                        <Copy size={14} />
                      </button>
                      <button
                        type="button"
                        className="flow-card-action-btn"
                        title="Tải ảnh về máy"
                        onClick={() => downloadImage(img.url, img.id)}
                      >
                        <Download size={14} />
                      </button>
                    </div>
                    <p className="flow-image-prompt-caption" title={img.prompt}>
                      {img.prompt}
                    </p>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* 2. Pinned Bottom Prompt Bar */}
      <div className="flow-prompt-bar">
        <div className="flow-prompt-meta">
          <span className="flow-prompt-badge">Model: {selectedModel || 'Mặc định'}</span>
          <span className="flow-prompt-badge">Tỉ lệ: {aspectRatio}</span>
          <span className="flow-prompt-badge">Số lượng: x{quantity}</span>
        </div>
        <div className="flow-prompt-input-row">
          <textarea
            className="flow-prompt-textarea"
            placeholder="Mô tả hình ảnh bạn muốn tạo... (Nhấn Ctrl+Enter để tạo)"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={handleKeyDown}
            rows={2}
          />
          <button
            type="button"
            className="flow-generate-btn"
            disabled={isGenerating || !prompt.trim()}
            onClick={() => void handleGenerate()}
          >
            {isGenerating ? (
              <>
                <RefreshCw size={18} className="spin" />
                <span>Đang tạo...</span>
              </>
            ) : (
              <>
                <Sparkles size={18} />
                <span>Generate</span>
              </>
            )}
          </button>
        </div>
      </div>

      {/* 3. Lightbox Preview Modal */}
      {previewImage && (
        <div className="flow-lightbox-overlay" onClick={() => setPreviewImage(null)}>
          <div className="flow-lightbox-content" onClick={(e) => e.stopPropagation()}>
            <button
              type="button"
              className="flow-lightbox-close"
              title="Đóng xem trước"
              onClick={() => setPreviewImage(null)}
            >
              <X size={18} />
            </button>
            <img src={previewImage.url} alt={previewImage.prompt} />
            <div className="flow-lightbox-footer">
              <p>{previewImage.prompt}</p>
              <div className="flow-lightbox-meta">
                <span>Model: {previewImage.model}</span>
                <span>Tỉ lệ: {previewImage.aspectRatio}</span>
                <span>Thời gian: {new Date(previewImage.createdAt).toLocaleTimeString()}</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
