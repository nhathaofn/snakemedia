import React, { useEffect } from 'react';
import { Button } from '../../../components/ui';
import { SettingRow, SettingsSection, TextSetting } from '../../../components/settings';
import { ToolProgressBar, progressText, toolStatus } from '../../../components/tools';
import { useConfigSection } from '../../../hooks';
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
  const { section, patch, saving } = useConfigSection('tools');
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

  return (
    <SettingsSection
      title="Công cụ"
      advanced={tools.flatMap((tool) =>
        tool.programs.map((program) => (
          <SettingRow key={program.name} label={program.name}>
            <TextSetting
              aria-label={`Đường dẫn ${program.name}`}
              value={paths?.[program.name] ?? ''}
              placeholder="Tự tìm"
              disabled={saving}
              onCommit={(path) => setPath(program.name, path)}
            />
          </SettingRow>
        )),
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
