import { describe, expect, it } from 'vitest';
import { isPromptMode, newPrompts, splitPromptList } from './prompts';

describe('splitPromptList', () => {
  it.each([
    ['empty text', 'line', '', []],
    ['only whitespace', 'line', ' \n\t\n ', []],
    ['one prompt per line', 'line', 'a\nb\n\tc  ', ['a', 'b', 'c']],
    ['line mode ignores blank lines', 'line', 'a1\na2\n\nb', ['a1', 'a2', 'b']],
    ['CRLF lines', 'line', 'a\r\nb\r\n', ['a', 'b']],
    ['a leading BOM', 'line', `${String.fromCharCode(0xfeff)}a\nb`, ['a', 'b']],
    ['block keeps multi-line prompts', 'block', 'a1\na2\n\nb', ['a1\na2', 'b']],
    ['block with spaced blank lines', 'block', 'a\n  \n\n\t\nb\nb2', ['a', 'b\nb2']],
    ['block CRLF', 'block', 'a1\r\na2\r\n\r\nb', ['a1\na2', 'b']],
    ['block without blank lines is one prompt', 'block', 'x\ny', ['x\ny']],
    ['single gathers everything', 'single', ' a\n\nb\nc \n', ['a\n\nb\nc']],
    ['single of blank text', 'single', ' \n ', []],
  ] as const)('%s', (_, mode, text, expected) => {
    expect(splitPromptList(text, mode)).toEqual(expected);
  });

  it('defaults to one prompt per line', () => {
    expect(splitPromptList('a\nb')).toEqual(['a', 'b']);
  });
});

describe('isPromptMode', () => {
  it('accepts only known modes', () => {
    expect(isPromptMode('block')).toBe(true);
    expect(isPromptMode('auto')).toBe(false);
    expect(isPromptMode(null)).toBe(false);
  });
});

describe('newPrompts', () => {
  it('leaves out prompts already waiting and repeats', () => {
    expect(newPrompts(['a cat', 'a  cat ', 'a dog', '', 'a bird'], ['a bird'])).toEqual([
      'a cat',
      'a dog',
    ]);
  });
});
