import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { LIST_REVEAL_STEP } from '../../constants/ui';
import { QueueTable } from './QueueTable';

describe('QueueTable pagination', () => {
  it('mounts at most one page of rows in a large queue', () => {
    const items = Array.from({ length: LIST_REVEAL_STEP * 2 + 1 }, (_, index) => index);
    const html = renderToStaticMarkup(
      <QueueTable
        label="Hàng đợi"
        selectAll={null}
        headings={['Prompt']}
        items={items}
        keyOf={(index) => String(index)}
        render={(index) => <div data-job={index}>{index}</div>}
        empty="Trống"
      />,
    );

    expect(html.match(/data-job=/g)).toHaveLength(LIST_REVEAL_STEP);
    expect(html).toContain('Phân trang hàng đợi');
    expect(html).not.toContain('data-job="' + LIST_REVEAL_STEP + '"');
  });
});
