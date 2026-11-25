import { create } from 'zustand';
import { Button } from '../Button/Button';
import { Modal } from '../Modal/Modal';
import './ConfirmModal.css';

export interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

interface PendingConfirm {
  options: ConfirmOptions;
  resolve: (confirmed: boolean) => void;
}

const useConfirmStore = create<{
  current: PendingConfirm | null;
  queue: PendingConfirm[];
}>(() => ({
  current: null,
  queue: [],
}));

/**
 * Opens an app-styled confirmation Modal and resolves with the user's choice,
 * avoiding native browser `window.confirm` popups inside the Wails WebView.
 */
export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    const entry: PendingConfirm = { options, resolve };
    useConfirmStore.setState((state) =>
      state.current ? { queue: [...state.queue, entry] } : { current: entry },
    );
  });
}

function settleConfirm(accepted: boolean) {
  let next: PendingConfirm | null = null;
  let rest: PendingConfirm[] = [];
  const state = useConfirmStore.getState();
  const active = state.current;
  if (!active) return;
  if (state.queue.length > 0) {
    [next, ...rest] = state.queue;
  }
  useConfirmStore.setState({ current: next, queue: rest });
  active.resolve(accepted);
}

/** Global host rendering active confirmation dialogs above workspace content. */
export function ConfirmDialogHost() {
  const current = useConfirmStore((s) => s.current);
  if (!current) return null;
  const {
    title,
    message,
    confirmLabel = 'Tiếp tục',
    cancelLabel = 'Huỷ',
    danger,
  } = current.options;
  return (
    <Modal
      title={title}
      onClose={() => settleConfirm(false)}
      footer={
        <div className="confirm-dialog-actions">
          <Button variant="ghost" onClick={() => settleConfirm(false)}>
            {cancelLabel}
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} onClick={() => settleConfirm(true)}>
            {confirmLabel}
          </Button>
        </div>
      }
    >
      <p className="confirm-dialog-message">{message}</p>
    </Modal>
  );
}
