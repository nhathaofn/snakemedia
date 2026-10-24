import React from 'react';
import { FlowStudioLayout } from '../shared';
import { IMAGE_MODES } from './modes';
import { FlowImageWorkspace } from './workspace/FlowImageWorkspace';

/** FlowImagePage - Image studio: mode tabs (Single / All) on the left, gallery on the right. */
export const FlowImagePage: React.FC = () => (
  <FlowStudioLayout label="Image" modes={IMAGE_MODES} workspace={<FlowImageWorkspace />} />
);
