import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { MusePromptPreview } from '../../../types';
import { MessagePreviewContent } from './MessagePreviewModal';

describe('MessagePreviewContent', () => {
  it('renders single prompt preview cleanly without multi-item pagination', () => {
    const items: MusePromptPreview[] = [
      {
        prompt: 'mèo dưới mưa',
        message: 'Task: Create a new video.\nprompt:\nmèo dưới mưa',
        tags: ['lan'],
        images: 1,
        warnings: ['Thiếu ảnh toàn thân'],
      },
    ];
    const html = renderToStaticMarkup(
      <MessagePreviewContent
        items={items}
        index={0}
        tab="main"
        onIndexChange={() => {}}
        onTabChange={() => {}}
      />,
    );
    expect(html).toContain('1 ảnh đính kèm');
    expect(html).toContain('Tư liệu: @lan');
    expect(html).toContain('Thiếu ảnh toàn thân');
    expect(html).toContain('Task: Create a new video.');
    expect(html).not.toContain('Prompt tiếp');
  });

  it('shows item counter and storyboard switch when storyboard message exists', () => {
    const items: MusePromptPreview[] = [
      {
        prompt: 'cảnh 1',
        message: 'video msg 1',
        storyboardMessage: 'sb msg 1',
        tags: [],
        images: 0,
        warnings: [],
      },
      {
        prompt: 'cảnh 2',
        message: 'video msg 2',
        tags: [],
        images: 0,
        warnings: [],
      },
    ];
    const htmlMain = renderToStaticMarkup(
      <MessagePreviewContent
        items={items}
        index={0}
        tab="main"
        onIndexChange={() => {}}
        onTabChange={() => {}}
      />,
    );
    expect(htmlMain).toContain('1 / 2');
    expect(htmlMain).toContain('Video chính');
    expect(htmlMain).toContain('Ảnh Storyboard');
    expect(htmlMain).toContain('video msg 1');

    const htmlStoryboard = renderToStaticMarkup(
      <MessagePreviewContent
        items={items}
        index={0}
        tab="storyboard"
        onIndexChange={() => {}}
        onTabChange={() => {}}
      />,
    );
    expect(htmlStoryboard).toContain('sb msg 1');
  });
});
