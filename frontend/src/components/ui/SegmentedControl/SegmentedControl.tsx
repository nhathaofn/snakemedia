import { useRef } from 'react';
import type { KeyboardEvent } from 'react';
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
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const focusIndex = Math.max(
    options.findIndex((option) => option.value === value),
    0,
  );
  const move = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const direction =
      event.key === 'ArrowRight' || event.key === 'ArrowDown'
        ? 1
        : event.key === 'ArrowLeft' || event.key === 'ArrowUp'
          ? -1
          : 0;
    if (direction === 0) return;
    event.preventDefault();
    const next = (index + direction + options.length) % options.length;
    onChange(options[next].value);
    buttons.current[next]?.focus();
  };

  return (
    <div className="segmented" role="radiogroup" aria-label={ariaLabel}>
      {options.map((opt, index) => {
        const selected = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={index === focusIndex ? 0 : -1}
            ref={(button) => {
              buttons.current[index] = button;
            }}
            onKeyDown={(event) => move(event, index)}
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
