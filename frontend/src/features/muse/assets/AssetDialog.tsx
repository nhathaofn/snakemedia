import { useState } from 'react';
import { EyeOff, FolderOpen, ImagePlus, Star, X } from 'lucide-react';
import { Button, Icon, Modal } from '../../../components/ui';
import { api, thumbUrl, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseAsset, MuseAssetItem, MuseRefRole } from '../../../types';
import type { MuseAssetType } from '../../../types';
import {
  ANGLE_SUGGESTIONS,
  ASSET_TYPES,
  STATE_SUGGESTIONS,
  VARIANT_SUGGESTIONS,
  buildCanonicalPreviewName,
  defaultTypeForRole,
  roleOrder,
  roles,
} from './assetRoles';
import { fileExt } from './batchImport';

/** Add or edit one asset: unlimited angle/variant items, project, type, tag, name, role and lock. */
export function AssetDialog({
  asset,
  onSave,
  onClose,
}: {
  asset: MuseAsset;
  onSave: (asset: MuseAsset) => Promise<boolean>;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState<MuseAsset>(() => {
    const items =
      asset.items && asset.items.length > 0
        ? asset.items.map((it) => MuseAssetItem.createFrom(it))
        : asset.images.map((url, idx) =>
            MuseAssetItem.createFrom({
              id: '',
              url,
              version: 1,
              primary: idx === 0,
            }),
          );
    return MuseAsset.createFrom({
      ...asset,
      type: asset.type || defaultTypeForRole(asset.role),
      items,
    });
  });
  const addToast = useAppStore((s) => s.addToast);
  const [saving, setSaving] = useState(false);
  const items = draft.items ?? [];

  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    try {
      await onSave(MuseAsset.createFrom(draft));
    } finally {
      setSaving(false);
    }
  };
  const patchDraft = (patch: Partial<MuseAsset>) =>
    setDraft((prev) => MuseAsset.createFrom({ ...prev, ...patch }));

  const updateItem = (idx: number, patch: Partial<MuseAssetItem>) => {
    const next = items.map((it, i) => {
      if (i === idx) {
        const merged = MuseAssetItem.createFrom({ ...it, ...patch });
        if (merged.rejected) merged.primary = false;
        return merged;
      }
      return it;
    });
    patchDraft({
      items: next,
      images: next.filter((it) => !it.rejected).map((it) => it.url),
    });
  };

  const setPrimaryItem = (idx: number) => {
    const next = items.map((it, i) =>
      MuseAssetItem.createFrom({
        ...it,
        primary: i === idx && !it.rejected,
      }),
    );
    patchDraft({ items: next });
  };

  const removeItem = (idx: number) => {
    const next = items.filter((_, i) => i !== idx);
    patchDraft({
      items: next,
      images: next.filter((it) => !it.rejected).map((it) => it.url),
    });
  };

  const pick = async () => {
    try {
      const picked = unwrap(await api.pickMuseAssetImages());
      const added = picked.map((p, idx) =>
        MuseAssetItem.createFrom({
          id: '',
          url: p.localUrl,
          version: 1,
          primary: items.length === 0 && idx === 0,
        }),
      );
      const next = [...items, ...added];
      patchDraft({
        items: next,
        images: next.filter((it) => !it.rejected).map((it) => it.url),
      });
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  return (
    <Modal
      title={asset.id ? `Sửa @${asset.tag}` : 'Thêm tư liệu vào Kho'}
      subtitle="Không giới hạn số góc nhìn / biến thể. Đánh dấu ★ Chính để tự chọn khi prompt chỉ ghi @tag."
      className="muse-asset-dialog"
      onClose={() => !saving && onClose()}
      footer={
        <div className="muse-dialog-actions">
          <Button onClick={onClose} disabled={saving}>
            Huỷ
          </Button>
          <Button variant="primary" disabled={saving} onClick={() => void handleSave()}>
            {saving ? 'Đang lưu…' : 'Lưu vào Kho'}
          </Button>
        </div>
      }
    >
      <div className="muse-asset-form">
        <div className="muse-asset-items-grid">
          {items.map((it, idx) => {
            const canonical = buildCanonicalPreviewName({
              project: draft.project,
              type: draft.type,
              alias: draft.tag || 'asset',
              angle: it.angle,
              variant: it.variant,
              state: it.state,
              version: it.version || 1,
              rejected: Boolean(it.rejected),
              ext: fileExt(it.url),
            });
            return (
              <div
                key={it.id || `${it.url}-${idx}`}
                className={`muse-vault-item-card${it.primary ? ' is-primary' : ''}${
                  it.rejected ? ' is-rejected' : ''
                }`}
              >
                <div className="muse-vault-item-thumb-wrap">
                  <img src={thumbUrl(it.url)} alt="" decoding="async" />
                  <div className="muse-vault-item-tools">
                    <button
                      type="button"
                      className={`muse-vault-tool-btn${it.primary ? ' is-on' : ''}`}
                      title="Đặt làm ảnh chính (@tag mặc định lấy ảnh này)"
                      disabled={Boolean(it.rejected)}
                      onClick={() => setPrimaryItem(idx)}
                    >
                      <Icon icon={Star} size="sm" />
                    </button>
                    <button
                      type="button"
                      className={`muse-vault-tool-btn${it.rejected ? ' is-warn' : ''}`}
                      title={it.rejected ? 'Khôi phục ảnh dùng được' : 'Chuyển vào loại (rejected)'}
                      onClick={() => updateItem(idx, { rejected: !it.rejected })}
                    >
                      <Icon icon={EyeOff} size="sm" />
                    </button>
                    {it.url.startsWith('/muse-vault/') && (
                      <button
                        type="button"
                        className="muse-vault-tool-btn"
                        title="Mở vị trí file trong Kho"
                        onClick={() => void api.revealFile(it.url)}
                      >
                        <Icon icon={FolderOpen} size="sm" />
                      </button>
                    )}
                    <button
                      type="button"
                      className="muse-vault-tool-btn is-danger"
                      title="Xoá ảnh này khỏi tư liệu"
                      onClick={() => removeItem(idx)}
                    >
                      <Icon icon={X} size="sm" />
                    </button>
                  </div>
                </div>
                <code className="muse-vault-item-filename" title={canonical}>
                  {canonical}
                </code>
                <div className="muse-vault-item-fields">
                  <input
                    className="batch-input"
                    list="asset-angle-suggest"
                    placeholder="Góc (front/close)"
                    value={it.angle ?? ''}
                    onChange={(e) => updateItem(idx, { angle: e.target.value })}
                  />
                  <input
                    className="batch-input"
                    list="asset-variant-suggest"
                    placeholder="Biến thể (work)"
                    value={it.variant ?? ''}
                    onChange={(e) => updateItem(idx, { variant: e.target.value })}
                  />
                  <input
                    className="batch-input"
                    list="asset-state-suggest"
                    placeholder="Trạng thái (soot)"
                    value={it.state ?? ''}
                    onChange={(e) => updateItem(idx, { state: e.target.value })}
                  />
                  <input
                    className="batch-input"
                    placeholder="Khoá riêng góc này"
                    value={it.lock ?? ''}
                    onChange={(e) => updateItem(idx, { lock: e.target.value })}
                  />
                </div>
              </div>
            );
          })}
          <button type="button" className="muse-asset-image is-add" onClick={() => void pick()}>
            <Icon icon={ImagePlus} size="lg" />
            <span>Thêm ảnh ({items.length})</span>
          </button>
        </div>

        <datalist id="asset-angle-suggest">
          {ANGLE_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <datalist id="asset-variant-suggest">
          {VARIANT_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <datalist id="asset-state-suggest">
          {STATE_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>

        <div className="muse-form-row">
          <label className="studio-field-group">
            <span className="studio-field-label">Dự án</span>
            <input
              className="batch-input"
              value={draft.project ?? ''}
              placeholder="OLIVA (trống = _chung)"
              onChange={(e) => patchDraft({ project: e.target.value })}
            />
          </label>
          <label className="studio-field-group">
            <span className="studio-field-label">Loại tư liệu</span>
            <select
              className="batch-input batch-select"
              value={draft.type || defaultTypeForRole(draft.role)}
              onChange={(e) => patchDraft({ type: e.target.value as MuseAssetType })}
            >
              {ASSET_TYPES.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="muse-form-row">
          <label className="studio-field-group">
            <span className="studio-field-label">Tag</span>
            <div className="muse-tag-input">
              <span>@</span>
              <input
                className="batch-input"
                value={draft.tag}
                placeholder="jimena, xe_tho"
                onChange={(e) => patchDraft({ tag: e.target.value })}
              />
            </div>
          </label>
          <label className="studio-field-group">
            <span className="studio-field-label">Tên (tuỳ chọn)</span>
            <input
              className="batch-input"
              value={draft.name}
              placeholder="Jimena"
              onChange={(e) => patchDraft({ name: e.target.value })}
            />
          </label>
        </div>
        <fieldset className="studio-field-group muse-role-picker">
          <legend className="studio-field-label">Vai trò</legend>
          <div className="muse-role-options">
            {roleOrder.map((r) => (
              <label
                key={r}
                className={`muse-role-option${draft.role === r ? ' is-on' : ''}${r === MuseRefRole.None ? ' is-wide' : ''}`}
              >
                <input
                  type="radio"
                  name="muse-role"
                  checked={draft.role === r}
                  onChange={() =>
                    patchDraft({
                      role: r,
                      type: defaultTypeForRole(r),
                    })
                  }
                />
                <strong>{roles[r].label}</strong>
                <span>{roles[r].detail}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <label className="studio-field-group">
          <span className="studio-field-label">Khoá (chi tiết phải giữ y hệt)</span>
          <textarea
            className="batch-input"
            rows={2}
            placeholder="nốt ruồi má trái, áo khoác đỏ logo ABC, chữ trên hộp"
            value={draft.lock ?? ''}
            onChange={(e) => patchDraft({ lock: e.target.value })}
          />
        </label>
      </div>
    </Modal>
  );
}
