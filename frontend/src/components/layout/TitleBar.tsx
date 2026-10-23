import React from 'react';
import { Minus, Square, X } from 'lucide-react';
import './TitleBar.css';
import { Quit, WindowMinimise, WindowToggleMaximise } from '../../../wailsjs/runtime/runtime';
import { APP_ICON } from '../../constants';
import { isWailsRuntime } from '../../services';
import { useAppStore } from '../../stores';
import { Icon } from '../ui';

/** Runs a window command only when the Wails runtime exists (no-op in a plain browser). */
const windowCommand = (command: () => void) => () => {
  if (isWailsRuntime()) command();
};

export const TitleBar: React.FC = () => {
  const productName = useAppStore((s) => s.appInfo?.productName ?? '');

  return (
    <div className="titlebar wails-drag">
      <div className="titlebar-title">
        <Icon icon={APP_ICON} size="md" className="titlebar-logo" />
        <span>{productName}</span>
      </div>
      <div className="titlebar-controls wails-no-drag">
        <button
          className="titlebar-btn"
          onClick={windowCommand(WindowMinimise)}
          title="Minimise"
          aria-label="Minimise"
        >
          <Icon icon={Minus} size="sm" />
        </button>
        <button
          className="titlebar-btn"
          onClick={windowCommand(WindowToggleMaximise)}
          title="Maximise"
          aria-label="Maximise"
        >
          <Icon icon={Square} size="sm" />
        </button>
        <button
          className="titlebar-btn titlebar-btn-close"
          onClick={windowCommand(Quit)}
          title="Close"
          aria-label="Close"
        >
          <Icon icon={X} size="sm" />
        </button>
      </div>
    </div>
  );
};
