import { describe, expect, it } from 'vitest';
import { buildProxyOptions } from './ProxyModal';

describe('buildProxyOptions', () => {
  it('offers none, auto-from-list, and every saved proxy line with active marker', () => {
    const opts = buildProxyOptions('1.2.3.4:8080', ['1.2.3.4:8080', 'socks5://5.6.7.8:1080']);
    expect(opts).toHaveLength(4);
    expect(opts[0]).toMatchObject({ value: 'none', label: 'Không dùng proxy', active: false });
    expect(opts[1]).toMatchObject({ value: '', description: '2 proxy khả dụng', active: false });
    expect(opts[2]).toMatchObject({ value: '1.2.3.4:8080', mono: true, active: true });
    expect(opts[3]).toMatchObject({ value: 'socks5://5.6.7.8:1080', mono: true, active: false });
  });

  it('marks none as active for newly created direct accounts', () => {
    const opts = buildProxyOptions('none', []);
    expect(opts[0].active).toBe(true);
    expect(opts[1].active).toBe(false);
    expect(opts[1].description).toBe('Cần có proxy trong danh sách');
  });
});
