import { MuseAssetType, MuseRefRole } from '../../../types';

/** Options offered for every Muse asset role, in creation-workflow order. */
export const ASSET_ROLES: readonly { value: MuseRefRole; label: string; hint: string }[] = [
  {
    value: MuseRefRole.Face,
    label: 'Gương mặt',
    hint: 'Giữ nét mặt và tóc; trang phục và dáng theo prompt',
  },
  {
    value: MuseRefRole.Character,
    label: 'Nhân vật',
    hint: 'Giữ gương mặt, vóc dáng, tóc và bộ trang phục trong ảnh',
  },
  {
    value: MuseRefRole.Animal,
    label: 'Thú / sinh vật',
    hint: 'Giữ loài, màu lông và đặc điểm riêng của con vật',
  },
  {
    value: MuseRefRole.Product,
    label: 'Sản phẩm',
    hint: 'Giữ hình dáng, màu sắc và chữ trên bao bì',
  },
  {
    value: MuseRefRole.Prop,
    label: 'Đạo cụ',
    hint: 'Giữ hình dáng, chất liệu và màu của vật cầm theo cảnh',
  },
  {
    value: MuseRefRole.Vehicle,
    label: 'Xe cộ / phương tiện',
    hint: 'Giữ kiểu dáng xe, màu sơn và chi tiết đặc trưng',
  },
  {
    value: MuseRefRole.Document,
    label: 'Tài liệu / bảng biểu',
    hint: 'Giữ bố cục và chữ đọc được trên tài liệu / sơ đồ',
  },
  {
    value: MuseRefRole.Setting,
    label: 'Bối cảnh',
    hint: 'Giữ kiến trúc, bố cục và ánh sáng, không lấy người trong ảnh',
  },
  {
    value: MuseRefRole.Storyboard,
    label: 'Storyboard',
    hint: 'Giữ góc máy và bố cục tham chiếu của cảnh',
  },
  {
    value: MuseRefRole.Style,
    label: 'Phong cách',
    hint: 'Chỉ lấy tông màu và chất liệu hình ảnh',
  },
  { value: MuseRefRole.None, label: 'Chung', hint: 'Chỉ mô tả ngắn, không ép quy tắc vai trò' },
];

export const roleOrder: readonly MuseRefRole[] = ASSET_ROLES.map((r) => r.value);

export const roles: Record<MuseRefRole, { label: string; detail: string }> = Object.fromEntries(
  ASSET_ROLES.map((r) => [r.value, { label: r.label, detail: r.hint }]),
) as Record<MuseRefRole, { label: string; detail: string }>;

export const ASSET_TYPES: readonly { value: MuseAssetType; label: string; short: string }[] = [
  { value: MuseAssetType.Char, label: 'CHAR · Nhân vật', short: 'CHAR' },
  { value: MuseAssetType.Anim, label: 'ANIM · Thú / sinh vật', short: 'ANIM' },
  { value: MuseAssetType.Loc, label: 'LOC · Bối cảnh', short: 'LOC' },
  { value: MuseAssetType.Prop, label: 'PROP · Đạo cụ / SP', short: 'PROP' },
  { value: MuseAssetType.Veh, label: 'VEH · Xe cộ', short: 'VEH' },
  { value: MuseAssetType.Doc, label: 'DOC · Tài liệu', short: 'DOC' },
  { value: MuseAssetType.Style, label: 'STYLE · Phong cách', short: 'STYLE' },
];

export const ANGLE_SUGGESTIONS: readonly string[] = [
  'front',
  'close',
  '34',
  'side',
  'back',
  'full',
  'wide',
  'table',
  'door',
  'window',
  'phone',
];

export const VARIANT_SUGGESTIONS: readonly string[] = ['work', 'dress', 'uniform', 'young', 'old'];

export const STATE_SUGGESTIONS: readonly string[] = [
  'soot',
  'wet',
  'injured',
  'crying',
  'dawn',
  'day',
  'dusk',
  'night',
  'before',
  'burning',
  'after',
  'storm',
  'rain',
];

export function assetRoleLabel(role: MuseRefRole): string {
  return ASSET_ROLES.find((r) => r.value === role)?.label ?? role;
}

export function defaultTypeForRole(role: MuseRefRole): MuseAssetType {
  switch (role) {
    case MuseRefRole.Face:
    case MuseRefRole.Character:
      return MuseAssetType.Char;
    case MuseRefRole.Animal:
      return MuseAssetType.Anim;
    case MuseRefRole.Setting:
    case MuseRefRole.Storyboard:
      return MuseAssetType.Loc;
    case MuseRefRole.Vehicle:
      return MuseAssetType.Veh;
    case MuseRefRole.Document:
      return MuseAssetType.Doc;
    case MuseRefRole.Style:
      return MuseAssetType.Style;
    default:
      return MuseAssetType.Prop;
  }
}

export function defaultRoleForAssetType(type: MuseAssetType): MuseRefRole {
  switch (type) {
    case MuseAssetType.Char:
      return MuseRefRole.Character;
    case MuseAssetType.Anim:
      return MuseRefRole.Animal;
    case MuseAssetType.Loc:
      return MuseRefRole.Setting;
    case MuseAssetType.Veh:
      return MuseRefRole.Vehicle;
    case MuseAssetType.Doc:
      return MuseRefRole.Document;
    case MuseAssetType.Style:
      return MuseRefRole.Style;
    default:
      return MuseRefRole.Prop;
  }
}

export function cleanToken(raw: string, allowPlus = false): string {
  let s = raw
    .trim()
    .toLowerCase()
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/đ/g, 'd');
  s = allowPlus
    ? s.replace(/[^a-z0-9_+]+/g, '_').replace(/_*\+_*|\+{2,}/g, '+')
    : s.replace(/[^a-z0-9]+/g, '_');
  return s.replace(/^[_+]+|[_+]+$/g, '');
}

export function normalizeProjectName(raw: string): string {
  const s = cleanToken(raw);
  if (!s || s === '_chung') return '';
  return s.toUpperCase();
}

export function buildCanonicalPreviewName(opts: {
  project?: string;
  type?: MuseAssetType;
  alias: string;
  angle?: string;
  variant?: string;
  state?: string;
  version?: number;
  rejected?: boolean;
  ext?: string;
}): string {
  const parts: string[] = [];
  const proj = normalizeProjectName(opts.project ?? '');
  if (proj) parts.push(proj);
  parts.push(opts.type || MuseAssetType.Prop);
  parts.push(cleanToken(opts.alias, true) || 'asset');
  const angle = cleanToken(opts.angle ?? '');
  if (angle) parts.push(angle);
  const variant = cleanToken(opts.variant ?? '');
  if (variant) parts.push(variant);
  const state = cleanToken(opts.state ?? '');
  if (state) parts.push(state);
  if ((opts.version ?? 0) >= 2) parts.push(`v${opts.version}`);
  if (opts.rejected) parts.push('rej');
  const ext = (opts.ext || '.png').toLowerCase();
  return `${parts.join('_')}${ext.startsWith('.') ? ext : `.${ext}`}`;
}
