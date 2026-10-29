import { thumbUrl } from '../../../services';
import { useEffect } from 'react';
import type { ReactNode } from 'react';
import { ImagePlus, X } from 'lucide-react';
import { AccountWarning } from '../../../components/accounts';
import { Icon } from '../../../components/ui';
import { useNow } from '../../../hooks';
import { api, unwrap } from '../../../services';
import { useAccountsStore } from '../../../stores';
import { AccountProvider, MuseJobKind } from '../../../types';
import type { MuseOptions, MuseReference } from '../../../types';
import { accountReady, formatBytes } from '../../../utils';

/** Tools panel of one Muse mode: fields from the top, account status at the foot. */
export function MusePanel({ children }: { children: ReactNode }) {
  return (
    <div className="studio-mode-panel">
      {children}
      <MuseAccountStatus />
    </div>
  );
}

/**
 * How many Muse accounts can take jobs now, or where to add one. It reads the
 * clock itself (rests end over time), so the panel does not re-render on it.
 */
function MuseAccountStatus() {
  const accounts = useAccountsStore((s) => s.accounts);
  const now = useNow();
  const ready = accounts.filter(
    (a) => a.provider === AccountProvider.Muse && accountReady(a, now),
  ).length;
  if (ready === 0) {
    return (
      <AccountWarning
        provider={AccountProvider.Muse}
        message="Chưa có tài khoản Muse sẵn sàng; tác vụ sẽ chờ."
        compact
      />
    );
  }
  return (
    <div className="studio-status is-ready">
      <span className="status-dot active" />
      <span>{ready} tài khoản Muse sẵn sàng</span>
    </div>
  );
}

/** Why references cannot be added yet: Muse's limits are read on an account check. */
const LIMITS_UNKNOWN =
  'Chưa đọc được giới hạn đính kèm của Muse. Kiểm tra một tài khoản Muse đã đăng nhập (Trang chủ → Tài khoản) để thêm ảnh tham chiếu.';

/**
 * Reference images for the next jobs. What Muse accepts (count, size, formats)
 * comes from the limits read on the accounts (MuseOptions).
 */
export function ReferencesField({
  label = 'Ảnh tham chiếu',
  references,
  options,
  onChange,
  onError,
}: {
  /** Field name; inside a group already titled "Ảnh tham chiếu", a shorter one. */
  label?: string;
  references: readonly MuseReference[];
  options: MuseOptions | null;
  onChange: (references: MuseReference[]) => void;
  onError: (cause: unknown) => void;
}) {
  const maxFiles = options?.known ? options.maxFiles : 0;
  const pick = async () => {
    try {
      const picked = unwrap(await api.pickMuseReferences());
      onChange([...references, ...picked].slice(0, maxFiles));
    } catch (cause) {
      onError(cause);
    }
  };

  return (
    <div className="studio-field-group">
      <span className="studio-field-label">
        {label}
        {options?.known && (
          <span
            className="studio-field-badge"
            title={`Muse nhận ${options.imageExtensions.join(', ')}; mỗi ảnh ≤ ${formatBytes(options.maxFileBytes, 0)}.`}
          >
            {references.length}/{maxFiles}
          </span>
        )}
      </span>
      <div className="muse-references">
        {references.map((ref) => (
          <div key={ref.localUrl} className="muse-thumb" title={ref.fileName}>
            <img src={thumbUrl(ref.localUrl)} alt={ref.fileName} loading="lazy" decoding="async" />
            <button
              type="button"
              className="muse-thumb-remove"
              aria-label={`Bỏ ${ref.fileName}`}
              onClick={() => onChange(references.filter((r) => r.localUrl !== ref.localUrl))}
            >
              <Icon icon={X} size="sm" />
            </button>
          </div>
        ))}
        {/* Shown even before Muse's limits are known, locked with the reason. */}
        {(!options?.known || references.length < maxFiles) && (
          <button
            type="button"
            className="muse-thumb muse-thumb-add"
            aria-label="Thêm ảnh tham chiếu"
            title={options?.known ? 'Thêm ảnh tham chiếu' : LIMITS_UNKNOWN}
            disabled={!options?.known}
            onClick={() => void pick()}
          >
            <Icon icon={ImagePlus} size="lg" />
          </button>
        )}
      </div>
      {!options?.known && <span className="studio-field-hint">{LIMITS_UNKNOWN}</span>}
    </div>
  );
}

/**
 * The frame shape and (video) length asked of Muse in the message, from the
 * values Muse is known to make (MuseOptions). "Theo prompt" asks for nothing.
 */
export function FormatFields({
  kind,
  options,
  ratio,
  duration,
  onChange,
}: {
  kind: MuseJobKind;
  options: MuseOptions | null;
  ratio: string;
  duration: number;
  onChange: (change: { ratio?: string; duration?: number }) => void;
}) {
  const ratios = (options?.ratios ?? []).filter((r) => r.kinds.includes(kind));
  const video = kind === MuseJobKind.Video;
  const durations = options?.durations ?? [];
  // A length kept from an older build that Muse no longer offers falls back
  // to "Theo prompt" rather than failing the next submit.
  const stale = video && duration !== 0 && durations.length > 0 && !durations.includes(duration);
  useEffect(() => {
    if (stale) onChange({ duration: 0 });
  }, [stale, onChange]);
  return (
    <div className="muse-format-fields">
      <label className="studio-field-group">
        <span className="studio-field-label">Tỉ lệ</span>
        <select
          className="batch-input batch-select"
          value={ratio}
          onChange={(e) => onChange({ ratio: e.target.value })}
        >
          <option value="">Theo prompt</option>
          {ratios.map((r) => (
            <option key={r.value} value={r.value}>
              {r.value} {r.label}
            </option>
          ))}
        </select>
      </label>
      {video && (
        <label className="studio-field-group">
          <span className="studio-field-label">Thời lượng</span>
          <select
            className="batch-input batch-select"
            value={duration}
            onChange={(e) => onChange({ duration: Number(e.target.value) })}
          >
            <option value={0}>Theo prompt</option>
            {durations.map((d) => (
              <option key={d} value={d}>
                {durationLabel(d, options?.clipSeconds ?? 0)}
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  );
}

/**
 * A video length as the select shows it: one clip, or a long video Muse makes
 * as several clips joined in one chat, each a next moment of the prompt.
 */
function durationLabel(seconds: number, clipSeconds: number): string {
  if (clipSeconds <= 0 || seconds <= clipSeconds) return `${seconds} giây`;
  return `${seconds} giây · ghép ${Math.ceil(seconds / clipSeconds)} cảnh`;
}
