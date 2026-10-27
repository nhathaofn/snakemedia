import { useCallback, useEffect } from 'react';
import { FolderOpen } from 'lucide-react';
import { api, unwrap } from '../../services';
import { useAppStore } from '../../stores';
import type { AccountProvider } from '../../types';
import { Button, Icon } from '../ui';

/**
 * The folder a batch saves its files into. It starts at the folder the
 * provider's last batch used (or the app's default) and the choice is
 * remembered for the next batches. Files are named "<n>-snakemedia-<provider>-<code>".
 */
export function OutputFolderField({
  provider,
  value,
  onChange,
}: {
  provider: AccountProvider;
  value: string;
  onChange: (dir: string) => void;
}) {
  const addToast = useAppStore((s) => s.addToast);
  const report = useCallback(
    (title: string, cause: unknown) =>
      addToast({
        type: 'error',
        title,
        message: cause instanceof Error ? cause.message : String(cause),
      }),
    [addToast],
  );

  // An empty field loads the provider's folder; onChange must be stable (a
  // state or store setter).
  useEffect(() => {
    if (value) return;
    let active = true;
    void api
      .getBatchOutputDirectory(provider)
      .then((res) => {
        if (active) onChange(unwrap(res));
      })
      .catch((cause: unknown) => {
        if (active) report('Không tải được thư mục lưu', cause);
      });
    return () => {
      active = false;
    };
  }, [onChange, provider, report, value]);

  const choose = async () => {
    try {
      const res = unwrap(await api.chooseBatchOutputDirectory(provider));
      if (!res.canceled && res.path) onChange(res.path);
    } catch (cause) {
      report('Không chọn được thư mục lưu', cause);
    }
  };

  return (
    <div className="studio-field-group">
      <span className="studio-field-label">Thư mục lưu</span>
      <div className="batch-folder-control">
        <output className={value ? undefined : 'is-empty'} title={value}>
          {value || 'Chưa có'}
        </output>
        <Button
          type="button"
          variant="outline"
          size="sm"
          title="Chọn thư mục lưu"
          onClick={() => void choose()}
        >
          <Icon icon={FolderOpen} size="sm" />
          <span>Chọn</span>
        </Button>
      </div>
    </div>
  );
}
