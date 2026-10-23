import React, { useState } from 'react';
import './FlowImagePage.css';
import { Tabs } from '../../../components/ui';
import { DEFAULT_IMAGE_MODE, IMAGE_MODES } from './modes';
import type { ImageMode } from './modes';

/**
 * FlowImagePage - Two-column workspace layout.
 * Left: fixed-width tools panel with mode tabs (Single / All); right: main content.
 * Registered with `layout: 'full'`, so it fills the content area and handles its own
 * inset and scrolling.
 */
export const FlowImagePage: React.FC = () => {
  const [mode, setMode] = useState<ImageMode>(DEFAULT_IMAGE_MODE);

  return (
    <div className="flow-image-page">
      <aside className="flow-image-sidebar" aria-label="Image tools">
        <Tabs aria-label="Image mode" items={IMAGE_MODES} value={mode} onChange={setMode} />
      </aside>
      <section className="flow-image-content" aria-label="Image workspace">
        <div className="flow-image-panel-inner">{/* Main content area */}</div>
      </section>
    </div>
  );
};
