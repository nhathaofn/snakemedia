import { describe, expect, it } from 'vitest';
import { ToolOrigin } from '../../types';
import type { Tool } from '../../types';
import { progressText, toolStatus } from './toolText';

const tool = (ready: boolean, origin: ToolOrigin, version = '') =>
  ({
    id: 'ffmpeg',
    name: 'FFmpeg',
    homepage: '',
    ready,
    downloadable: true,
    version,
    installing: false,
    programs: [{ name: 'ffmpeg', path: '', origin }],
  }) as Tool;

describe('toolStatus', () => {
  it('says where a ready tool was found', () => {
    expect(toolStatus(tool(false, ToolOrigin.None))).toBe('Chưa có');
    expect(toolStatus(tool(true, ToolOrigin.System))).toBe('Có sẵn trên máy');
    expect(toolStatus(tool(true, ToolOrigin.Config))).toBe('Đường dẫn tự chọn');
    expect(toolStatus(tool(true, ToolOrigin.App, '9.0.2'))).toBe('Đã tải · 9.0.2');
  });
});

describe('progressText', () => {
  it('waits for the size before showing a total', () => {
    expect(progressText({ tool: 'ffmpeg', done: 0, total: 0 })).toBe('Đang chuẩn bị…');
    expect(progressText({ tool: 'ffmpeg', done: 1024, total: 2048 })).toContain('/');
  });
});
