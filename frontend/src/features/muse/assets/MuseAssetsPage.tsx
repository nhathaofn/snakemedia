import { thumbUrl } from '../../../services';
import { useState } from 'react';
import { FileText, ImagePlus, Pencil, Plus, Trash2, Users, X } from 'lucide-react';
import '../shared/Muse.css';
import './MuseAssetsPage.css';
import { Button, Icon, IconAction, Modal } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseAsset, MusePromptTemplate, MuseRefRole } from '../../../types';
import { useMuseStore } from '../shared/museStore';
import { useMuseStudio } from '../shared/StudioExtras';

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
  [MuseRefRole.Storyboard]: {
    label: 'Storyboard',
    detail: 'Bố cục, góc máy, bối cảnh, trang phục',
  },
  [MuseRefRole.Setting]: { label: 'Bối cảnh', detail: 'Địa điểm, ánh sáng, màu sắc' },
  [MuseRefRole.Style]: { label: 'Phong cách', detail: 'Chỉ lấy màu, ánh sáng, chất ảnh' },
};

/** Roles in the order the form offers them: who, what, where, then the rest. */
const roleOrder: readonly MuseRefRole[] = [
  MuseRefRole.Character,
  MuseRefRole.Face,
  MuseRefRole.Product,
  MuseRefRole.Storyboard,
  MuseRefRole.Setting,
  MuseRefRole.Style,
  MuseRefRole.None,
];

const emptyAsset = (): MuseAsset =>
  MuseAsset.createFrom({
    id: '',
    tag: '',
    name: '',
    role: MuseRefRole.Character,
    lock: '',
    images: [],
    createdAt: '',
  });

const emptyTemplate = (): MusePromptTemplate =>
  MusePromptTemplate.createFrom({ id: '', name: '', text: '', createdAt: '' });

/**
 * Tư liệu: characters, products, settings and styles kept by the app. A prompt
 * names one with @tag (or its name) and its images go with the message, with
 * a rule for their role and the details locked; saved prompts are listed below.
 */
export function MuseAssetsPage() {
  const { assets, templates, load, loadError } = useMuseStudio();
  const addToast = useAppStore((s) => s.addToast);
  const [editing, setEditing] = useState<MuseAsset | null>(null);
  const [template, setTemplate] = useState<MusePromptTemplate | null>(null);

  /** Runs a change, reloads the library, and reports whether it worked. */
  const act = async (action: () => Promise<void>) => {
    try {
      await action();
      await load();
      return true;
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
      return false;
    }
  };

  return (
    <div className="page-shell muse-page">
      <header className="muse-header">
        <div className="muse-assets-heading">
          <h1 className="muse-title">Tư liệu Muse</h1>
          <p className="muse-assets-lead">
            Gọi trong prompt bằng <code>@tag</code> hoặc đúng tên. Ảnh được gửi kèm vai trò và chi
            tiết khoá để nhân vật, sản phẩm giữ đồng nhất.
          </p>
        </div>
        <Button variant="primary" onClick={() => setEditing(emptyAsset())}>
          <Icon icon={Plus} />
          <span>Thêm tư liệu</span>
        </Button>
      </header>

      {loadError && (
        <div role="alert" className="muse-alert muse-alert-danger">
          {loadError}
        </div>
      )}

      <section className="muse-assets-section">
        <h2 className="muse-assets-section-title">
          Nhân vật, sản phẩm, bối cảnh <span className="muse-assets-count">{assets.length}</span>
        </h2>
        {assets.length === 0 ? (
          <EmptyState
            icon={Users}
            title="Chưa có tư liệu"
            text="Thêm 1–3 ảnh của một nhân vật hoặc sản phẩm, đặt tag rồi gọi @tag trong prompt."
            action={
              <Button onClick={() => setEditing(emptyAsset())}>
                <Icon icon={Plus} />
                <span>Thêm tư liệu đầu tiên</span>
              </Button>
            }
          />
        ) : (
          <div className="muse-assets-grid">
            {assets.map((a) => (
              <article key={a.id} className="muse-asset-card">
                <div className={`muse-asset-cover is-${Math.min(a.images.length, 3)}`}>
                  {a.images.map((img) => (
                    <img
                      key={img}
                      src={thumbUrl(img)}
                      alt={a.tag}
                      loading="lazy"
                      decoding="async"
                    />
                  ))}
                  <span className={`muse-role-badge is-${a.role || 'none'}`}>
                    {roles[a.role].label}
                  </span>
                </div>
                <div className="muse-asset-body">
                  <div className="muse-asset-name">
                    <strong>@{a.tag}</strong>
                    {a.name && <span>{a.name}</span>}
                  </div>
                  {a.lock && <p className="muse-asset-lock">Khoá: {a.lock}</p>}
                </div>
                <footer className="muse-asset-footer">
                  <IconAction icon={Pencil} label="Sửa" onClick={() => setEditing(a)} />
                  <IconAction
                    icon={Trash2}
                    label="Xoá"
                    onClick={() => {
                      if (!window.confirm(`Xoá @${a.tag}? Tác vụ đã xếp hàng vẫn giữ ảnh của nó.`))
                        return;
                      void act(async () => void unwrap(await api.removeMuseAssets([a.id])));
                    }}
                  />
                </footer>
              </article>
            ))}
          </div>
        )}
      </section>

      <section className="muse-assets-section">
        <div className="muse-header">
          <h2 className="muse-assets-section-title">
            Mẫu prompt <span className="muse-assets-count">{templates.length}</span>
          </h2>
          <Button size="sm" onClick={() => setTemplate(emptyTemplate())}>
            <Icon icon={Plus} size="sm" />
            <span>Thêm mẫu</span>
          </Button>
        </div>
        {templates.length === 0 ? (
          <EmptyState
            icon={FileText}
            title="Chưa có mẫu prompt"
            text="Lưu những prompt hay dùng để chèn nhanh ở màn Đơn hoặc chép vào Hàng loạt."
          />
        ) : (
          <div className="muse-templates-grid">
            {templates.map((t) => (
              <article key={t.id} className="muse-template-card">
                <div className="muse-template-head">
                  <strong>{t.name}</strong>
                  <div className="muse-template-actions">
                    <IconAction icon={Pencil} label="Sửa" onClick={() => setTemplate(t)} />
                    <IconAction
                      icon={Trash2}
                      label="Xoá"
                      onClick={() => {
                        if (!window.confirm(`Xoá mẫu "${t.name}"?`)) return;
                        void act(async () => void unwrap(await api.removeMuseTemplate(t.id)));
                      }}
                    />
                  </div>
                </div>
                <p className="muse-template-text">{t.text}</p>
              </article>
            ))}
          </div>
        )}
      </section>

      {editing && (
        <AssetDialog
          asset={editing}
          onClose={() => setEditing(null)}
          onSave={(asset) =>
            act(async () => {
              unwrap(await api.saveMuseAsset(asset));
              setEditing(null);
              addToast({ type: 'success', message: `Đã lưu @${asset.tag}.` });
            })
          }
        />
      )}
      {template && (
        <TemplateDialog
          template={template}
          onClose={() => setTemplate(null)}
          onSave={(t) =>
            act(async () => {
              unwrap(await api.saveMuseTemplate(MusePromptTemplate.createFrom(t)));
              setTemplate(null);
            })
          }
        />
      )}
    </div>
  );
}

function EmptyState({
  icon,
  title,
  text,
  action,
}: {
  icon: typeof Users;
  title: string;
  text: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="muse-assets-empty">
      <Icon icon={icon} size="lg" />
      <strong>{title}</strong>
      <span>{text}</span>
      {action}
    </div>
  );
}

/** Add or edit one asset: images, tag, name, role and lock. */
function AssetDialog({
  asset,
  onSave,
  onClose,
}: {
  asset: MuseAsset;
  onSave: (asset: MuseAsset) => Promise<boolean>;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(asset);
  const addToast = useAppStore((s) => s.addToast);
  const maxImages = useMuseStore((s) => s.options?.maxAssetImages ?? 0);
  const pick = async () => {
    try {
      const picked = unwrap(await api.pickMuseAssetImages());
      setDraft({
        ...draft,
        images: [...draft.images, ...picked.map((p) => p.localUrl)].slice(0, maxImages),
      });
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };
  return (
    <Modal
      title={asset.id ? `Sửa @${asset.tag}` : 'Thêm tư liệu'}
      subtitle={`1–${maxImages || 1} ảnh của cùng một nhân vật hoặc sản phẩm, nhiều góc nhìn`}
      className="muse-asset-dialog"
      onClose={onClose}
      footer={
        <div className="muse-dialog-actions">
          <Button onClick={onClose}>Huỷ</Button>
          <Button variant="primary" onClick={() => void onSave(MuseAsset.createFrom(draft))}>
            Lưu tư liệu
          </Button>
        </div>
      }
    >
      <div className="muse-asset-form">
        <div className="muse-asset-images">
          {draft.images.map((img) => (
            <div key={img} className="muse-asset-image">
              <img src={img} alt="" decoding="async" />
              <button
                type="button"
                className="muse-thumb-remove"
                aria-label="Bỏ ảnh"
                onClick={() =>
                  setDraft({ ...draft, images: draft.images.filter((x) => x !== img) })
                }
              >
                <Icon icon={X} size="sm" />
              </button>
            </div>
          ))}
          {draft.images.length < maxImages && (
            <button type="button" className="muse-asset-image is-add" onClick={() => void pick()}>
              <Icon icon={ImagePlus} size="lg" />
              <span>Thêm ảnh</span>
            </button>
          )}
        </div>
        <div className="muse-form-row">
          <label className="studio-field-group">
            <span className="studio-field-label">Tag</span>
            <div className="muse-tag-input">
              <span>@</span>
              <input
                className="batch-input"
                value={draft.tag}
                placeholder="lan, hop-qua"
                onChange={(e) => setDraft({ ...draft, tag: e.target.value })}
              />
            </div>
          </label>
          <label className="studio-field-group">
            <span className="studio-field-label">Tên (tuỳ chọn)</span>
            <input
              className="batch-input"
              value={draft.name}
              placeholder="Lan"
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
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
                  onChange={() => setDraft({ ...draft, role: r })}
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
            onChange={(e) => setDraft({ ...draft, lock: e.target.value })}
          />
        </label>
      </div>
    </Modal>
  );
}

/** Add or edit one saved prompt. */
function TemplateDialog({
  template,
  onSave,
  onClose,
}: {
  template: MusePromptTemplate;
  onSave: (template: MusePromptTemplate) => Promise<boolean>;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(template);
  return (
    <Modal
      title={template.id ? 'Sửa mẫu prompt' : 'Thêm mẫu prompt'}
      onClose={onClose}
      footer={
        <div className="muse-dialog-actions">
          <Button onClick={onClose}>Huỷ</Button>
          <Button variant="primary" onClick={() => void onSave(draft)}>
            Lưu mẫu
          </Button>
        </div>
      }
    >
      <div className="muse-asset-form">
        <label className="studio-field-group">
          <span className="studio-field-label">Tên mẫu</span>
          <input
            className="batch-input"
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          />
        </label>
        <label className="studio-field-group">
          <span className="studio-field-label">Nội dung (có thể chứa @tag)</span>
          <textarea
            className="batch-input"
            rows={8}
            value={draft.text}
            onChange={(e) => setDraft({ ...draft, text: e.target.value })}
          />
        </label>
      </div>
    </Modal>
  );
}
