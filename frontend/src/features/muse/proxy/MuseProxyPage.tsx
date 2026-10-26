import React, { useEffect, useState } from 'react';
import { Activity, CheckCircle2, RefreshCw, Save, Shield, ShieldAlert } from 'lucide-react';
import '../shared/Muse.css';
import './MuseProxyPage.css';
import { Button, Card, Icon } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import type { ProxyCheckResult } from '../../../types';

export const MuseProxyPage: React.FC = () => {
  const [proxyText, setProxyText] = useState('');
  const [initialText, setInitialText] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [checking, setChecking] = useState(false);
  const [results, setResults] = useState<ProxyCheckResult[]>([]);
  const addToast = useAppStore((s) => s.addToast);

  useEffect(() => {
    let mounted = true;
    void (async () => {
      try {
        const text = unwrap(await api.getMuseProxies());
        if (mounted) {
          setProxyText(text);
          setInitialText(text);
        }
      } catch (cause) {
        addToast({
          type: 'error',
          message: cause instanceof Error ? cause.message : 'Không tải được danh sách proxy',
        });
      } finally {
        if (mounted) setLoading(false);
      }
    })();
    return () => {
      mounted = false;
    };
  }, [addToast]);

  const handleSave = async () => {
    setSaving(true);
    try {
      unwrap(await api.saveMuseProxies(proxyText));
      setInitialText(proxyText);
      addToast({ type: 'success', message: 'Đã lưu danh sách proxy an toàn (mã hoá DPAPI)' });
    } catch (cause) {
      addToast({
        type: 'error',
        message: cause instanceof Error ? cause.message : 'Không lưu được proxy',
      });
    } finally {
      setSaving(false);
    }
  };

  const handleCheck = async () => {
    setChecking(true);
    try {
      if (proxyText !== initialText) {
        await handleSave();
      }
      const checkResults = unwrap(await api.checkMuseProxies());
      setResults(checkResults);
      const liveCount = checkResults.filter((r) => r.ok).length;
      addToast({
        type: 'info',
        message: `Đã kiểm tra ${checkResults.length} proxy: ${liveCount} sống, ${checkResults.length - liveCount} lỗi.`,
      });
    } catch (cause) {
      addToast({
        type: 'error',
        message: cause instanceof Error ? cause.message : 'Lỗi khi kiểm tra proxy',
      });
    } finally {
      setChecking(false);
    }
  };

  const getStatusBadge = (res: ProxyCheckResult) => {
    let badgeClass = 'dead';
    let label = 'Chết';
    if (res.ok) {
      badgeClass = 'live';
      label = 'Hoạt động';
    } else if (res.status === 'rate_limited') {
      badgeClass = 'rate_limited';
      label = 'Chờ lượt';
    } else if (res.status === 'limit_reached') {
      badgeClass = 'limit_reached';
      label = 'Hết hạn';
    }

    return (
      <span className={`muse-status-badge ${badgeClass}`}>
        <span className="muse-dot" />
        {label}
      </span>
    );
  };

  if (loading) {
    return (
      <div className="page-shell muse-page">
        <div className="muse-loading">Đang tải cấu hình proxy...</div>
      </div>
    );
  }

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <div>
          <h1 className="muse-title">Quản lý Proxy</h1>
          <p className="muse-subtitle">
            Cấu hình proxy tĩnh và xoay IP cho tài khoản Muse. Mật khẩu được mã hoá bằng DPAPI và
            kết nối qua Local Bridge nội bộ.
          </p>
        </div>
        <div className="muse-actions">
          <Button variant="primary" isLoading={saving} disabled={checking} onClick={handleSave}>
            <Icon icon={Save} />
            <span>Lưu danh sách</span>
          </Button>
          <Button isLoading={checking} disabled={saving} onClick={handleCheck}>
            <Icon icon={Activity} />
            <span>Kiểm tra tất cả</span>
          </Button>
        </div>
      </div>

      <div className="muse-proxy-layout">
        <div className="muse-proxy-grid">
          <Card className="muse-panel muse-proxy-editor">
            <div className="muse-form-title">
              <Icon icon={Shield} />
              <span>Kho Proxy</span>
            </div>
            <textarea
              className="muse-proxy-textarea"
              placeholder={`# Nhập mỗi dòng 1 proxy, ví dụ:\n103.150.12.34:8080\n103.150.12.34:8080:username:password\nuser:pass@103.150.12.34:8080\nsocks5://user:pass@103.150.12.34:1080\nproxyvn:YOUR_API_KEY\ntmproxy:YOUR_API_KEY\nshoplike:YOUR_API_KEY\nhttps://api.proxyprovider.com/get-ip?key=xyz`}
              value={proxyText}
              onChange={(e) => setProxyText(e.target.value)}
            />
          </Card>

          <Card className="muse-panel muse-proxy-guide">
            <div className="muse-form-title">
              <Icon icon={CheckCircle2} />
              <span>Hướng dẫn & Quy tắc bảo vệ</span>
            </div>
            <p>
              Hệ thống hỗ trợ tự động nhận diện cả <strong>proxy tĩnh</strong> và{' '}
              <strong>proxy xoay IP</strong>:
            </p>
            <ul>
              <li>
                <strong>Proxy tĩnh:</strong> <code>ip:port</code>, <code>ip:port:user:pass</code>,{' '}
                <code>user:pass@ip:port</code> hoặc <code>socks5://...</code>.
              </li>
              <li>
                <strong>Proxy xoay theo nhà cung cấp:</strong> <code>proxyvn:KEY</code>,{' '}
                <code>tmproxy:KEY</code>, <code>shoplike:KEY</code>.
              </li>
              <li>
                <strong>URL API xoay tuỳ chỉnh:</strong> Đường dẫn HTTP/HTTPS có chứa tham số query
                hoặc path (ví dụ <code>https://proxyxoay.shop/api/get.php?key=...</code>).
              </li>
            </ul>
            <div style={{ marginTop: '1rem' }}>
              <div className="muse-warning-title">
                <Icon icon={ShieldAlert} size="sm" />
                <span>Bảo vệ IP thật tuyệt đối:</span>
              </div>
              <p style={{ marginTop: '0.25rem' }}>
                Khi tài khoản được bật proxy, ứng dụng sẽ chạy Chrome thông qua Bridge nội bộ{' '}
                <code>127.0.0.1</code>. Nếu toàn bộ proxy bị lỗi hoặc chết, tiến trình sẽ chủ động
                dừng lại và báo lỗi thay vì chuyển sang IP thật của máy.
              </p>
            </div>
          </Card>
        </div>

        {results.length > 0 && (
          <Card className="muse-panel">
            <div
              className="muse-form-title"
              style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <Icon icon={Activity} />
                <span>Kết quả kiểm tra ({results.length})</span>
              </div>
              <Button size="sm" isLoading={checking} onClick={handleCheck}>
                <Icon icon={RefreshCw} size="sm" />
                <span>Kiểm tra lại</span>
              </Button>
            </div>

            <div className="muse-proxy-table-container">
              <table className="muse-proxy-table">
                <thead>
                  <tr>
                    <th>STT</th>
                    <th>Proxy / Key</th>
                    <th>Trạng thái</th>
                    <th>IP ngoại mạng</th>
                    <th>Độ trễ</th>
                    <th>Thông điệp</th>
                    <th>Kiểm tra lúc</th>
                  </tr>
                </thead>
                <tbody>
                  {results.map((res, idx) => (
                    <tr key={idx}>
                      <td>{idx + 1}</td>
                      <td>
                        <code>{res.raw}</code>
                      </td>
                      <td>{getStatusBadge(res)}</td>
                      <td>{res.ip ? `${res.ip}${res.port ? `:${res.port}` : ''}` : '-'}</td>
                      <td>{res.latencyMs ? `${res.latencyMs} ms` : '-'}</td>
                      <td>{res.message || '-'}</td>
                      <td>{new Date(res.checkedAt).toLocaleTimeString('vi-VN')}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        )}
      </div>
    </div>
  );
};
