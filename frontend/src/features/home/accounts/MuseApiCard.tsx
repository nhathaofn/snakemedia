import React, { useState } from 'react';
import { AlertCircle, CheckCircle2, Eye, EyeOff, RefreshCw, Save, Server } from 'lucide-react';
import './MuseApiCard.css';
import { Button, Icon } from '../../../components/ui';
import { useConfigSection } from '../../../hooks';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import type { MuseAPIStatus } from '../../../types';

export const MuseApiCard: React.FC = () => {
  const { section: muse, patch, saving } = useConfigSection('muse');
  const addToast = useAppStore((s) => s.addToast);

  const savedEndpoint = muse?.apiEndpoint || 'http://127.0.0.1:18610/v1';
  const savedApiKey = muse?.apiKey || '';

  const [draftEndpoint, setDraftEndpoint] = useState<string | null>(null);
  const [draftApiKey, setDraftApiKey] = useState<string | null>(null);

  const endpoint = draftEndpoint ?? savedEndpoint;
  const apiKey = draftApiKey ?? savedApiKey;

  const [showKey, setShowKey] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<MuseAPIStatus | null>(null);

  const useApi = Boolean(muse?.useApi);

  const handleToggleUseApi = async () => {
    const next = !useApi;
    const ok = await patch({ useApi: next });
    if (!ok) return;
    addToast({
      type: 'info',
      message: next
        ? 'Đã bật chế độ API cho Muse (sử dụng gateway muse2api).'
        : 'Đã tắt chế độ API (sử dụng tài khoản trình duyệt Chrome).',
    });
  };

  const handleSave = async () => {
    const ok = await patch({
      apiEndpoint: endpoint.trim(),
      apiKey: apiKey.trim(),
    });
    if (!ok) return;
    setDraftEndpoint(null);
    setDraftApiKey(null);
    addToast({ type: 'success', message: 'Đã lưu cấu hình Muse API thành công.' });
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const res = unwrap(await api.checkMuseAPI(endpoint.trim(), apiKey.trim()));
      setTestResult(res);
      if (res.ok) {
        addToast({ type: 'success', message: 'Kết nối muse2api thành công!' });
      } else {
        addToast({ type: 'error', message: res.message || 'Kết nối thất bại.' });
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setTestResult({
        ok: false,
        latencyMs: 0,
        message: msg,
      });
      addToast({ type: 'error', message: msg });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className={`muse-api-card ${useApi ? 'is-active' : ''}`}>
      <div className="muse-api-head">
        <div className="muse-api-title-group">
          <div className="muse-api-icon">
            <Icon icon={Server} />
          </div>
          <div>
            <div className="muse-api-title">Cổng API Muse (muse2api)</div>
            <div className="muse-api-subtitle">
              Sử dụng gateway API từ dự án <code>muse2api</code> thay cho việc mở trình duyệt Chrome
              trên máy.
            </div>
          </div>
        </div>

        <div className="muse-api-toggle-group">
          <div className={`muse-api-status-badge ${useApi ? 'is-on' : 'is-off'}`}>
            <span className={`muse-api-dot ${useApi ? 'is-on' : 'is-off'}`} />
            {useApi ? 'API đang BẬT' : 'Dùng tài khoản Chrome'}
          </div>
          <Button
            variant={useApi ? 'primary' : 'outline'}
            size="sm"
            disabled={saving}
            onClick={handleToggleUseApi}
          >
            {useApi ? 'Tắt API' : 'Kích hoạt API'}
          </Button>
        </div>
      </div>

      <div className="muse-api-body">
        <div className="muse-api-fields">
          <div className="muse-api-field">
            <label className="muse-api-label" htmlFor="muse-api-endpoint">
              Địa chỉ API (Base URL)
            </label>
            <div className="muse-api-input-wrap">
              <input
                id="muse-api-endpoint"
                type="text"
                className="muse-api-input"
                placeholder="http://127.0.0.1:18610/v1"
                value={endpoint}
                onChange={(e) => setDraftEndpoint(e.target.value)}
              />
            </div>
          </div>

          <div className="muse-api-field">
            <label className="muse-api-label" htmlFor="muse-api-key">
              Khóa API (Bearer API Key)
            </label>
            <div className="muse-api-input-wrap">
              <input
                id="muse-api-key"
                type={showKey ? 'text' : 'password'}
                className="muse-api-input muse-api-input--with-toggle"
                placeholder="m2a-..."
                value={apiKey}
                onChange={(e) => setDraftApiKey(e.target.value)}
              />
              <button
                type="button"
                className="muse-api-key-toggle"
                onClick={() => setShowKey(!showKey)}
                aria-label={showKey ? 'Ẩn khóa API' : 'Hiện khóa API'}
              >
                <Icon icon={showKey ? EyeOff : Eye} />
              </button>
            </div>
          </div>
        </div>

        {testResult && (
          <div className={`muse-api-result ${testResult.ok ? 'is-success' : 'is-error'}`}>
            <Icon icon={testResult.ok ? CheckCircle2 : AlertCircle} />
            <span>{testResult.message}</span>
          </div>
        )}

        <div className="muse-api-actions">
          <Button
            variant="outline"
            size="sm"
            isLoading={testing}
            disabled={testing}
            onClick={handleTest}
          >
            <Icon icon={RefreshCw} />
            <span>Kiểm tra kết nối</span>
          </Button>
          <Button
            variant="primary"
            size="sm"
            isLoading={saving}
            disabled={saving}
            onClick={handleSave}
          >
            <Icon icon={Save} />
            <span>Lưu cấu hình API</span>
          </Button>
        </div>
      </div>
    </div>
  );
};
