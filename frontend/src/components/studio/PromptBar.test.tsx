import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { PromptBar } from './PromptBar';

describe('PromptBar submission', () => {
  const props = {
    prompt: 'a video',
    placeholder: '',
    onPromptChange: () => {},
    onGenerate: () => {},
    generating: true,
  };

  it('locks only while the queue request is being accepted', () => {
    const html = renderToStaticMarkup(<PromptBar {...props} submitting />);
    expect(html).toMatch(/<button[^>]*disabled=""/);
    expect(html).toMatch(/<textarea[^>]*readOnly=""/);
  });

  it('allows another prompt while generation runs in the background', () => {
    const html = renderToStaticMarkup(<PromptBar {...props} />);
    expect(html).not.toMatch(/<button[^>]*disabled/);
    expect(html).not.toMatch(/<textarea[^>]*readOnly/);
  });
});
