import { AlertCircle, ChevronDown, RefreshCw } from 'lucide-react';
import { Icon } from '../../../components/ui';

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
  /** Text shown when the backend returned no options. */
  emptyLabel?: string;
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
  emptyLabel = 'Không có lựa chọn',
}: SelectFieldProps<T>) {
  return (
    <div className="studio-field-group">
      <label htmlFor={id} className="studio-field-label">
        <span>{label}</span>
      </label>
      {loading ? (
        <div className="flow-select-wrapper">
          <select id={id} className="flow-select" disabled>
            <option>Đang tải...</option>
          </select>
          <Icon icon={RefreshCw} size="sm" className="flow-select-arrow spin" />
        </div>
      ) : error ? (
        <div className="flow-error-banner" role="alert">
          <div className="flow-error-banner-title">
            <Icon icon={AlertCircle} size="sm" />
            <strong>{error.title}</strong>
          </div>
          <span>{error.message}</span>
        </div>
      ) : options.length === 0 ? (
        <div className="flow-select-wrapper">
          <select id={id} className="flow-select" disabled>
            <option>{emptyLabel}</option>
          </select>
          <Icon icon={ChevronDown} size="sm" className="flow-select-arrow" />
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
          <Icon icon={ChevronDown} size="sm" className="flow-select-arrow" />
        </div>
      )}
    </div>
  );
}
