import { describe, expect, it } from 'vitest';
import {
  APP_SECTIONS,
  DEFAULT_PATH,
  HEADER_TABS,
  SECTION_NAVIGATION,
  SIDEBAR_PINNED,
  defaultFeaturePath,
  featurePath,
  findSection,
} from './registry';

const SEGMENT = /^[a-z0-9-]+$/;

describe('section registry', () => {
  it('has unique section ids and paths', () => {
    const ids = APP_SECTIONS.map((s) => s.id);
    const paths = APP_SECTIONS.map((s) => s.path);
    expect(new Set(ids).size).toBe(ids.length);
    expect(new Set(paths).size).toBe(paths.length);
  });

  it('uses absolute single-segment section paths', () => {
    for (const s of APP_SECTIONS) {
      expect(s.path.startsWith('/')).toBe(true);
      expect(s.path.slice(1)).toMatch(SEGMENT);
    }
  });

  it('gives every section at least one visible feature', () => {
    for (const s of APP_SECTIONS) {
      expect(SECTION_NAVIGATION.get(s.id)?.length).toBeGreaterThan(0);
    }
  });

  it('uses unique relative single-segment feature paths within a section', () => {
    for (const s of APP_SECTIONS) {
      const ids = s.features.map((f) => f.id);
      const paths = s.features.map((f) => f.path);
      expect(new Set(ids).size).toBe(ids.length);
      expect(new Set(paths).size).toBe(paths.length);
      expect(paths.every((p) => SEGMENT.test(p))).toBe(true);
    }
  });

  it('places every section exactly once, header or sidebar', () => {
    expect(HEADER_TABS.length).toBeGreaterThan(0);
    expect([...HEADER_TABS, ...SIDEBAR_PINNED].map((s) => s.id).sort()).toEqual(
      APP_SECTIONS.map((s) => s.id).sort(),
    );
  });

  it('opens on the first section, which is a header tab', () => {
    expect(HEADER_TABS[0]).toBe(APP_SECTIONS[0]);
    expect(DEFAULT_PATH).toBe(defaultFeaturePath(APP_SECTIONS[0]));
    expect(findSection(DEFAULT_PATH)).toBe(APP_SECTIONS[0]);
  });

  it('derives sidebar navigation from visible features', () => {
    for (const s of APP_SECTIONS) {
      const visible = s.features.filter((f) => f.showInNav !== false);
      expect(SECTION_NAVIGATION.get(s.id)?.map((n) => n.path)).toEqual(
        visible.map((f) => featurePath(s, f)),
      );
    }
  });
});

describe('findSection', () => {
  const [first] = APP_SECTIONS;

  it('matches a section root and its features', () => {
    expect(findSection(first.path)).toBe(first);
    expect(findSection(featurePath(first, first.features[0]))).toBe(first);
  });

  it('matches whole segments only', () => {
    expect(findSection(`${first.path}extra`)).toBeUndefined();
    expect(findSection('/')).toBeUndefined();
  });
});
