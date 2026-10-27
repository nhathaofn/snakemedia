import { describe, expect, it } from 'vitest';

// Every page and component takes its colours from the theme tokens, or it
// looks wrong in one of the themes. tokens.css is the only file that may
// hold raw colours.
//
// Vitest replaces .css imports with empty modules, so the files are read from
// disk. The project has no Node typings; this is the only Node API used.
interface NodeFs {
  readFileSync(path: URL, encoding: 'utf8'): string;
  readdirSync(path: URL, options: { recursive: true }): string[];
}
const fs = (await import(/* @vite-ignore */ 'node:fs' as string)) as NodeFs;

const read = (dir: URL, pattern: RegExp): [string, string][] =>
  fs
    .readdirSync(dir, { recursive: true })
    .map((name) => name.replaceAll('\\', '/'))
    .filter((name) => pattern.test(name))
    .map((name) => [name, fs.readFileSync(new URL(name, dir), 'utf8')]);

const under = (dir: string, pattern: RegExp): [string, string][] =>
  read(new URL(dir, import.meta.url), pattern).map(([name, text]) => [`${dir}${name}`, text]);

const sources = [
  ...under('../features/', /\.(css|tsx)$/),
  ...under('../components/', /\.(css|tsx)$/),
  ...under('../routes/', /\.(css|tsx)$/),
];
const tokenFiles = read(new URL('./', import.meta.url), /\.css$/);
const tokens = tokenFiles.map(([, text]) => text).join('\n');
// Tokens, plus the few custom properties a component defines for itself.
const definitions = [tokens, ...sources.map(([, text]) => text)].join('\n');
const defined = new Set([...definitions.matchAll(/^\s*(--[a-z0-9-]+)\s*:/gm)].map((m) => m[1]));

describe('theme tokens', () => {
  it('reads the sources and tokens', () => {
    expect(sources.length).toBeGreaterThan(40);
    expect(defined.has('--bg-primary')).toBe(true);
  });

  it('uses only defined design tokens', () => {
    const missing = sources.flatMap(([file, text]) =>
      [...text.matchAll(/var\((--[a-z0-9-]+)/g)]
        .map((m) => m[1])
        .filter((name) => !defined.has(name))
        .map((name) => `${file}: ${name}`),
    );
    expect(missing).toEqual([]);
  });

  it('has no hard-coded colours or token fallbacks', () => {
    const offenders = sources.flatMap(([file, text]) =>
      text
        .split('\n')
        .map((line, i) => ({ line, i }))
        .filter(({ line }) => /#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(|var\(--[a-z0-9-]+,/.test(line))
        .map(({ line, i }) => `${file}:${i + 1}: ${line.trim()}`),
    );
    expect(offenders).toEqual([]);
  });

  it('switches native controls with the theme', () => {
    expect(tokens).toMatch(/:root\s*{[^}]*color-scheme:\s*dark/);
    expect(tokens).toMatch(/\[data-theme='light'\]\s*{[^}]*color-scheme:\s*light/);
  });
});
