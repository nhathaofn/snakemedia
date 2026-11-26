import { useState } from 'react';
import { Button, Modal } from '../ui';
import { useAppStore, useToolsStore } from '../../stores';
import { ToolProgressBar } from './ToolProgressBar';
import { progressText } from './toolText';
import './ToolPrompt.css';

/**
 * Asks to download a tool when a feature the user runs needs one the machine
 * does not have (internal/tools). Nothing is asked before a feature needs it;
 * "Để sau" quiets that tool for this run.
 */
export function ToolPrompt() {
  const request = useToolsStore((s) => s.requests[0]);
  const progress = useToolsStore((s) => (request ? s.progress[request.tool.id] : undefined));
  const install = useToolsStore((s) => s.install);
  const postpone = useToolsStore((s) => s.postpone);
  const addToast = useAppStore((s) => s.addToast);
  const [failed, setFailed] = useState<{ toolId: string; message: string } | null>(null);

  if (!request) return null;
  const { tool, feature } = request;
  const error = failed?.toolId === tool.id ? failed.message : null;
  const downloading = progress !== undefined;

  const download = async () => {
    setFailed(null);
    const err = await install(tool.id);
    if (err) setFailed({ toolId: tool.id, message: err });
    else addToast({ type: 'success', message: `Đã tải ${tool.name}.` });
  };

  return (
    <Modal
      title={`Cần ${tool.name}`}
      onClose={() => !downloading && postpone(tool.id)}
      footer={
        <div className="tool-prompt-actions">
          <Button disabled={downloading} onClick={() => postpone(tool.id)}>
            Để sau
          </Button>
          <Button variant="primary" isLoading={downloading} onClick={() => void download()}>
            {error ? 'Thử lại' : 'Tải về'}
          </Button>
        </div>
      }
    >
      <div className="tool-prompt-body">
        <p>
          <strong>{feature}</strong> cần {tool.name}, máy chưa có.
        </p>
        {downloading && (
          <div className="tool-prompt-progress">
            <ToolProgressBar progress={progress} />
            <span>{progressText(progress)}</span>
          </div>
        )}
        {error && <p className="tool-prompt-error">{error}</p>}
      </div>
    </Modal>
  );
}
