import { useId, useRef } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';
import './Tabs.css';
import type { IconComponent } from '../../../types';
import { Icon } from '../Icon/Icon';

export interface TabItem<T extends string> {
  value: T;
  label: string;
  icon?: IconComponent;
  content: ReactNode;
}

export interface TabsProps<T extends string> {
  items: readonly TabItem<T>[];
  value: T;
  onChange: (value: T) => void;
  'aria-label': string;
}

/**
 * Tab strip with one panel per tab. Inactive panels stay mounted (hidden), so input
 * typed in one tab survives switching to another. Arrow keys, Home and End move
 * between tabs (WAI-ARIA tabs pattern).
 */
export function Tabs<T extends string>({
  items,
  value,
  onChange,
  'aria-label': ariaLabel,
}: TabsProps<T>) {
  const baseId = useId();
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const tabId = (v: T) => `${baseId}-tab-${v}`;
  const panelId = (v: T) => `${baseId}-panel-${v}`;

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const current = items.findIndex((item) => item.value === value);
    const last = items.length - 1;
    const next: Record<string, number> = {
      ArrowRight: current === last ? 0 : current + 1,
      ArrowLeft: current === 0 ? last : current - 1,
      Home: 0,
      End: last,
    };
    const index = next[event.key];
    if (index === undefined) return;
    event.preventDefault();
    onChange(items[index].value);
    tabRefs.current[index]?.focus();
  };

  return (
    <div className="tabs">
      <div className="tabs-list" role="tablist" aria-label={ariaLabel} onKeyDown={onKeyDown}>
        {items.map((item, index) => {
          const selected = item.value === value;
          return (
            <button
              key={item.value}
              ref={(el) => {
                tabRefs.current[index] = el;
              }}
              id={tabId(item.value)}
              type="button"
              role="tab"
              aria-selected={selected}
              aria-controls={panelId(item.value)}
              tabIndex={selected ? 0 : -1}
              className={`tabs-tab ${selected ? 'active' : ''}`}
              onClick={() => !selected && onChange(item.value)}
            >
              {item.icon && <Icon icon={item.icon} size="sm" />}
              <span>{item.label}</span>
            </button>
          );
        })}
      </div>
      {items.map((item) => (
        <div
          key={item.value}
          id={panelId(item.value)}
          role="tabpanel"
          aria-labelledby={tabId(item.value)}
          className="tabs-panel"
          hidden={item.value !== value}
        >
          {item.content}
        </div>
      ))}
    </div>
  );
}
