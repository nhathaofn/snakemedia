import { splitPromptList } from '../../../components/batch/prompts';
import { MuseAssetBatchConflict, MuseRefRole } from '../../../types';
import type { MuseAsset, MuseAssetType, MuseFolderImage } from '../../../types';
import {
  buildCanonicalPreviewName,
  cleanToken,
  defaultTypeForRole,
  normalizeProjectName,
} from './assetRoles';

const BOM = String.fromCharCode(0xfeff);
const MAX_TAG_LENGTH = 40;
const VALID_TAG_RE = /^[\p{L}\p{N}_+-]{1,40}$/u;
const DISALLOWED_TAG_RE = /[^\p{L}\p{N}_+-]+/gu;

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
  project?: string;
  type?: MuseAssetType;
  tag?: string;
  name?: string;
  role?: MuseRefRole;
  lock?: string;
  angle?: string;
  variant?: string;
  state?: string;
  rejected?: boolean;
}

export type BatchRowStatus = 'ready' | 'rename' | 'overwrite' | 'skip' | 'excluded' | 'error';

export interface BatchPreviewRow {
  index: number;
  image: MuseFolderImage;
  project: string;
  /** Raw controlled-input draft so typing underscores/spaces mid-word is not stripped live. */
  projectInput: string;
  type: MuseAssetType;
  tag: string;
  resolvedTag: string;
  name: string;
  role: MuseRefRole;
  lock: string;
  angle: string;
  angleInput: string;
  variant: string;
  variantInput: string;
  state: string;
  stateInput: string;
  version: number;
  primary: boolean;
  rejected: boolean;
  canonicalName: string;
  hint?: string;
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

  animal: MuseRefRole.Animal,
  pet: MuseRefRole.Animal,
  thú: MuseRefRole.Animal,
  thu: MuseRefRole.Animal,
  'thú cưng': MuseRefRole.Animal,
  'thu cung': MuseRefRole.Animal,
  'động vật': MuseRefRole.Animal,
  'dong vat': MuseRefRole.Animal,

  product: MuseRefRole.Product,
  'sản phẩm': MuseRefRole.Product,
  'san pham': MuseRefRole.Product,
  sp: MuseRefRole.Product,

  prop: MuseRefRole.Prop,
  'đạo cụ': MuseRefRole.Prop,
  'dao cu': MuseRefRole.Prop,
  'đồ vật': MuseRefRole.Prop,
  'do vat': MuseRefRole.Prop,

  vehicle: MuseRefRole.Vehicle,
  veh: MuseRefRole.Vehicle,
  xe: MuseRefRole.Vehicle,
  'xe cộ': MuseRefRole.Vehicle,
  'xe co': MuseRefRole.Vehicle,
  'phương tiện': MuseRefRole.Vehicle,
  'phuong tien': MuseRefRole.Vehicle,

  document: MuseRefRole.Document,
  doc: MuseRefRole.Document,
  'tài liệu': MuseRefRole.Document,
  'tai lieu': MuseRefRole.Document,
  'bảng biểu': MuseRefRole.Document,
  'bang bieu': MuseRefRole.Document,

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

export function fileExt(fileName: string): string {
  const clean = fileName.split('?')[0];
  const dot = clean.lastIndexOf('.');
  return dot > 0 ? clean.slice(dot).toLowerCase() : '.png';
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

function uniqueRenamedTag(tag: string, scopePrefix: string, taken: Set<string>): string {
  const runes = Array.from(tag);
  for (let n = 2; ; n++) {
    const suffix = `_${n}`;
    const keep = Math.max(0, MAX_TAG_LENGTH - suffix.length);
    const candidate = `${runes.slice(0, keep).join('')}${suffix}`;
    if (!taken.has(`${scopePrefix}|${candidate.toLowerCase()}`)) {
      return candidate;
    }
  }
}

function assetScopeKey(project: string, tag: string): string {
  return `${normalizeProjectName(project)}|${tag.toLowerCase().trim()}`;
}

export function buildBatchRows({
  images,
  aliases,
  roles,
  locks,
  defaultProject = '',
  defaultRole,
  conflict,
  mergeByAlias = false,
  existingAssets,
  overrides,
}: {
  images: readonly MuseFolderImage[];
  aliases: readonly ParsedAlias[];
  roles: readonly ParsedRole[];
  locks: readonly string[];
  defaultProject?: string;
  defaultRole: MuseRefRole;
  conflict: MuseAssetBatchConflict;
  mergeByAlias?: boolean;
  existingAssets: readonly MuseAsset[];
  overrides: ReadonlyMap<number, BatchRowOverride>;
}): { rows: BatchPreviewRow[]; warnings: string[] } {
  const existingScopes = new Set(existingAssets.map((a) => assetScopeKey(a.project ?? '', a.tag)));
  const taken = new Set(existingScopes);
  const batchSeen = new Set<string>();
  const normDefaultProject = normalizeProjectName(defaultProject);

  const rows: BatchPreviewRow[] = images.map((image, index) => {
    const ov = overrides.get(index);
    const alias = aliases[index];
    const parsedRole = roles[index];
    const parsedName = image.parsed;
    const fallbackTag = sanitizeTag(fileStem(image.fileName), `tu-lieu-${index + 1}`);

    const rawProject =
      ov?.project !== undefined ? ov.project : (parsedName?.project ?? normDefaultProject);
    const project = normalizeProjectName(rawProject);
    const projectInput = ov?.project !== undefined ? ov.project : project;
    const rawTag =
      ov?.tag !== undefined
        ? ov.tag
        : alias
          ? alias.tag
          : parsedName?.alias
            ? parsedName.alias
            : fallbackTag;
    const tag = rawTag.replace(/^@+/, '').trim();
    const name = ov?.name !== undefined ? ov.name : (alias?.name ?? '');
    const role =
      ov?.role !== undefined
        ? ov.role
        : (parsedRole?.role ?? (parsedName ? parsedName.role : defaultRole));
    const type =
      ov?.type !== undefined
        ? ov.type
        : parsedName?.type && ov?.role === undefined
          ? parsedName.type
          : defaultTypeForRole(role);
    const lock = ov?.lock !== undefined ? ov.lock : (locks[index] ?? '');
    const angle = cleanToken(ov?.angle !== undefined ? ov.angle : (parsedName?.angle ?? ''));
    const angleInput = ov?.angle !== undefined ? ov.angle : angle;
    const variant = cleanToken(
      ov?.variant !== undefined ? ov.variant : (parsedName?.variant ?? ''),
    );
    const variantInput = ov?.variant !== undefined ? ov.variant : variant;
    const state = cleanToken(ov?.state !== undefined ? ov.state : (parsedName?.state ?? ''));
    const stateInput = ov?.state !== undefined ? ov.state : state;
    const version = parsedName?.version ?? 1;
    const rejected = ov?.rejected !== undefined ? ov.rejected : Boolean(parsedName?.rejected);
    const primary = !rejected && (angle === 'front' || !angle);
    const canonicalName = buildCanonicalPreviewName({
      project,
      type,
      alias: cleanToken(tag, true) || fallbackTag,
      angle,
      variant,
      state,
      version,
      rejected,
      ext: fileExt(image.fileName),
    });
    const excluded = ov?.excluded ?? Boolean(image.error);

    const baseRow = {
      index,
      image,
      project,
      projectInput,
      type,
      tag,
      resolvedTag: tag,
      name,
      role,
      lock,
      angle,
      angleInput,
      variant,
      variantInput,
      state,
      stateInput,
      version,
      primary,
      rejected,
      canonicalName,
      hint: parsedName?.hint,
      excluded: false,
    };

    if (excluded) {
      return {
        ...baseRow,
        excluded: true,
        status: image.error ? 'error' : 'excluded',
        note: image.error || 'Đã bỏ chọn',
      };
    }

    if (image.error) {
      return {
        ...baseRow,
        status: 'error',
        note: image.error,
      };
    }

    if (ov?.role === undefined && parsedRole?.error) {
      return {
        ...baseRow,
        status: 'error',
        note: parsedRole.error,
      };
    }

    if (!isValidTag(tag)) {
      return {
        ...baseRow,
        status: 'error',
        note: 'Tag chỉ gồm chữ, số, +, - và _ (tối đa 40 ký tự)',
      };
    }

    const scopeKey = assetScopeKey(project, tag);
    const inExisting = existingScopes.has(scopeKey);
    const inBatch = batchSeen.has(scopeKey);

    if (mergeByAlias && inBatch) {
      return {
        ...baseRow,
        status: 'ready',
        note: 'Gộp thêm góc vào @' + tag,
      };
    }

    if (mergeByAlias && inExisting) {
      batchSeen.add(scopeKey);
      return {
        ...baseRow,
        status: 'overwrite',
        note: 'Thêm góc vào tư liệu đã có',
      };
    }

    if (!inExisting && !inBatch) {
      taken.add(scopeKey);
      batchSeen.add(scopeKey);
      return {
        ...baseRow,
        status: 'ready',
      };
    }

    if (conflict === MuseAssetBatchConflict.Skip) {
      return {
        ...baseRow,
        status: 'skip',
        note: inExisting ? 'Trùng tag đã có (bỏ qua)' : 'Trùng tag trong danh sách (bỏ qua)',
      };
    }

    if (conflict === MuseAssetBatchConflict.Overwrite) {
      taken.add(scopeKey);
      batchSeen.add(scopeKey);
      return {
        ...baseRow,
        status: 'overwrite',
        note: inExisting ? 'Ghi đè tư liệu cũ' : 'Ghi đè dòng trùng trước đó',
      };
    }

    const resolvedTag = uniqueRenamedTag(tag, project, taken);
    const resolvedScope = assetScopeKey(project, resolvedTag);
    taken.add(resolvedScope);
    batchSeen.add(resolvedScope);
    return {
      ...baseRow,
      resolvedTag,
      canonicalName: buildCanonicalPreviewName({
        project,
        type,
        alias: cleanToken(resolvedTag, true) || fallbackTag,
        angle,
        variant,
        state,
        version,
        rejected,
        ext: fileExt(image.fileName),
      }),
      status: 'rename',
      note: `Đổi thành @${resolvedTag}`,
    };
  });

  const warnings: string[] = [];
  if (images.length > 0) {
    if (aliases.length > 0 && aliases.length !== images.length) {
      warnings.push(`Alias có ${aliases.length} dòng nhưng danh sách có ${images.length} ảnh.`);
    }
    if (roles.length > 0 && roles.length !== images.length) {
      warnings.push(`Vai trò có ${roles.length} dòng nhưng danh sách có ${images.length} ảnh.`);
    }
    if (locks.length > 0 && locks.length !== images.length) {
      warnings.push(`Prompt khoá có ${locks.length} mục nhưng danh sách có ${images.length} ảnh.`);
    }
  }

  return { rows, warnings };
}
