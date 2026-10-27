import React from 'react';
import { FlowStudioLayout } from '../shared';
import { VIDEO_MODES } from './modes';
import { FlowVideoWorkspace } from './workspace/FlowVideoWorkspace';

/** FlowVideoPage - Video studio: the tools panel on the left, the gallery on the right. */
export const FlowVideoPage: React.FC = () => (
  <FlowStudioLayout label="Video" modes={VIDEO_MODES} workspace={<FlowVideoWorkspace />} />
);
