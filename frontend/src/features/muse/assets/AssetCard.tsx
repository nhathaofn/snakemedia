import { useRef } from 'react';
import { ImageOff, Pencil, Trash2 } from 'lucide-react';
import { Icon, IconAction } from '../../../components/ui';
import { thumbUrl } from '../../../services';
import type { MuseAsset } from '../../../types';
import { defaultTypeForRole, roles } from './assetRoles';

interface AssetCardProps {
  asset: MuseAsset;
  selected: boolean;
  hasSelection: boolean;
  onToggleSelect: (id: string, shiftKey: boolean) => void;
  onEdit: (asset: MuseAsset) => void;
  onDelete: (asset: MuseAsset) => void;
}

/** One Muse Vault asset card: up to 3 preview views, role pill, project/type tags, and quick edit/delete. */
export function AssetCard({
  asset,
  selected,
  hasSelection,
  onToggleSelect,
  onEdit,
  onDelete,
}: AssetCardProps) {
  const shiftHeld = useRef(false);
  const effType = asset.type || defaultTypeForRole(asset.role);
  const totalCount = asset.items?.length || asset.images.length;
  const activeCount = asset.images.length;
  const coverUrls = asset.images.slice(0, 3);
  const countLabel =
    totalCount > activeCount ? `${activeCount}/${totalCount} ảnh` : `${totalCount} ảnh`;

  return (
    <article className={`muse-asset-card${selected ? ' is-selected' : ''}`}>
      <div
        className={`muse-asset-cover is-${Math.min(coverUrls.length || 1, 3)}`}
        onClick={(e) => {
          if (hasSelection || e.shiftKey || e.ctrlKey || e.metaKey) {
            onToggleSelect(asset.id, e.shiftKey);
          } else {
            onEdit(asset);
          }
        }}
      >
        {coverUrls.length > 0 ? (
          coverUrls.map((img) => (
            <img key={img} src={thumbUrl(img)} alt={asset.tag} loading="lazy" decoding="async" />
          ))
        ) : (
          <div className="muse-asset-cover-empty">
            <Icon icon={ImageOff} size="lg" />
            <span>Chỉ có ảnh loại</span>
          </div>
        )}
        <span className={`muse-role-badge is-${asset.role || 'none'}`}>
          {roles[asset.role].label}
        </span>
        {totalCount > 1 && <span className="muse-asset-count-pill">{countLabel}</span>}
        <label
          className={`muse-asset-check${selected ? ' is-on' : ''}`}
          onClick={(e) => e.stopPropagation()}
        >
          <input
            type="checkbox"
            checked={selected}
            aria-label={`Chọn @${asset.tag}`}
            onMouseDown={(e) => {
              shiftHeld.current = e.shiftKey;
            }}
            onKeyDown={(e) => {
              shiftHeld.current = e.shiftKey;
            }}
            onChange={() => {
              const shift = shiftHeld.current;
              shiftHeld.current = false;
              onToggleSelect(asset.id, shift);
            }}
          />
        </label>
      </div>
      <div className="muse-asset-body">
        <div className="muse-asset-submeta">
          <span className="muse-type-tag">{effType}</span>
          {asset.project && <span className="muse-proj-tag">{asset.project}</span>}
        </div>
        <div className="muse-asset-name">
          <strong>@{asset.tag}</strong>
          {asset.name && <span title={asset.name}>{asset.name}</span>}
        </div>
        {asset.lock && (
          <p className="muse-asset-lock" title={asset.lock}>
            Khoá: {asset.lock}
          </p>
        )}
      </div>
      <footer className="muse-asset-footer">
        <IconAction icon={Pencil} label="Sửa" onClick={() => onEdit(asset)} />
        <IconAction icon={Trash2} label="Xoá" onClick={() => onDelete(asset)} />
      </footer>
    </article>
  );
}
