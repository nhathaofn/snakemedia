import React, { useEffect, useRef, useState } from 'react';
import { AlertCircle, CheckCircle2, Globe, RefreshCw, X } from 'lucide-react';
import './LoginModal.css';
import { Button, Icon, Spinner } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import type {
  AccountProfile,
  ApiResponse,
  LoginProcessInfo,
  LoginStatusInfo,
} from '../../../types';
import { createLoginMonitor } from './loginMonitor';

export interface LoginSession {
  id: string;
  request: Promise<ApiResponse<LoginProcessInfo>>;
}
export interface LoginModalProps {
  session: LoginSession;
  onClose: () => void;
  onSuccess: (profile: AccountProfile) => void;
}

export const LoginModal: React.FC<LoginModalProps> = ({ session, onClose, onSuccess }) => {
  const appInfo = useAppStore((s) => s.appInfo);
  const [info, setInfo] = useState<LoginProcessInfo | null>(null);
  const [status, setStatus] = useState<LoginStatusInfo | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const monitor = useRef<ReturnType<typeof createLoginMonitor> | null>(null);
  const mounted = useRef(false);

  useEffect(() => {
    let active = true;
    mounted.current = true;
    void session.request.then((response) => {
      if (!active) return;
      try {
        const launched = unwrap(response);
        setInfo(launched);
        const next = createLoginMonitor({
          info: launched,
          check: async () => unwrap(await api.checkLoginStatus(session.id)),
          onStatus: (value) => {
            setStatus(value);
            setError(null);
          },
          onSuccess: (profile) => {
            setChecking(false);
            onSuccess(profile);
          },
          onError: setError,
          onChecking: setChecking,
        });
        monitor.current = next;
        next.start();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    });
    return () => {
      active = false;
      mounted.current = false;
      monitor.current?.stop();
      // StrictMode immediately runs setup again. Cancel only a real unmount,
      // including navigation away, without creating a second launch request.
      queueMicrotask(() => {
        if (!mounted.current) void api.cancelLogin(session.id);
      });
    };
  }, [session, onSuccess]);

  return (
    <div className="login-modal-overlay" onClick={onClose}>
      <div
        className="login-modal-container"
        role="dialog"
        aria-modal="true"
        aria-labelledby="login-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="login-modal-header">
          <div id="login-title" className="login-modal-title">
            <Icon icon={Globe} />
            <span>Đăng nhập tài khoản Google Flow</span>
          </div>
          <button className="login-modal-close-btn" onClick={onClose} aria-label="Đóng">
            <Icon icon={X} />
          </button>
        </div>
        <div className="login-modal-body">
          {error && (
            <div className="login-step-item login-error" role="alert">
              <Icon icon={AlertCircle} />
              <span>{error}</span>
            </div>
          )}
          <div className="login-step-list">
            <div className="login-step-item">
              {info ? (
                <Icon icon={CheckCircle2} />
              ) : error ? (
                <Icon icon={AlertCircle} />
              ) : (
                <Spinner />
              )}
              <div className="login-step-content">
                <span className="login-step-title">1. Mở Chrome với profile riêng</span>
                <span className="login-step-desc">
                  {info
                    ? 'Trình duyệt đã sẵn sàng. Đăng nhập trên cửa sổ Chrome vừa mở.'
                    : 'Đang khởi chạy trình duyệt…'}
                </span>
              </div>
            </div>
            <div className="login-step-item">
              <Icon icon={Globe} />
              <div className="login-step-content">
                <span className="login-step-title">2. Nhận diện tài khoản Flow</span>
                <span className="login-step-desc">
                  {status?.message || 'Hệ thống sẽ kiểm tra sau khi trình duyệt sẵn sàng.'}
                </span>
              </div>
            </div>
            {status?.isLoggedIn && (
              <div className="login-step-item login-complete">
                <Icon icon={CheckCircle2} />
                <div className="login-step-content">
                  <span className="login-step-title">{status.profile?.email}</span>
                  <span className="login-step-desc">
                    Tài khoản đã được lưu{appInfo ? ` vào ${appInfo.productName}` : ''}. Số dư được
                    cập nhật riêng trên thẻ tài khoản.
                  </span>
                </div>
              </div>
            )}
          </div>
          {info && (
            <div className="login-info-card">
              <div className="login-info-row">
                <span className="login-info-label">Profile</span>
                <span className="login-info-value" title={info.profileDir}>
                  {info.profileDir}
                </span>
              </div>
              <div className="login-info-row">
                <span className="login-info-label">Thời gian chờ tối đa</span>
                <span>{new Date(info.expiresAt).toLocaleTimeString()}</span>
              </div>
            </div>
          )}
        </div>
        <div className="login-modal-footer">
          {!status?.isLoggedIn && (
            <Button
              variant="outline"
              size="sm"
              disabled={!info || checking}
              onClick={() => void monitor.current?.check()}
            >
              <Icon icon={RefreshCw} size="sm" />
              <span>{checking ? 'Đang kiểm tra…' : 'Kiểm tra lại'}</span>
            </Button>
          )}
          <Button
            variant={status?.isLoggedIn ? 'primary' : 'secondary'}
            size="sm"
            onClick={onClose}
          >
            {status?.isLoggedIn ? 'Hoàn tất' : 'Đóng'}
          </Button>
        </div>
      </div>
    </div>
  );
};
