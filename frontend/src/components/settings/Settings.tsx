import React, { useState } from 'react';
import type { ReactNode } from 'react';
import './Settings.css';
import { Card } from '../ui/Card/Card';
import { SegmentedControl } from '../ui/SegmentedControl/SegmentedControl';

/** A settings screen: title, then sections. */
export const SettingsShell: React.FC<{ title: string; children: ReactNode }> = ({
  title,
  children,
}) => (
  <div className="page-shell settings-page">
    <h1 className="settings-title">{title}</h1>
    {children}
  </div>
);

interface SettingsSectionProps {
  title: string;
  children: ReactNode;
  /** Rows for rarely changed values, folded under "Nâng cao". */
  advanced?: ReactNode;
}

/** A titled card of setting rows; advanced rows start folded. */
export const SettingsSection: React.FC<SettingsSectionProps> = ({ title, children, advanced }) => (
  <section className="settings-section">
    <h2 className="settings-section-title">{title}</h2>
    <Card className="settings-card">
      {children}
      {advanced && (
        <details className="settings-advanced">
          <summary>Nâng cao</summary>
          {advanced}
        </details>
      )}
    </Card>
  </section>
);

interface SettingRowProps {
  label: string;
  hint?: string;
  children: ReactNode;
}

export const SettingRow: React.FC<SettingRowProps> = ({ label, hint, children }) => (
  <div className="settings-row">
    <div className="settings-row-text">
      <div className="settings-row-label">{label}</div>
      {hint && <div className="settings-row-hint">{hint}</div>}
    </div>
    <div className="settings-row-control">{children}</div>
  </div>
);

interface NumberSettingProps {
  value: number;
  min: number;
  max: number;
  step?: number;
  unit?: string;
  disabled?: boolean;
  'aria-label': string;
  onCommit: (value: number) => void;
}

/**
 * Number input that saves when it loses focus or on Enter, clamped to
 * min..max, so typing never sends half-written values to the backend.
 */
export const NumberSetting: React.FC<NumberSettingProps> = ({
  value,
  min,
  max,
  step = 1,
  unit,
  disabled,
  'aria-label': ariaLabel,
  onCommit,
}) => {
  // null while not editing: the input then shows the saved value.
  const [draft, setDraft] = useState<string | null>(null);

  const commit = () => {
    if (draft === null) return;
    setDraft(null);
    const parsed = Number(draft);
    if (draft.trim() === '' || !Number.isFinite(parsed)) return;
    const next = Math.min(max, Math.max(min, parsed));
    if (next !== value) onCommit(next);
  };

  return (
    <span className="settings-number">
      <input
        type="number"
        className="settings-input"
        aria-label={ariaLabel}
        min={min}
        max={max}
        step={step}
        value={draft ?? String(value)}
        disabled={disabled}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => e.key === 'Enter' && commit()}
      />
      {unit && <span className="settings-unit">{unit}</span>}
    </span>
  );
};

interface TextSettingProps {
  value: string;
  placeholder?: string;
  disabled?: boolean;
  'aria-label': string;
  onCommit: (value: string) => void;
}

/** Text input that saves when it loses focus or on Enter. */
export const TextSetting: React.FC<TextSettingProps> = ({
  value,
  placeholder,
  disabled,
  'aria-label': ariaLabel,
  onCommit,
}) => {
  // null while not editing: the input then shows the saved value.
  const [draft, setDraft] = useState<string | null>(null);
  const commit = () => {
    if (draft === null) return;
    setDraft(null);
    const next = draft.trim();
    if (next !== value) onCommit(next);
  };
  return (
    <input
      type="text"
      className="settings-input settings-input-wide"
      aria-label={ariaLabel}
      placeholder={placeholder}
      value={draft ?? value}
      disabled={disabled}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => e.key === 'Enter' && commit()}
    />
  );
};

const onOff = [
  { value: 'true', label: 'Bật' },
  { value: 'false', label: 'Tắt' },
] as const;

interface ToggleSettingProps {
  value: boolean;
  disabled?: boolean;
  'aria-label': string;
  onChange: (value: boolean) => void;
}

export const ToggleSetting: React.FC<ToggleSettingProps> = ({
  value,
  disabled,
  'aria-label': ariaLabel,
  onChange,
}) => (
  <SegmentedControl
    aria-label={ariaLabel}
    options={onOff}
    value={value ? 'true' : 'false'}
    disabled={disabled}
    onChange={(next) => onChange(next === 'true')}
  />
);
