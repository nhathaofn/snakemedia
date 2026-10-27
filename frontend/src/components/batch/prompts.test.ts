import { describe, expect, it } from 'vitest';
import { repeatPrompts, splitPromptList } from './prompts';

describe('splitPromptList', () => {
  it.each([
    ['empty text', '', []],
    ['only whitespace', ' \n\t\n ', []],
    ['one prompt per line', 'a\nb\n\tc  ', ['a', 'b', 'c']],
    ['CRLF lines', 'a\r\nb\r\n', ['a', 'b']],
    ['a leading BOM', `${String.fromCharCode(0xfeff)}a\nb`, ['a', 'b']],
    ['blank-line blocks keep multi-line prompts', 'a1\na2\n\nb', ['a1\na2', 'b']],
    ['blank lines with spaces and repeats', 'a\n  \n\n\t\nb\nb2', ['a', 'b\nb2']],
    ['CRLF blocks', 'a1\r\na2\r\n\r\nb', ['a1\na2', 'b']],
    ['one block without blank lines stays per line', 'x\ny', ['x', 'y']],
  ])('%s', (_, text, expected) => {
    expect(splitPromptList(text)).toEqual(expected);
  });
});

describe('repeatPrompts', () => {
  it('repeats each prompt in place', () => {
    expect(repeatPrompts(['a', 'b'], 2)).toEqual(['a', 'a', 'b', 'b']);
  });

  it('treats counts below one as one', () => {
    expect(repeatPrompts(['a'], 0)).toEqual(['a']);
    expect(repeatPrompts(['a'], 1.7)).toEqual(['a']);
  });
});
