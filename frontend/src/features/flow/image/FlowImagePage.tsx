import React from 'react';
import { FlowStudioLayout } from '../shared';
import { IMAGE_MODES } from './modes';
import { FlowImageWorkspace } from './workspace/FlowImageWorkspace';
import { AllWorkspace } from './all/AllWorkspace';

/** FlowImagePage - Image studio: mode tabs (Đơn / Hàng loạt) on the left, gallery on the right. */
export const FlowImagePage: React.FC = () => (
  <FlowStudioLayout
    label="Image"
    modes={IMAGE_MODES}
    renderWorkspace={(mode) => (mode === 'all' ? <AllWorkspace /> : <FlowImageWorkspace />)}
  />
);
