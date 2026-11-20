import { splitPromptList } from '../../../components/batch/prompts';
import { MuseAssetBatchConflict, MuseRefRole } from '../../../types';
import type { MuseAsset, MuseFolderImage } from '../../../types';

const BOM = String.fromCharCode(0xfeff);
const MAX_TAG_LENGTH = 40;
const VALID_TAG_RE = /^[\p{L}\p{N}_-]{1,40}$/u;
const DISALLOWED_TAG_RE = /[^\p{L}\p{N}_-]+/gu;

export type LockSplitMode = 'line' | 'positional' | 'block';

export interface ParsedAlias {
  tag: string;
  name: string;
}

export interface ParsedRole {
  raw: string;
  role?: MuseRefRole;
  error?: string;
}

export interface BatchRowOverride {
  excluded?: boolean;
  tag?: string;
  name?: string;
  role?: MuseRefRole;
  lock?: string;
}

export type BatchRowStatus = 'ready' | 'rename' | 'overwrite' | 'skip' | 'excluded' | 'error';

export interface BatchPreviewRow {
  index: number;
  image: MuseFolderImage;
  tag: string;
  resolvedTag: string;
  name: string;
  role: MuseRefRole;
  lock: string;
  excluded: boolean;
  status: BatchRowStatus;
  note?: string;
}

const ROLE_ALIASES: Record<string, MuseRefRole> = {
  character: MuseRefRole.Character,
  'nhân vật': MuseRefRole.Character,
  'nhan vat': MuseRefRole.Character,
  nv: MuseRefRole.Character,
  'toàn thân': MuseRefRole.Character,
  'toan than': MuseRefRole.Character,
  full: MuseRefRole.Character,

  face: MuseRefRole.Face,
  'khuôn mặt': MuseRefRole.Face,
  'khuon mat': MuseRefRole.Face,
  mặt: MuseRefRole.Face,
  mat: MuseRefRole.Face,
  'chân dung': MuseRefRole.Face,
  'chan dung': MuseRefRole.Face,

  product: MuseRefRole.Product,
  'sản phẩm': MuseRefRole.Product,
  'san pham': MuseRefRole.Product,
  sp: MuseRefRole.Product,

  prop: MuseRefRole.Prop,
  'đạo cụ': MuseRefRole.Prop,
  'dao cu': MuseRefRole.Prop,
  'đồ vật': MuseRefRole.Prop,
  'do vat': MuseRefRole.Prop,

  storyboard: MuseRefRole.Storyboard,
  sb: MuseRefRole.Storyboard,
  'khung cảnh': MuseRefRole.Storyboard,
  'khung canh': MuseRefRole.Storyboard,
  'bố cục': MuseRefRole.Storyboard,
  'bo cuc': MuseRefRole.Storyboard,

  setting: MuseRefRole.Setting,
  'bối cảnh': MuseRefRole.Setting,
  'boi canh': MuseRefRole.Setting,
  nền: MuseRefRole.Setting,
  nen: MuseRefRole.Setting,
  background: MuseRefRole.Setting,
  bg: MuseRefRole.Setting,

  style: MuseRefRole.Style,
  'phong cách': MuseRefRole.Style,
  'phong cach': MuseRefRole.Style,

  none: MuseRefRole.None,
  'tham chiếu': MuseRefRole.None,
  'tham chieu': MuseRefRole.None,
  'tham chiếu chung': MuseRefRole.None,
  'tham chieu chung': MuseRefRole.None,
  chung: MuseRefRole.None,
  ref: MuseRefRole.None,
};

function stripBomAndNormalizeEol(text: string): string {
  return (text.startsWith(BOM) ? text.slice(1) : text).replace(/\r\n?/g, '\n');
}

export function fileStem(fileName: string): string {
  const dot = fileName.lastIndexOf('.');
  return dot > 0 ? fileName.slice(0, dot) : fileName;
}

export function isValidTag(tag: string): boolean {
  return VALID_TAG_RE.test(tag);
}

/** Converts arbitrary alias text or filename stem into a valid @tag. */
export function sanitizeTag(input: string, fallback = ''): string {
  const tryClean = (raw: string): string => {
    const cleaned = raw
      .trim()
      .replace(/^@+/, '')
      .replace(DISALLOWED_TAG_RE, '-')
      .replace(/^-+|-+$/g, '');
    return Array.from(cleaned).slice(0, MAX_TAG_LENGTH).join('');
  };
  return tryClean(input) || tryClean(fallback);
}

/**
 * Parses alias/tag lines ordered top-to-bottom:
 * - `@tag` or `tag` -> `{ tag, name: '' }`
 * - `tag | Tên` or `tag\tTên` -> `{ tag, name }`
 * - `Tên có dấu cách` -> `{ tag: sanitized, name: 'Tên có dấu cách' }`
 */
export function parseAliasLines(text: string): ParsedAlias[] {
  return splitPromptList(text, 'line').map((line) => {
    const sep = line.includes('|') ? '|' : line.includes('\t') ? '\t' : '';
    if (sep) {
      const idx = line.indexOf(sep);
      const rawTag = line.slice(0, idx).trim();
      const rawName = line.slice(idx + 1).trim();
      return {
        tag: sanitizeTag(rawTag, rawName),
        name: rawName,
      };
    }
    const stripped = line.replace(/^@+/, '').trim();
    if (isValidTag(stripped)) {
      return { tag: stripped, name: '' };
    }
    return {
      tag: sanitizeTag(stripped),
      name: line.trim(),
    };
  });
}

/** Parses one role token in Vietnamese or English; `-` / `mặc định` uses defaultRole. */
export function parseRoleToken(raw: string): MuseRefRole | null | 'default' {
  const key = raw.trim().toLowerCase().replace(/\s+/g, ' ');
  if (!key || key === '-' || key === 'default' || key === 'mặc định' || key === 'mac dinh') {
    return 'default';
  }
  return ROLE_ALIASES[key] ?? null;
}

export function parseRoleLines(text: string): ParsedRole[] {
  return splitPromptList(text, 'line').map((line) => {
    const parsed = parseRoleToken(line);
    if (parsed === 'default') {
      return { raw: line };
    }
    if (parsed === null) {
      return { raw: line, error: `Không nhận diện được vai trò "${line}"` };
    }
    return { raw: line, role: parsed };
  });
}

/**
 * Splits lock prompts top-to-bottom:
 * - `line`: every non-empty line is one prompt
 * - `positional`: keeps internal blank lines aligned with image index (drops trailing empty lines)
 * - `block`: multi-line prompts separated by blank lines
 */
export function parseLockList(text: string, mode: LockSplitMode): string[] {
  if (mode === 'line' || mode === 'block') {
    return splitPromptList(text, mode);
  }
  const lines = stripBomAndNormalizeEol(text).split('\n');
  while (lines.length > 0 && lines[lines.length - 1].trim() === '') {
    lines.pop();
  }
  return lines.map((line) => line.trim());
}

function uniqueRenamedTag(tag: string, taken: Set<string>): string {
  const runes = Array.from(tag);
  for (let n = 2; ; n++) {
    const suffix = `_${n}`;
    const keep = Math.max(0, MAX_TAG_LENGTH - suffix.length);
    const candidate = `${runes.slice(0, keep).join('')}${suffix}`;
    if (!taken.has(candidate.toLowerCase())) {
      return candidate;
    }
  }
}

export function buildBatchRows({
  images,
  aliases,
  roles,
  locks,
  defaultRole,
  conflict,
  existingAssets,
  overrides,
}: {
  images: readonly MuseFolderImage[];
  aliases: readonly ParsedAlias[];
  roles: readonly ParsedRole[];
  locks: readonly string[];
  defaultRole: MuseRefRole;
  conflict: MuseAssetBatchConflict;
  existingAssets: readonly MuseAsset[];
  overrides: ReadonlyMap<number, BatchRowOverride>;
}): { rows: BatchPreviewRow[]; warnings: string[] } {
  const existingTags = new Set(existingAssets.map((a) => a.tag.toLowerCase()));
  const taken = new Set(existingTags);
  const batchSeen = new Set<string>();

  const rows: BatchPreviewRow[] = images.map((image, index) => {
    const ov = overrides.get(index);
    const alias = aliases[index];
    const parsedRole = roles[index];
    const fallbackTag = sanitizeTag(fileStem(image.fileName), `tu-lieu-${index + 1}`);

    const rawTag = ov?.tag !== undefined ? ov.tag : alias ? alias.tag : fallbackTag;
    const tag = rawTag.replace(/^@+/, '').trim();
    const name = ov?.name !== undefined ? ov.name : (alias?.name ?? '');
    const role = ov?.role !== undefined ? ov.role : (parsedRole?.role ?? defaultRole);
    const lock = ov?.lock !== undefined ? ov.lock : (locks[index] ?? '');
    const excluded = ov?.excluded ?? Boolean(image.error);

    if (excluded) {
      return {
        index,
        image,
        tag,
        resolvedTag: tag,
        name,
        role,
        lock,
        excluded: true,
        status: image.error ? 'error' : 'excluded',
        note: image.error || 'Đã bỏ chọn',
      };
    }

    if (image.error) {
      return {
        index,
        image,
        tag,
        resolvedTag: tag,
        name,
        role,
        lock,
        excluded: false,
        status: 'error',
        note: image.error,
      };
    }

    if (ov?.role === undefined && parsedRole?.error) {
      return {
        index,
        image,
        tag,
        resolvedTag: tag,
        name,
        role,
        lock,
        excluded: false,
        status: 'error',
        note: parsedRole.error,
      };
    }

    if (!isValidTag(tag)) {
      return {
        index,
        image,
        tag,
        resolvedTag: tag,
        name,
        role,
        lock,
        excluded: false,
        status: 'error',
        note: 'Tag chỉ gồm chữ, số, - và _ (tối đa 40 ký tự)',
      };
    }

    const lower = tag.toLowerCase();
    const inExisting = existingTags.has(lower);
    const inBatch = batchSeen.has(lower);

    if (!inExisting && !inBatch) {
      taken.add(lower);
      batchSeen.add(lower);
      return {
        index,
        image,
        tag,
        resolvedTag: tag,
        name,
        role,
        lock,
        excluded: false,
        status: 'ready',
      };
    }

    if (conflict === MuseAssetBatchConflict.Skip) {
      return {
        index,
        image,
        tag,
        resolvedTag: tag,
        name,
        role,
        lock,
        excluded: false,
        status: 'skip',
        note: inExisting ? 'Trùng tag đã có (bỏ qua)' : 'Trùng tag trong danh sách (bỏ qua)',
      };
    }

    if (conflict === MuseAssetBatchConflict.Overwrite) {
      taken.add(lower);
      batchSeen.add(lower);
      return {
        index,
        image,
        tag,
        resolvedTag: tag,
        name,
        role,
        lock,
        excluded: false,
        status: 'overwrite',
        note: inExisting ? 'Ghi đè tư liệu cũ' : 'Ghi đè dòng trùng trước đó',
      };
    }

    const resolvedTag = uniqueRenamedTag(tag, taken);
    taken.add(resolvedTag.toLowerCase());
    batchSeen.add(resolvedTag.toLowerCase());
    return {
      index,
      image,
      tag,
      resolvedTag,
      name,
      role,
      lock,
      excluded: false,
      status: 'rename',
      note: `Đổi thành @${resolvedTag}`,
    };
  });

  const warnings: string[] = [];
  if (images.length > 0) {
    if (aliases.length > 0 && aliases.length !== images.length) {
      warnings.push(`Alias có ${aliases.length} dòng nhưng thư mục có ${images.length} ảnh.`);
    }
    if (roles.length > 0 && roles.length !== images.length) {
      warnings.push(`Vai trò có ${roles.length} dòng nhưng thư mục có ${images.length} ảnh.`);
    }
    if (locks.length > 0 && locks.length !== images.length) {
      warnings.push(`Prompt khoá có ${locks.length} mục nhưng thư mục có ${images.length} ảnh.`);
    }
  }

  return { rows, warnings };
}
