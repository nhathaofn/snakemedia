import './SegmentedControl.css';
import type { IconComponent } from '../../../types';
import { Icon } from '../Icon/Icon';

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  icon?: IconComponent;
}

export interface SegmentedControlProps<T extends string> {
  options: readonly SegmentedOption<T>[];
  value: T | undefined;
  onChange: (value: T) => void;
  disabled?: boolean;
  'aria-label': string;
}

/** Single-choice button group (radio semantics). */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  disabled = false,
  'aria-label': ariaLabel,
}: SegmentedControlProps<T>) {
  return (
    <div className="segmented" role="radiogroup" aria-label={ariaLabel}>
      {options.map((opt) => {
        const selected = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={selected}
            className={`segmented-option ${selected ? 'selected' : ''}`}
            disabled={disabled}
            onClick={() => !selected && onChange(opt.value)}
          >
            {opt.icon && <Icon icon={opt.icon} size="sm" />}
            <span>{opt.label}</span>
          </button>
        );
      })}
    </div>
  );
}
