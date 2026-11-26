import React, { useEffect, useState } from 'react';
import { RotateCcw } from 'lucide-react';
import { Button, Icon } from '../../../components/ui';
import { SettingRow, SettingsSection } from '../../../components/settings';
import { ToolProgressBar, progressText, toolStatus } from '../../../components/tools';
import { useConfigSection } from '../../../hooks';
import { api, unwrap } from '../../../services';
import { useAppStore, useToolsStore } from '../../../stores';
import type { Tool } from '../../../types';
import './ToolsSection.css';

/**
 * Công cụ: the external programs features run (internal/tools). Each is
 * found on the machine first; the app downloads its own copy only when the
 * user asks, here or when a feature needs it.
 */
export const ToolsSection: React.FC = () => {
  const tools = useToolsStore((s) => s.tools);
  const load = useToolsStore((s) => s.load);
  const addToast = useAppStore((s) => s.addToast);
  const { section, patch, saving } = useConfigSection('tools');
  const [pickingProgram, setPickingProgram] = useState<string | null>(null);
  const paths = section?.paths;

  // A path changed here changes where programs are found.
  useEffect(() => {
    void load();
  }, [load, paths]);

  if (tools.length === 0) return null;

  const setPath = (program: string, path: string) => {
    const next = { ...(paths ?? {}) };
    if (path) next[program] = path;
    else delete next[program];
    patch({ paths: next });
  };

  const handlePickPath = async (program: string) => {
    setPickingProgram(program);
    try {
      const picked = unwrap(await api.pickToolPath(program)).trim();
      if (!picked) return;
      setPath(program, picked);
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      setPickingProgram(null);
    }
  };

  return (
    <SettingsSection
      title="Công cụ"
      advanced={tools.flatMap((tool) =>
        tool.programs.map((program) => {
          const currentPath = paths?.[program.name] ?? '';
          const busy = saving || pickingProgram === program.name;
          return (
            <SettingRow key={program.name} label={program.name}>
              <div className="tools-path-control">
                <input
                  type="text"
                  readOnly
                  className="settings-input tools-path-input"
                  aria-label={`Đường dẫn ${program.name}`}
                  value={currentPath}
                  placeholder="Tự tìm"
                  title={currentPath}
                />
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={busy}
                  onClick={() => void handlePickPath(program.name)}
                >
                  Chọn…
                </Button>
                {currentPath && (
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => setPath(program.name, '')}
                    title="Dùng chế độ tự tìm mặc định"
                  >
                    <Icon icon={RotateCcw} size="sm" />
                    Mặc định
                  </Button>
                )}
              </div>
            </SettingRow>
          );
        }),
      )}
    >
      {tools.map((tool) => (
        <ToolRow key={tool.id} tool={tool} />
      ))}
    </SettingsSection>
  );
};

const ToolRow: React.FC<{ tool: Tool }> = ({ tool }) => {
  const progress = useToolsStore((s) => s.progress[tool.id]);
  const install = useToolsStore((s) => s.install);
  const remove = useToolsStore((s) => s.remove);
  const addToast = useAppStore((s) => s.addToast);
  const downloading = progress !== undefined;

  const run = async (action: Promise<string | null>, done: string) => {
    const error = await action;
    addToast(error ? { type: 'error', message: error } : { type: 'success', message: done });
  };

  return (
    <SettingRow label={tool.name} hint={downloading ? progressText(progress) : toolStatus(tool)}>
      <div className="tools-row-control">
        {downloading && <ToolProgressBar progress={progress} />}
        {!tool.ready && tool.downloadable && (
          <Button
            size="sm"
            variant="primary"
            isLoading={downloading}
            onClick={() => void run(install(tool.id), `Đã tải ${tool.name}.`)}
          >
            Tải về
          </Button>
        )}
        {tool.version && !downloading && (
          <Button size="sm" onClick={() => void run(remove(tool.id), `Đã gỡ ${tool.name}.`)}>
            Gỡ
          </Button>
        )}
      </div>
    </SettingRow>
  );
};
