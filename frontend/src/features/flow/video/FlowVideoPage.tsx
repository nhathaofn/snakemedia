import React from 'react';
import { StudioLayout } from '../../../components/studio';
import { AllWorkspace } from './all/AllWorkspace';
import { VIDEO_MODES } from './modes';
import { FlowVideoWorkspace } from './workspace/FlowVideoWorkspace';

/** FlowVideoPage - Video studio: mode tabs (Đơn / Hàng loạt) on the left, gallery/queue on the right. */
export const FlowVideoPage: React.FC = () => (
  <StudioLayout
    label="Video"
    modes={VIDEO_MODES}
    renderWorkspace={(mode) => (mode === 'all' ? <AllWorkspace /> : <FlowVideoWorkspace />)}
  />
);
