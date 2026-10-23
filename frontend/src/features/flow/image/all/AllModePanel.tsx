import React from 'react';
import { Layers } from 'lucide-react';

/**
 * AllModePanel - Tools applied to every image at once.
 * Ready for domain-specific batch feature implementation.
 */
export const AllModePanel: React.FC = () => {
  return (
    <div
      className="flow-image-panel-inner"
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        height: '100%',
        gap: '12px',
        color: 'var(--text-muted)',
        textAlign: 'center',
        padding: '24px',
      }}
    >
      <Layers size={28} />
      <p style={{ margin: 0, fontSize: '0.875rem' }}>
        Chế độ tạo All (hàng loạt) sẵn sàng triển khai.
      </p>
    </div>
  );
};
