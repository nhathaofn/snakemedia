import { MuseRefRole } from '../../../types';

/** A role's short name (badge) and what it makes Muse do (form). */
export const roles: Record<MuseRefRole, { label: string; detail: string }> = {
  [MuseRefRole.None]: { label: 'Tham chiếu', detail: 'Tham chiếu chung, không vai trò riêng' },
  [MuseRefRole.Face]: { label: 'Khuôn mặt', detail: 'Chỉ giữ danh tính: mặt, tóc, màu da, tuổi' },
  [MuseRefRole.Character]: {
    label: 'Nhân vật',
    detail: 'Giữ cả mặt, dáng người và trang phục',
  },
  [MuseRefRole.Product]: {
    label: 'Sản phẩm',
    detail: 'Ưu tiên cao nhất: hình dáng, màu, chữ, logo',
  },
  [MuseRefRole.Prop]: {
    label: 'Đạo cụ',
    detail: 'Giữ hình dáng, chất liệu và màu sắc; không lấy người hay hậu cảnh',
  },
  [MuseRefRole.Storyboard]: {
    label: 'Storyboard',
    detail: 'Bố cục, góc máy, bối cảnh, trang phục',
  },
  [MuseRefRole.Setting]: { label: 'Bối cảnh', detail: 'Địa điểm, ánh sáng, màu sắc' },
  [MuseRefRole.Style]: { label: 'Phong cách', detail: 'Chỉ lấy màu, ánh sáng, chất ảnh' },
};

/** Roles in the order the forms offer them: who, what, where, then the rest. */
export const roleOrder: readonly MuseRefRole[] = [
  MuseRefRole.Character,
  MuseRefRole.Face,
  MuseRefRole.Prop,
  MuseRefRole.Product,
  MuseRefRole.Storyboard,
  MuseRefRole.Setting,
  MuseRefRole.Style,
  MuseRefRole.None,
];
