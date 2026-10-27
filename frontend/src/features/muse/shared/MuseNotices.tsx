import { useState } from 'react';
import { X } from 'lucide-react';
import { AccountWarning } from '../../../components/accounts';
import { Button, Icon } from '../../../components/ui';
import { useNow } from '../../../hooks';
import { AccountProvider } from '../../../types';
import type { Account } from '../../../types';
import { accountReady } from '../../../utils';

/**
 * The warning shown while no Muse account can take jobs. It reads the clock
 * itself (rests end over time), so the queue does not re-render on it.
 */
export function NoReadyAccountWarning({ accounts }: { accounts: readonly Account[] }) {
  const now = useNow();
  if (accounts.some((a) => accountReady(a, now))) return null;
  return (
    <AccountWarning
      provider={AccountProvider.Muse}
      message="Chưa có tài khoản Muse nào sẵn sàng. Tác vụ sẽ chờ tới khi có tài khoản đăng nhập."
    />
  );
}

/**
 * Shown when the job list was unreadable on start and moved aside: jobs it
 * held are no longer listed, and sent ones may still be running on muse.ai.
 */
export function JournalWarning({ message }: { message: string }) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) return null;
  return (
    <div role="alert" className="muse-alert muse-alert-warning">
      <span>
        {message}. Các tác vụ cũ không còn trong danh sách; tác vụ đã gửi có thể vẫn đang chạy — hãy
        kiểm tra trên muse.ai trước khi tạo lại.
      </span>
      <Button size="sm" variant="ghost" aria-label="Ẩn cảnh báo" onClick={() => setDismissed(true)}>
        <Icon icon={X} />
      </Button>
    </div>
  );
}
