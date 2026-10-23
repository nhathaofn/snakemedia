import React, { useEffect, useState } from 'react';
import { AlertCircle, ChevronDown, RefreshCw } from 'lucide-react';
import './SingleModePanel.css';
import { api } from '../../../../services/api';
import type { FlowModel } from '../../../../types';
import { useFlowImageStore } from '../store/flowImageStore';
import type { AspectRatio } from '../store/flowImageStore';

interface AspectOption {
  value: AspectRatio;
  label: string;
  wireframeClass: string;
}

const ASPECT_OPTIONS: readonly AspectOption[] = [
  { value: '16:9', label: '16:9', wireframeClass: 'aspect-16-9' },
  { value: '4:3', label: '4:3', wireframeClass: 'aspect-4-3' },
  { value: '1:1', label: '1:1', wireframeClass: 'aspect-1-1' },
  { value: '3:4', label: '3:4', wireframeClass: 'aspect-3-4' },
  { value: '9:16', label: '9:16', wireframeClass: 'aspect-9-16' },
];

const QUANTITY_OPTIONS = [1, 2, 3, 4] as const;

/**
 * SingleModePanel - Controls for single image generation.
 * Includes real model selection, 5 aspect ratios, quantity (x1-x4),
 * and round-robin account rotation indicator.
 */
export const SingleModePanel: React.FC = () => {
  const [models, setModels] = useState<FlowModel[]>([]);
  const selectedModel = useFlowImageStore((s) => s.selectedModel);
  const setSelectedModel = useFlowImageStore((s) => s.setSelectedModel);
  const aspectRatio = useFlowImageStore((s) => s.aspectRatio);
  const setAspectRatio = useFlowImageStore((s) => s.setAspectRatio);
  const quantity = useFlowImageStore((s) => s.quantity);
  const setQuantity = useFlowImageStore((s) => s.setQuantity);
  const [activeAccountsCount, setActiveAccountsCount] = useState<number>(0);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;

    void (async () => {
      const [modelsRes, countRes] = await Promise.all([
        api.getImageModels(),
        api.getActiveRotationCount(),
      ]);

      if (!active) return;

      if (countRes.success && typeof countRes.data === 'number') {
        setActiveAccountsCount(countRes.data);
      }

      if (modelsRes.success && modelsRes.data && modelsRes.data.length > 0) {
        setModels(modelsRes.data);
        const defaultMod = modelsRes.data.find((m) => m.default) ?? modelsRes.data[0];
        setSelectedModel(defaultMod.id);
        setError(null);
      } else {
        setModels([]);
        setError(
          modelsRes.error ??
            'Không có tài khoản Google Flow nào có cookie hợp lệ. Vui lòng kiểm tra lại tài khoản.',
        );
      }
      setLoading(false);
    })();

    return () => {
      active = false;
    };
  }, [setSelectedModel]);

  return (
    <div className="single-mode-panel">
      {/* 1. Model Selection */}
      <div className="flow-field-group">
        <label htmlFor="flow-model-select" className="flow-field-label">
          <span>Model</span>
          {selectedModel && (
            <span className="flow-field-badge">
              {models.find((m) => m.id === selectedModel)?.alias ?? ''}
            </span>
          )}
        </label>

        {loading ? (
          <div className="flow-select-wrapper">
            <select id="flow-model-select" className="flow-select" disabled>
              <option>Đang tải danh sách model...</option>
            </select>
            <RefreshCw
              size={14}
              className="flow-select-arrow"
              style={{ animation: 'spin 1s linear infinite' }}
            />
          </div>
        ) : error ? (
          <div className="flow-error-banner" role="alert">
            <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <AlertCircle size={14} />
              <strong>Lỗi Cookie / Phiên làm việc</strong>
            </div>
            <span>{error}</span>
          </div>
        ) : (
          <div className="flow-select-wrapper">
            <select
              id="flow-model-select"
              className="flow-select"
              value={selectedModel}
              onChange={(e) => setSelectedModel(e.target.value)}
            >
              {models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name} ({m.alias})
                </option>
              ))}
            </select>
            <ChevronDown size={14} className="flow-select-arrow" />
          </div>
        )}
      </div>

      {/* 2. Aspect Ratio Selection */}
      <div className="flow-field-group">
        <label className="flow-field-label">
          <span>Tỉ lệ</span>
          <span className="flow-field-badge">{aspectRatio}</span>
        </label>
        <div className="aspect-ratio-grid" role="group" aria-label="Chọn tỉ lệ hình ảnh">
          {ASPECT_OPTIONS.map((opt) => {
            const isActive = aspectRatio === opt.value;
            return (
              <button
                key={opt.value}
                type="button"
                className={`aspect-btn ${isActive ? 'active' : ''}`}
                onClick={() => setAspectRatio(opt.value)}
                aria-pressed={isActive}
                title={`Tỉ lệ ${opt.label}`}
              >
                <span className={`aspect-wireframe ${opt.wireframeClass}`} aria-hidden="true" />
                <span>{opt.label}</span>
              </button>
            );
          })}
        </div>
      </div>

      {/* 3. Quantity Selection (x1 to x4) */}
      <div className="flow-field-group">
        <label className="flow-field-label">
          <span>Số lượng tạo</span>
          <span className="flow-field-badge">x{quantity}</span>
        </label>
        <div className="quantity-grid" role="group" aria-label="Chọn số lượng ảnh tạo">
          {QUANTITY_OPTIONS.map((num) => {
            const isActive = quantity === num;
            return (
              <button
                key={num}
                type="button"
                className={`quantity-btn ${isActive ? 'active' : ''}`}
                onClick={() => setQuantity(num)}
                aria-pressed={isActive}
              >
                x{num}
              </button>
            );
          })}
        </div>
      </div>

      {/* 4. Round-Robin Account Pool Status */}
      <div
        className={`rotation-status-badge ${activeAccountsCount > 0 ? 'has-accounts' : 'no-accounts'}`}
        title="Tự động luân phiên tài khoản để chia đều luồng tạo"
      >
        <span className={`status-dot ${activeAccountsCount > 0 ? 'active' : 'inactive'}`} />
        <span>
          {activeAccountsCount > 0
            ? `Xoay vòng: ${activeAccountsCount} tài khoản sẵn sàng`
            : 'Chưa có tài khoản có cookie hợp lệ'}
        </span>
      </div>
    </div>
  );
};
