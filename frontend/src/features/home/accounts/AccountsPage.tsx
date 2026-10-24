import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ExternalLink, Plus, RefreshCw, Trash2, User } from 'lucide-react';
import './AccountsPage.css';
import { Button, Card, Icon } from '../../../components/ui';
import { ACCOUNT_AUTO_REFRESH_AFTER_MS } from '../../../constants';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { AccountStatus, AccountTier } from '../../../types';
import type { AccountProfile } from '../../../types';
import { LoginModal } from './LoginModal';
import type { LoginSession } from './LoginModal';

const statusLabels: Record<AccountStatus, string> = {
  [AccountStatus.Unverified]: 'Phiên đã lưu, cần xác minh lại',
  [AccountStatus.SignedIn]: 'Đã lưu đăng nhập Google Flow',
  [AccountStatus.Connected]: 'Đã kết nối Flow',
  [AccountStatus.Expired]: 'Cần kiểm tra đăng nhập lại',
  [AccountStatus.Error]: 'Không xác minh được phiên',
};

export const AccountsPage: React.FC = () => {
  const [profiles, setProfiles] = useState<AccountProfile[]>([]);
  const [session, setSession] = useState<LoginSession | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const busy = useRef(false);
  const alive = useRef(true);
  const addToast = useAppStore((s) => s.addToast);

  const report = useCallback(
    (cause: unknown) => {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    },
    [addToast],
  );

  const onSuccess = useCallback((profile: AccountProfile) => {
    setProfiles((list) => {
      const exists = list.some((item) => item.id === profile.id);
      if (exists) {
        return list.map((item) => (item.id === profile.id ? profile : item));
      }
      return [...list, profile];
    });
  }, []);

  useEffect(() => {
    let active = true;
    alive.current = true;
    void api.getAccounts().then(async (response) => {
      if (!active) return;
      try {
        const list = unwrap(response);
        setProfiles(list);
        setLoading(false);

        // Auto-refresh accounts in background on page load if they were verified before
        for (const account of list) {
          if (!active) break;
          const isFresh =
            account.lastVerifiedAt &&
            Date.now() - new Date(account.lastVerifiedAt).getTime() < ACCOUNT_AUTO_REFRESH_AFTER_MS;
          if (account.status === AccountStatus.Connected && isFresh) {
            continue;
          }
          if (account.lastVerifiedAt || account.status === AccountStatus.Connected) {
            if (busy.current) break;
            busy.current = true;
            setBusyId(account.id);
            try {
              const res = await api.refreshAccount(account.id);
              if (active) {
                if (res.success && res.data) {
                  onSuccess(res.data);
                } else {
                  const refreshed = await api.getAccounts();
                  if (active && refreshed.success && refreshed.data) {
                    setProfiles(refreshed.data);
                  }
                }
              }
            } catch {
              // Ignore background auto-refresh network errors so UI isn't interrupted
            } finally {
              busy.current = false;
              if (alive.current) setBusyId(null);
            }
          }
        }
      } catch (cause) {
        if (active) {
          setLoadError(cause instanceof Error ? cause.message : String(cause));
          setLoading(false);
        }
      }
    });
    return () => {
      active = false;
      alive.current = false;
    };
  }, [onSuccess]);

  const run = useCallback(
    async (id: string, action: () => Promise<void>) => {
      if (busy.current) return;
      busy.current = true;
      setBusyId(id);
      try {
        await action();
      } catch (cause) {
        report(cause);
      } finally {
        busy.current = false;
        if (alive.current) setBusyId(null);
      }
    },
    [report],
  );

  const refreshProfile = useCallback(
    async (id: string) => {
      const response = await api.refreshAccount(id);
      if (!alive.current) return;
      if (response.success) onSuccess(unwrap(response));
      else {
        const accounts = unwrap(await api.getAccounts());
        if (alive.current) setProfiles(accounts);
        unwrap(response);
      }
    },
    [onSuccess],
  );

  const handleLoginSuccess = useCallback(
    (profile: AccountProfile) => {
      onSuccess(profile);
      // Show the saved identity immediately. Enrichment is a single separate
      // operation; failure keeps the card and the backend's diagnostic message.
      void run(profile.id, () => refreshProfile(profile.id));
    },
    [onSuccess, run, refreshProfile],
  );

  const launchLogin = () => {
    if (busy.current || session) return;
    const id = crypto.randomUUID();
    // Launch is an explicit user action, not a mount effect replayed by StrictMode.
    setSession({ id, request: api.launchLoginBrowser(id) });
  };

  const closeLogin = () => {
    if (session)
      void api.cancelLogin(session.id).then((response) => {
        try {
          unwrap(response);
        } catch (cause) {
          report(cause);
        }
      });
    setSession(null);
  };

  return (
    <div className="page-shell accounts-page">
      <div className="accounts-header">
        <h1 className="accounts-title">Account</h1>
        <Button
          variant="primary"
          disabled={loading || !!loadError || busyId !== null || session !== null}
          onClick={launchLogin}
        >
          <Icon icon={Plus} />
          <span>Thêm tài khoản</span>
        </Button>
      </div>
      {loadError && (
        <div role="alert" className="accounts-error">
          {loadError}
        </div>
      )}
      <div className="accounts-content">
        {loading ? (
          <div className="accounts-empty-frame">Đang tải tài khoản…</div>
        ) : profiles.length > 0 ? (
          <div className="accounts-grid">
            {profiles.map((profile) => (
              <Card key={profile.id} className="account-profile-card">
                <div className="account-profile-header">
                  <div className="account-profile-avatar">
                    <Icon icon={User} />
                  </div>
                  <div className="account-profile-meta">
                    <div className="account-profile-email" title={profile.email}>
                      {profile.email}
                    </div>
                    <div className={`account-profile-status account-status-${profile.status}`}>
                      <span className="account-status-dot" />
                      <span>{statusLabels[profile.status] ?? 'Trạng thái chưa xác định'}</span>
                    </div>
                  </div>
                  <div className="account-credits-badge">
                    {profile.balance
                      ? `${profile.balance.credits} Credits (${profile.balance.tier === AccountTier.Unknown ? `Mã gói ${profile.balance.tierCode}` : profile.balance.tier})`
                      : 'Chưa xác minh số dư'}
                  </div>
                </div>
                <div className="account-profile-details">
                  {profile.balanceError && (
                    <div className="accounts-error" role="status">
                      Thông tin Flow chưa cập nhật đầy đủ: {profile.balanceError}
                    </div>
                  )}
                  <div className="account-detail-row">
                    <span className="account-detail-label">Gaia ID</span>
                    <span className="account-detail-value">
                      {profile.gaiaId || 'Chưa xác minh'}
                    </span>
                  </div>
                  <div className="account-detail-row">
                    <span className="account-detail-label">Cookies của phiên</span>
                    <span className="account-detail-value">{profile.cookieCount}</span>
                  </div>
                  <div className="account-detail-row">
                    <span className="account-detail-label">Hạn mức</span>
                    <span className="account-detail-value">
                      {profile.balance?.dailyLimit ?? 'Chưa xác minh'}
                    </span>
                  </div>
                  <div className="account-detail-row">
                    <span className="account-detail-label">Mã khả dụng Flow</span>
                    <span className="account-detail-value">
                      {profile.balance?.availabilityCode ?? 'Chưa có mã từ máy chủ'}
                    </span>
                  </div>
                  <div className="account-detail-row">
                    <span className="account-detail-label">Đặt lại số dư</span>
                    <span className="account-detail-value">
                      {profile.balance?.resetAt
                        ? new Date(profile.balance.resetAt).toLocaleString()
                        : 'Chưa có dữ liệu'}
                    </span>
                  </div>
                  <div className="account-detail-row">
                    <span className="account-detail-label">Xác minh gần nhất</span>
                    <span className="account-detail-value">
                      {profile.lastVerifiedAt
                        ? new Date(profile.lastVerifiedAt).toLocaleString()
                        : 'Chưa xác minh'}
                    </span>
                  </div>
                  <div className="account-detail-row">
                    <span className="account-detail-label">Profile</span>
                    <span className="account-detail-value" title={profile.profileDir}>
                      {profile.profileDir}
                    </span>
                  </div>
                </div>
                <div className="account-profile-actions">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busyId !== null || session !== null}
                    onClick={() => void run(profile.id, () => refreshProfile(profile.id))}
                  >
                    <Icon
                      icon={RefreshCw}
                      size="sm"
                      className={busyId === profile.id ? 'spin' : ''}
                    />
                    <span>{busyId === profile.id ? 'Đang xử lý…' : 'Cập nhật số dư'}</span>
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busyId !== null || session !== null}
                    onClick={() =>
                      void run(profile.id, async () => {
                        unwrap(await api.openAccountBrowser(profile.id));
                      })
                    }
                  >
                    <Icon icon={ExternalLink} size="sm" />
                    <span>Mở Chrome</span>
                  </Button>
                  <Button
                    variant="danger"
                    size="sm"
                    disabled={busyId !== null || session !== null}
                    onClick={() => {
                      if (
                        window.confirm(
                          'Xóa tài khoản và toàn bộ thư mục profile Chrome của tài khoản này? Cookies, cache và dữ liệu đăng nhập trong profile sẽ bị xóa vĩnh viễn.',
                        )
                      )
                        void run(profile.id, async () => {
                          unwrap(await api.removeAccount(profile.id));
                          if (alive.current)
                            setProfiles((list) => list.filter((item) => item.id !== profile.id));
                        });
                    }}
                  >
                    <Icon icon={Trash2} size="sm" />
                    <span>Xóa</span>
                  </Button>
                </div>
              </Card>
            ))}
          </div>
        ) : (
          !loadError && <div className="accounts-empty-frame">Chưa có profile nào đăng nhập</div>
        )}
      </div>
      {session && (
        <LoginModal session={session} onClose={closeLogin} onSuccess={handleLoginSuccess} />
      )}
    </div>
  );
};
