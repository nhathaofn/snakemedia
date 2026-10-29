import React, { useState } from 'react';
import { Trash2 } from 'lucide-react';
import { Button, Icon } from '../../../components/ui';
import { api, unwrapList } from '../../../services';
import { useAppStore } from '../../../stores';
import type { MuseCleanResult } from '../../../types';

/**
 * "Dọn đoạn chat phụ": sends the cleanup message on every signed-in Muse
 * account and lists what each answered. Accounts with a job in progress are
 * skipped by the backend, so a running job never loses its chat.
 */
export const CleanChatsButton: React.FC<{ disabled?: boolean }> = ({ disabled }) => {
  const addToast = useAppStore((s) => s.addToast);
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<MuseCleanResult[]>([]);

  const clean = async () => {
    if (
      !window.confirm(
        'Gửi tin nhắn dọn cho mọi tài khoản Muse đã đăng nhập? Muse sẽ xoá các đoạn chat khác của tài khoản. Tài khoản còn tác vụ đang chạy được bỏ qua.',
      )
    )
      return;
    setBusy(true);
    try {
      setResults(unwrapList(await api.cleanMuseChats()));
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="muse-clean-chats">
      <Button onClick={() => void clean()} disabled={disabled || busy}>
        <Icon icon={Trash2} />
        <span>{busy ? 'Đang dọn… (chờ Muse trả lời)' : 'Dọn đoạn chat phụ'}</span>
      </Button>
      {results.length > 0 && (
        <ul className="muse-clean-results">
          {results.map((r) => (
            <li key={r.accountId} className={r.error ? 'is-error' : undefined}>
              <strong>{r.account}</strong>: {r.error || r.reply || 'Muse đã nhận, không trả lời.'}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
