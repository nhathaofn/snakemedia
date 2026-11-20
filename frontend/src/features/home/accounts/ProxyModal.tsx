import React, { useEffect, useState } from 'react';
import { Check } from 'lucide-react';
import { Button, Icon, Modal } from '../../../components/ui';
import { api } from '../../../services';
import type { Account } from '../../../types';
import { proxyLines } from './AccountPanel';

export interface ProxyOption {
  value: string;
  label: string;
  description?: string;
  mono?: boolean;
  active: boolean;
}

/** Builds selectable proxy options from the shared list, marking the active choice. */
export function buildProxyOptions(currentProxy: string, listed: readonly string[]): ProxyOption[] {
  const options: ProxyOption[] = [
    {
      value: 'none',
      label: 'Không dùng proxy',
      description: 'Kết nối trực tiếp',
      active: currentProxy === 'none',
    },
    {
      value: '',
      label: 'Tự chọn từ danh sách proxy',
      description:
        listed.length > 0 ? `${listed.length} proxy khả dụng` : 'Cần có proxy trong danh sách',
      active: currentProxy === '',
    },
  ];
  for (const line of listed) {
    options.push({
      value: line,
      label: line,
      mono: true,
      active: currentProxy === line,
    });
  }
  return options;
}

interface ProxyModalProps {
  account: Account;
  busy: boolean;
  onSelectProxy: (proxyLine: string) => void;
  onOpenProxyList: () => void;
  onClose: () => void;
}

export const ProxyModal: React.FC<ProxyModalProps> = ({
  account,
  busy,
  onSelectProxy,
  onOpenProxyList,
  onClose,
}) => {
  const [listed, setListed] = useState<string[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [customProxy, setCustomProxy] = useState('');

  useEffect(() => {
    let active = true;
    const current = account.proxy;
    void api.getProxies().then((res) => {
      if (!active) return;
      const lines = res.success && res.data ? proxyLines(res.data) : [];
      setListed(lines);
      setLoaded(true);
      if (current && current !== 'none' && !lines.includes(current)) {
        setCustomProxy(current);
      } else {
        setCustomProxy('');
      }
    });
    return () => {
      active = false;
    };
  }, [account.id, account.proxy]);

  const options = buildProxyOptions(account.proxy, listed);
  const isCustomActive =
    !!account.proxy && account.proxy !== 'none' && loaded && !listed.includes(account.proxy);

  const submitCustom = (event: React.FormEvent) => {
    event.preventDefault();
    const line = customProxy.trim();
    if (!line || busy) return;
    onSelectProxy(line);
  };

  return (
    <Modal
      title={`Đổi proxy cho "${account.name}"`}
      subtitle="Bấm để áp dụng ngay; Chrome khởi động lại với proxy mới ở lần dùng kế tiếp."
      onClose={onClose}
      footer={
        <>
          <Button type="button" variant="ghost" size="sm" onClick={onOpenProxyList}>
            Quản lý danh sách proxy
          </Button>
          <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onClose}>
            Đóng
          </Button>
        </>
      }
    >
      <div className="proxy-modal-body">
        <div className="proxy-option-list" role="listbox" aria-label="Danh sách proxy">
          {options.map((opt) => (
            <button
              key={opt.value || '__auto__'}
              type="button"
              role="option"
              aria-selected={opt.active}
              disabled={busy}
              className={`proxy-option${opt.active ? ' proxy-option-active' : ''}`}
              onClick={() => onSelectProxy(opt.value)}
            >
              <div className="proxy-option-text">
                <span className={opt.mono ? 'account-mono' : undefined}>{opt.label}</span>
                {opt.description && <span className="proxy-option-desc">{opt.description}</span>}
              </div>
              {opt.active && (
                <span className="proxy-option-badge">
                  <Icon icon={Check} size="sm" />
                  <span>Đang dùng</span>
                </span>
              )}
            </button>
          ))}
        </div>

        {loaded && listed.length === 0 && (
          <p className="proxy-modal-empty">
            Danh sách proxy chung đang trống. Bạn có thể{' '}
            <button type="button" className="account-link" onClick={onOpenProxyList}>
              thêm proxy tại mục Proxy
            </button>{' '}
            hoặc nhập nhanh một proxy riêng bên dưới.
          </p>
        )}

        <form className="proxy-custom-form" onSubmit={submitCustom}>
          <label className="accounts-field">
            <span>
              Hoặc nhập proxy riêng (ip:port, ip:port:user:pass, socks5://…, proxyvn:KEY…)
              {isCustomActive && ' · Đang dùng'}
            </span>
            <div className="proxy-custom-row">
              <input
                className="accounts-input"
                value={customProxy}
                placeholder="Ví dụ: 1.2.3.4:8080:user:pass"
                disabled={busy}
                onChange={(e) => setCustomProxy(e.target.value)}
              />
              <Button
                type="submit"
                variant="primary"
                isLoading={busy}
                disabled={!customProxy.trim()}
              >
                Áp dụng
              </Button>
            </div>
          </label>
        </form>
      </div>
    </Modal>
  );
};
