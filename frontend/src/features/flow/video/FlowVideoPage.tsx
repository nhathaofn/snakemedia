import React from 'react';
import { FlowStudioLayout, ModePlaceholder } from '../shared';
import { VIDEO_MODES } from './modes';
import { FlowVideoWorkspace } from './workspace/FlowVideoWorkspace';

/** FlowVideoPage - Video studio: mode tabs (Single / All) on the left, gallery on the right. */
export const FlowVideoPage: React.FC = () => (
  <FlowStudioLayout
    label="Video"
    modes={VIDEO_MODES}
    // All has no queue yet, so it must not offer the Single prompt box either.
    renderWorkspace={(mode) =>
      mode === 'all' ? (
        <ModePlaceholder text="Chế độ All (hàng loạt) cho video chưa được hỗ trợ. Dùng tab Single để tạo video." />
      ) : (
        <FlowVideoWorkspace />
      )
    }
  />
);
