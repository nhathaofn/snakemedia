import { ToolOrigin } from '../../types';
import type { Tool, ToolProgress } from '../../types';
import { formatBytes } from '../../utils';

/** "Đang tải 12 MB / 110 MB" while a tool downloads. */
export function progressText(p: ToolProgress): string {
  if (p.total <= 0) return p.done > 0 ? `Đang tải ${formatBytes(p.done, 1)}` : 'Đang chuẩn bị…';
  return `Đang tải ${formatBytes(p.done, 1)} / ${formatBytes(p.total, 1)}`;
}

/** Where a tool was found, in a few words. */
export function toolStatus(tool: Tool): string {
  if (!tool.ready) return 'Chưa có';
  const origins = new Set(tool.programs.map((p) => p.origin));
  if (origins.has(ToolOrigin.Config)) return 'Đường dẫn tự chọn';
  if (origins.has(ToolOrigin.System)) return 'Có sẵn trên máy';
  return tool.version ? `Đã tải · ${tool.version}` : 'Đã tải';
}
