import { AlertCircle, ChevronDown, RefreshCw } from 'lucide-react';

export interface SelectOption<T extends string | number> {
  value: T;
  label: string;
}

export interface SelectFieldProps<T extends string | number> {
  id: string;
  label: string;
  value: T;
  options: readonly SelectOption<T>[];
  onChange: (value: T) => void;
  /** Shows a disabled select with a spinner while options load. */
  loading?: boolean;
  /** Replaces the select with an error banner (e.g. models could not be fetched). */
  error?: { title: string; message: string } | null;
}

/** Labelled dropdown used for model, ratio and duration on every Flow page. */
export function SelectField<T extends string | number>({
  id,
  label,
  value,
  options,
  onChange,
  loading = false,
  error = null,
}: SelectFieldProps<T>) {
  return (
    <div className="flow-field-group">
      <label htmlFor={id} className="flow-field-label">
        <span>{label}</span>
      </label>
      {loading ? (
        <div className="flow-select-wrapper">
          <select id={id} className="flow-select" disabled>
            <option>Đang tải...</option>
          </select>
          <RefreshCw size={14} className="flow-select-arrow spin" />
        </div>
      ) : error ? (
        <div className="flow-error-banner" role="alert">
          <div className="flow-error-banner-title">
            <AlertCircle size={14} />
            <strong>{error.title}</strong>
          </div>
          <span>{error.message}</span>
        </div>
      ) : options.length === 0 ? (
        <div className="flow-select-wrapper">
          <select id={id} className="flow-select" disabled>
            <option>Không có lựa chọn</option>
          </select>
          <ChevronDown size={14} className="flow-select-arrow" />
        </div>
      ) : (
        <div className="flow-select-wrapper">
          <select
            id={id}
            className="flow-select"
            value={value}
            onChange={(e) => {
              const option = options.find((opt) => String(opt.value) === e.target.value);
              if (option) onChange(option.value);
            }}
          >
            {options.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
          <ChevronDown size={14} className="flow-select-arrow" />
        </div>
      )}
    </div>
  );
}

/** Output count picker, x1 to the backend's per-request maximum. */
export function QuantityField({
  value,
  max,
  onChange,
  ariaLabel,
}: {
  value: number;
  max: number;
  onChange: (value: number) => void;
  ariaLabel: string;
}) {
  return (
    <div className="flow-field-group">
      <label className="flow-field-label">
        <span>Số lượng</span>
      </label>
      <div className="quantity-grid" role="group" aria-label={ariaLabel}>
        {Array.from({ length: max }, (_, i) => i + 1).map((num) => (
          <button
            key={num}
            type="button"
            className={`quantity-btn ${value === num ? 'active' : ''}`}
            onClick={() => onChange(num)}
            aria-pressed={value === num}
          >
            x{num}
          </button>
        ))}
      </div>
    </div>
  );
}
