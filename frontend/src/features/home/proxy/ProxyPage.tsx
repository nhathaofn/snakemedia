import React, { useEffect, useState } from 'react';
import { Activity, Save } from 'lucide-react';
import './ProxyPage.css';
import { Button, Card, Icon } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import type { ProxyCheckResult } from '../../../types';

const placeholder = `# Mỗi dòng một proxy, ví dụ:
103.150.12.34:8080
103.150.12.34:8080:username:password
user:pass@103.150.12.34:8080
socks5://user:pass@103.150.12.34:1080
proxyvn:YOUR_API_KEY
tmproxy:YOUR_API_KEY
shoplike:YOUR_API_KEY
https://api.proxyprovider.com/get-ip?key=xyz`;

const statusLabels: Record<string, string> = {
  live: 'Hoạt động',
  rate_limited: 'Chờ lượt',
  limit_reached: 'Hết hạn',
  dead: 'Lỗi',
};

/**
 * The proxy list every account shares. An account set to "auto" takes the
 * least used live line; an account never falls back to the real IP.
 */
export const ProxyPage: React.FC = () => {
  const addToast = useAppStore((s) => s.addToast);
  const [text, setText] = useState('');
  const [saved, setSaved] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<'save' | 'check' | null>(null);
  const [results, setResults] = useState<ProxyCheckResult[]>([]);

  useEffect(() => {
    let active = true;
    void api.getProxies().then((res) => {
      if (!active) return;
      if (res.success && res.data !== undefined) {
        setText(res.data);
        setSaved(res.data);
      } else {
        addToast({ type: 'error', message: res.error ?? 'Không tải được danh sách proxy' });
      }
      setLoading(false);
    });
    return () => {
      active = false;
    };
  }, [addToast]);

  const fail = (cause: unknown) =>
    addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });

  const save = async () => {
    unwrap(await api.saveProxies(text));
    setSaved(text);
  };

  const onSave = async () => {
    setBusy('save');
    try {
      await save();
      addToast({ type: 'success', message: 'Đã lưu danh sách proxy (mã hoá trên máy này).' });
    } catch (cause) {
      fail(cause);
    } finally {
      setBusy(null);
    }
  };

  const onCheck = async () => {
    setBusy('check');
    try {
      if (text !== saved) await save();
      const checked = unwrap(await api.checkProxies());
      setResults(checked);
      const live = checked.filter((r) => r.ok).length;
      addToast({
        type: 'info',
        message: `Đã kiểm tra ${checked.length} proxy: ${live} hoạt động, ${checked.length - live} lỗi.`,
      });
    } catch (cause) {
      fail(cause);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="page-shell proxy-page">
      <div className="proxy-header">
        <h1 className="proxy-title">Proxy</h1>
        <div className="proxy-actions">
          <Button
            variant="primary"
            isLoading={busy === 'save'}
            disabled={loading || !!busy}
            onClick={onSave}
          >
            <Icon icon={Save} />
            <span>Lưu</span>
          </Button>
          <Button isLoading={busy === 'check'} disabled={loading || !!busy} onClick={onCheck}>
            <Icon icon={Activity} />
            <span>Kiểm tra tất cả</span>
          </Button>
        </div>
      </div>

      <p className="proxy-hint">
        Dùng chung cho mọi tài khoản (Flow, Muse). Tài khoản để chế độ “Tự chọn” nhận proxy ít người
        dùng nhất còn sống; proxy riêng hoặc “Không dùng proxy” đặt ở thẻ tài khoản. Chrome và các
        yêu cầu của app đi qua cầu nối nội bộ 127.0.0.1; khi proxy lỗi, tài khoản dừng lại chứ không
        dùng IP thật.
      </p>

      <div className="proxy-layout">
        <Card className="proxy-panel">
          <textarea
            className="proxy-textarea"
            aria-label="Danh sách proxy"
            placeholder={placeholder}
            spellCheck={false}
            value={text}
            disabled={loading}
            onChange={(e) => setText(e.target.value)}
          />
        </Card>
        <Card className="proxy-panel proxy-guide">
          <div className="proxy-guide-title">Định dạng hỗ trợ</div>
          <ul>
            <li>
              Tĩnh: <code>ip:port</code>, <code>ip:port:user:pass</code>,{' '}
              <code>user:pass@ip:port</code>, <code>socks5://…</code>
            </li>
            <li>
              Xoay theo nhà cung cấp: <code>proxyvn:KEY</code>, <code>tmproxy:KEY</code>,{' '}
              <code>shoplike:KEY</code>
            </li>
            <li>
              Link API xoay tuỳ chỉnh: URL có path hoặc query, ví dụ{' '}
              <code>https://…/get.php?key=…</code>
            </li>
            <li>
              Dòng bắt đầu bằng <code>#</code> hoặc <code>//</code> là ghi chú.
            </li>
          </ul>
        </Card>
      </div>

      {results.length > 0 && (
        <Card className="proxy-panel">
          <div className="proxy-table-wrap">
            <table className="proxy-table">
              <thead>
                <tr>
                  <th>#</th>
                  <th>Proxy</th>
                  <th>Trạng thái</th>
                  <th>IP</th>
                  <th>Độ trễ</th>
                  <th>Ghi chú</th>
                </tr>
              </thead>
              <tbody>
                {results.map((res, index) => (
                  <tr key={`${index}-${res.raw}`}>
                    <td>{index + 1}</td>
                    <td>
                      <code>{res.raw}</code>
                    </td>
                    <td>
                      <span className={`proxy-badge proxy-badge-${res.ok ? 'live' : res.status}`}>
                        {statusLabels[res.ok ? 'live' : res.status] ?? res.status}
                      </span>
                    </td>
                    <td>{res.ip ? `${res.ip}${res.port ? `:${res.port}` : ''}` : '–'}</td>
                    <td>{res.latencyMs ? `${res.latencyMs} ms` : '–'}</td>
                    <td>{res.message || '–'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </div>
  );
};
