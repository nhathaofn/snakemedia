import { describe, expect, it } from 'vitest';
import { thumbUrl } from './media';

describe('thumbUrl', () => {
  it('asks the backend for a thumbnail of stored media only', () => {
    expect(thumbUrl('/flow-media/a.png')).toBe('/flow-media/a.png?thumb');
    expect(thumbUrl('/flow-media/a.png?v=1')).toBe('/flow-media/a.png?v=1&thumb');
    expect(thumbUrl('blob:abc')).toBe('blob:abc');
    expect(thumbUrl('data:image/png;base64,xx')).toBe('data:image/png;base64,xx');
  });
});
