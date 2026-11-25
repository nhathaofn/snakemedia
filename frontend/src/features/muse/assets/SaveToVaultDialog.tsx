import { useEffect, useMemo, useState } from 'react';
import '../../../components/batch/BatchForm.css';
import '../../../components/studio/Studio.css';
import './MuseAssetsPage.css';
import { Button, Modal } from '../../../components/ui';
import { api, thumbUrl, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseAssetType, MuseMediaAssetImportRequest, MuseRefRole } from '../../../types';
import type { MuseAsset } from '../../../types';
import { useMuseStudioStore } from '../shared/museStudioStore';
import {
  ANGLE_SUGGESTIONS,
  ASSET_ROLES,
  ASSET_TYPES,
  STATE_SUGGESTIONS,
  VARIANT_SUGGESTIONS,
  buildCanonicalPreviewName,
  cleanToken,
  defaultRoleForAssetType,
  defaultTypeForRole,
  normalizeProjectName,
} from './assetRoles';
import { BatchImportAssetsDialog } from './BatchImportAssetsDialog';
import { fileExt, sanitizeTag } from './batchImport';

export interface VaultImportSource {
  localUrl: string;
  suggestedPrompt?: string;
  suggestedName?: string;
}

export interface SaveToVaultDialogProps {
  sources: VaultImportSource[];
  defaultProject?: string;
  onClose: () => void;
  onSaved?: (count: number) => void;
}

/**
 * 1-click bridge from Flow or Muse generated images into the Muse Asset Vault:
 * - Single image: compact dialog to save as a new Vault asset OR attach as a new
 *   angle/variant onto an existing asset.
 * - Multiple images: opens the Batch Import dialog preloaded with the images.
 */
export function SaveToVaultDialog({
  sources,
  defaultProject = '',
  onClose,
  onSaved,
}: SaveToVaultDialogProps) {
  const assets = useMuseStudioStore((s) => s.assets);
  const loaded = useMuseStudioStore((s) => s.loaded);
  const ensure = useMuseStudioStore((s) => s.ensure);
  const load = useMuseStudioStore((s) => s.load);

  useEffect(() => {
    if (!loaded) ensure();
  }, [loaded, ensure]);

  if (sources.length === 0) return null;

  if (sources.length > 1) {
    return (
      <BatchImportAssetsDialog
        existingAssets={assets}
        initialMediaSources={sources}
        defaultProject={defaultProject}
        onImported={async () => {
          await load();
          onSaved?.(sources.length);
        }}
        onClose={onClose}
      />
    );
  }

  return (
    <SingleMediaToVaultModal
      source={sources[0]}
      existingAssets={assets}
      defaultProject={defaultProject}
      onSaved={async () => {
        await load();
        onSaved?.(1);
        onClose();
      }}
      onClose={onClose}
    />
  );
}

function SingleMediaToVaultModal({
  source,
  existingAssets,
  defaultProject,
  onSaved,
  onClose,
}: {
  source: VaultImportSource;
  existingAssets: readonly MuseAsset[];
  defaultProject: string;
  onSaved: () => Promise<void>;
  onClose: () => void;
}) {
  const addToast = useAppStore((s) => s.addToast);
  const projects = useMemo(
    () =>
      Array.from(
        new Set(existingAssets.map((a) => (a.project ?? '').trim()).filter(Boolean)),
      ).sort(),
    [existingAssets],
  );
  const initialTag = useMemo(() => {
    const raw = source.suggestedName || source.suggestedPrompt || 'tu_lieu';
    return cleanToken(raw.split(/\s+/).slice(0, 3).join('_')) || sanitizeTag(raw, 'tu_lieu');
  }, [source]);

  const [mode, setMode] = useState<'new' | 'append'>('new');
  const [targetId, setTargetId] = useState<string>(() => existingAssets[0]?.id ?? '');
  const [project, setProject] = useState(normalizeProjectName(defaultProject));
  const [assetType, setAssetType] = useState<MuseAssetType>(MuseAssetType.Char);
  const [role, setRole] = useState<MuseRefRole>(MuseRefRole.Character);
  const [tag, setTag] = useState(initialTag);
  const [name, setName] = useState('');
  const [lock, setLock] = useState('');
  const [angle, setAngle] = useState('front');
  const [variant, setVariant] = useState('');
  const [state, setState] = useState('');
  const [primary, setPrimary] = useState(true);
  const [saving, setSaving] = useState(false);

  const selectedTarget =
    mode === 'append' ? existingAssets.find((a) => a.id === targetId) : undefined;
  const effProject = selectedTarget ? (selectedTarget.project ?? '') : project;
  const effType = selectedTarget ? selectedTarget.type || assetType : assetType;
  const effTag = selectedTarget ? selectedTarget.tag : tag;

  const canonicalPreview = buildCanonicalPreviewName({
    project: effProject,
    type: effType,
    alias: effTag || 'asset',
    angle,
    variant,
    state,
    version: 1,
    rejected: false,
    ext: fileExt(source.localUrl),
  });

  const handleSubmit = async () => {
    if (saving) return;
    setSaving(true);
    try {
      const req = MuseMediaAssetImportRequest.createFrom({
        sourceUrl: source.localUrl,
        targetAssetId: mode === 'append' ? targetId : undefined,
        project: effProject,
        type: effType,
        tag: effTag,
        name: selectedTarget ? selectedTarget.name : name,
        role: selectedTarget ? selectedTarget.role : role,
        lock: selectedTarget ? undefined : lock,
        itemLock: mode === 'append' ? lock : undefined,
        angle,
        variant,
        state,
        primary,
      });
      const saved = unwrap(await api.importMuseAssetFromMedia(req));
      addToast({
        type: 'success',
        message: `Đã lưu vào Kho tư liệu (@${saved.tag} · ${canonicalPreview}).`,
      });
      await onSaved();
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title="Lưu ảnh vào Kho Tư liệu Muse"
      subtitle="Ảnh giữ nguyên định dạng gốc trong kho và tự đặt tên theo quy ước"
      className="muse-asset-dialog"
      onClose={() => !saving && onClose()}
      footer={
        <div className="muse-dialog-actions">
          <Button onClick={onClose} disabled={saving}>
            Huỷ
          </Button>
          <Button
            variant="primary"
            disabled={saving || (mode === 'append' && !targetId)}
            onClick={() => void handleSubmit()}
          >
            {saving ? 'Đang lưu…' : 'Lưu vào Kho tư liệu'}
          </Button>
        </div>
      }
    >
      <div className="muse-asset-form">
        <div className="muse-vault-save-head">
          <img
            className="muse-vault-save-thumb"
            src={thumbUrl(source.localUrl)}
            alt=""
            decoding="async"
          />
          <div className="muse-vault-save-meta">
            <span className="studio-field-label">Tên file sẽ lưu trong kho</span>
            <code className="muse-vault-preview-name">
              {(normalizeProjectName(effProject) || '_chung') + '/' + canonicalPreview}
            </code>
            {existingAssets.length > 0 && (
              <div className="muse-vault-mode-tabs">
                <button
                  type="button"
                  className={`muse-vault-mode-btn${mode === 'new' ? ' is-on' : ''}`}
                  onClick={() => setMode('new')}
                >
                  Tạo tư liệu mới
                </button>
                <button
                  type="button"
                  className={`muse-vault-mode-btn${mode === 'append' ? ' is-on' : ''}`}
                  onClick={() => {
                    setMode('append');
                    if (!targetId && existingAssets[0]) setTargetId(existingAssets[0].id);
                  }}
                >
                  Thêm làm góc của @tag đã có
                </button>
              </div>
            )}
          </div>
        </div>

        {mode === 'append' ? (
          <label className="studio-field-group">
            <span className="studio-field-label">Chọn tư liệu đích</span>
            <select
              className="batch-input batch-select"
              value={targetId}
              onChange={(e) => setTargetId(e.target.value)}
            >
              {existingAssets.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.project ? `[${a.project}] ` : ''}@{a.tag}
                  {a.name ? ` · ${a.name}` : ''} ({a.images.length} ảnh)
                </option>
              ))}
            </select>
          </label>
        ) : (
          <>
            <div className="muse-form-row">
              <label className="studio-field-group">
                <span className="studio-field-label">Dự án (tuỳ chọn)</span>
                <input
                  className="batch-input"
                  list="vault-projects-datalist"
                  value={project}
                  placeholder="OLIVA (_chung nếu trống)"
                  onChange={(e) => setProject(e.target.value)}
                />
                <datalist id="vault-projects-datalist">
                  {projects.map((p) => (
                    <option key={p} value={p} />
                  ))}
                </datalist>
              </label>
              <label className="studio-field-group">
                <span className="studio-field-label">Loại tư liệu</span>
                <select
                  className="batch-input batch-select"
                  value={assetType}
                  onChange={(e) => {
                    const nextType = e.target.value as MuseAssetType;
                    setAssetType(nextType);
                    setRole(defaultRoleForAssetType(nextType));
                  }}
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
                <span className="studio-field-label">Tag (@alias)</span>
                <div className="muse-tag-input">
                  <span>@</span>
                  <input
                    className="batch-input"
                    value={tag}
                    placeholder="jimena"
                    onChange={(e) => setTag(e.target.value)}
                  />
                </div>
              </label>
              <label className="studio-field-group">
                <span className="studio-field-label">Vai trò khi gửi Muse</span>
                <select
                  className="batch-input batch-select"
                  value={role}
                  onChange={(e) => {
                    const nextRole = e.target.value as MuseRefRole;
                    setRole(nextRole);
                    setAssetType(defaultTypeForRole(nextRole));
                  }}
                >
                  {ASSET_ROLES.map((r) => (
                    <option key={r.value} value={r.value}>
                      {r.label}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <label className="studio-field-group">
              <span className="studio-field-label">Tên hiển thị (tuỳ chọn)</span>
              <input
                className="batch-input"
                value={name}
                placeholder="Cô gái áo đỏ / Jimena"
                onChange={(e) => setName(e.target.value)}
              />
            </label>
          </>
        )}

        <div className="muse-form-row-3">
          <label className="studio-field-group">
            <span className="studio-field-label">Góc nhìn / khu vực</span>
            <input
              className="batch-input"
              list="vault-angle-list"
              value={angle}
              placeholder="front, close, 34, wide..."
              onChange={(e) => setAngle(e.target.value)}
            />
            <datalist id="vault-angle-list">
              {ANGLE_SUGGESTIONS.map((s) => (
                <option key={s} value={s} />
              ))}
            </datalist>
          </label>
          <label className="studio-field-group">
            <span className="studio-field-label">Biến thể / thời kỳ</span>
            <input
              className="batch-input"
              list="vault-variant-list"
              value={variant}
              placeholder="work, dress, 1920..."
              onChange={(e) => setVariant(e.target.value)}
            />
            <datalist id="vault-variant-list">
              {VARIANT_SUGGESTIONS.map((s) => (
                <option key={s} value={s} />
              ))}
            </datalist>
          </label>
          <label className="studio-field-group">
            <span className="studio-field-label">Trạng thái</span>
            <input
              className="batch-input"
              list="vault-state-list"
              value={state}
              placeholder="soot, wet, night..."
              onChange={(e) => setState(e.target.value)}
            />
            <datalist id="vault-state-list">
              {STATE_SUGGESTIONS.map((s) => (
                <option key={s} value={s} />
              ))}
            </datalist>
          </label>
        </div>

        <label className="muse-check">
          <input type="checkbox" checked={primary} onChange={(e) => setPrimary(e.target.checked)} />
          <span>Đặt làm ảnh chính (mặc định gắn khi prompt chỉ gọi @{effTag || 'tag'})</span>
        </label>

        <label className="studio-field-group">
          <span className="studio-field-label">
            {mode === 'append' ? 'Ghi chú khoá riêng cho góc này' : 'Khoá chi tiết giữ y hệt'}
          </span>
          <input
            className="batch-input"
            value={lock}
            placeholder="nốt ruồi má trái, khăn quàng đỏ..."
            onChange={(e) => setLock(e.target.value)}
          />
        </label>
      </div>
    </Modal>
  );
}
