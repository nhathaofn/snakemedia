import { thumbUrl } from '../../../services';
import { useMemo, useRef, useState } from 'react';
import {
  CheckSquare,
  FileText,
  FolderPlus,
  ImagePlus,
  Pencil,
  Plus,
  Search,
  Square,
  Trash2,
  Users,
  X,
} from 'lucide-react';
import '../shared/Muse.css';
import './MuseAssetsPage.css';
import { Button, Icon, IconAction, Modal } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseAsset, MusePromptTemplate, MuseRefRole } from '../../../types';
import { useMuseStore } from '../shared/museStore';
import { useMuseStudio } from '../shared/StudioExtras';
import { roleOrder, roles } from './assetRoles';
import { BatchImportAssetsDialog } from './BatchImportAssetsDialog';

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
  const [batchOpen, setBatchOpen] = useState(false);
  const [template, setTemplate] = useState<MusePromptTemplate | null>(null);
  const [search, setSearch] = useState('');
  const [roleFilter, setRoleFilter] = useState<'all' | MuseRefRole>('all');
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(() => new Set());
  const [bulkRole, setBulkRole] = useState<MuseRefRole>(MuseRefRole.Character);
  const lastClickedId = useRef<string | null>(null);
  const shiftHeld = useRef(false);

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

  const roleCounts = useMemo(() => {
    const counts = new Map<MuseRefRole, number>();
    for (const a of assets) {
      counts.set(a.role, (counts.get(a.role) ?? 0) + 1);
    }
    return counts;
  }, [assets]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase().replace(/^@/, '');
    return assets.filter((a) => {
      if (roleFilter !== 'all' && a.role !== roleFilter) return false;
      if (!q) return true;
      return (
        a.tag.toLowerCase().includes(q) ||
        a.name.toLowerCase().includes(q) ||
        (a.lock ?? '').toLowerCase().includes(q)
      );
    });
  }, [assets, roleFilter, search]);

  const liveSelectedIds = useMemo(() => {
    const existing = new Set(assets.map((a) => a.id));
    return Array.from(selectedIds).filter((id) => existing.has(id));
  }, [assets, selectedIds]);

  const allShownSelected = filtered.length > 0 && filtered.every((a) => selectedIds.has(a.id));

  const toggleSelect = (id: string, shiftKey: boolean) => {
    const currentlyOn = selectedIds.has(id);
    const nextOn = !currentlyOn;
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (shiftKey && lastClickedId.current) {
        const idxA = filtered.findIndex((a) => a.id === lastClickedId.current);
        const idxB = filtered.findIndex((a) => a.id === id);
        if (idxA >= 0 && idxB >= 0) {
          const from = Math.min(idxA, idxB);
          const to = Math.max(idxA, idxB);
          for (let i = from; i <= to; i++) {
            if (nextOn) next.add(filtered[i].id);
            else next.delete(filtered[i].id);
          }
          return next;
        }
      }
      if (nextOn) next.add(id);
      else next.delete(id);
      return next;
    });
    lastClickedId.current = id;
  };

  const toggleSelectShown = () => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (allShownSelected) {
        for (const a of filtered) next.delete(a.id);
      } else {
        for (const a of filtered) next.add(a.id);
      }
      return next;
    });
  };

  const removeSelected = () => {
    if (liveSelectedIds.length === 0) return;
    if (
      !window.confirm(
        `Xoá ${liveSelectedIds.length} tư liệu đã chọn? Ảnh không còn tác vụ nào dùng sẽ được xoá khỏi máy ngay.`,
      )
    ) {
      return;
    }
    void act(async () => {
      const n = unwrap(await api.removeMuseAssets(liveSelectedIds));
      setSelectedIds(new Set());
      addToast({ type: 'success', message: `Đã xoá ${n} tư liệu.` });
    });
  };

  const applyBulkRole = () => {
    if (liveSelectedIds.length === 0) return;
    void act(async () => {
      const n = unwrap(await api.updateMuseAssetsRole(liveSelectedIds, bulkRole));
      addToast({
        type: 'success',
        message: `Đã đổi vai trò sang "${roles[bulkRole].label}" cho ${n} tư liệu.`,
      });
    });
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
        <div className="muse-assets-header-actions">
          <Button variant="secondary" onClick={() => setBatchOpen(true)}>
            <Icon icon={FolderPlus} />
            <span>Thêm nhanh từ thư mục</span>
          </Button>
          <Button variant="primary" onClick={() => setEditing(emptyAsset())}>
            <Icon icon={Plus} />
            <span>Thêm tư liệu</span>
          </Button>
        </div>
      </header>

      {loadError && (
        <div role="alert" className="muse-alert muse-alert-danger">
          {loadError}
        </div>
      )}

      <section className="muse-assets-section">
        <div className="muse-header">
          <h2 className="muse-assets-section-title">
            Nhân vật, sản phẩm, đạo cụ, bối cảnh{' '}
            <span className="muse-assets-count">
              {filtered.length !== assets.length
                ? `${filtered.length}/${assets.length}`
                : assets.length}
            </span>
          </h2>
          {assets.length > 0 && (
            <div className="muse-assets-section-actions">
              <Button
                variant="secondary"
                size="sm"
                disabled={filtered.length === 0}
                onClick={toggleSelectShown}
              >
                <Icon icon={allShownSelected ? Square : CheckSquare} size="sm" />
                <span>
                  {allShownSelected ? 'Bỏ chọn đang lọc' : `Chọn tất cả (${filtered.length})`}
                </span>
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  if (
                    !window.confirm(
                      `Xoá tất cả ${assets.length} tư liệu? Ảnh không còn tác vụ nào dùng sẽ được xoá khỏi máy ngay.`,
                    )
                  )
                    return;
                  void act(async () => {
                    const n = unwrap(await api.removeMuseAssets(assets.map((a) => a.id)));
                    setSelectedIds(new Set());
                    addToast({ type: 'success', message: `Đã xoá ${n} tư liệu.` });
                  });
                }}
              >
                <Icon icon={Trash2} size="sm" />
                <span>Xoá tất cả</span>
              </Button>
            </div>
          )}
        </div>

        {assets.length > 0 && (
          <div className="muse-assets-toolbar">
            <div className="muse-assets-search">
              <Icon icon={Search} size="sm" />
              <input
                type="text"
                value={search}
                placeholder="Tìm theo @tag, tên hoặc prompt khoá…"
                aria-label="Tìm kiếm tư liệu"
                onChange={(e) => setSearch(e.target.value)}
              />
              {search && (
                <button
                  type="button"
                  className="muse-assets-search-clear"
                  aria-label="Xoá tìm kiếm"
                  onClick={() => setSearch('')}
                >
                  <Icon icon={X} size="sm" />
                </button>
              )}
            </div>
            <select
              className="batch-input batch-select muse-assets-role-filter"
              aria-label="Lọc theo vai trò"
              value={roleFilter}
              onChange={(e) => setRoleFilter(e.target.value as 'all' | MuseRefRole)}
            >
              <option value="all">Tất cả vai trò ({assets.length})</option>
              {roleOrder.map((r) => (
                <option key={r} value={r}>
                  {roles[r].label} ({roleCounts.get(r) ?? 0})
                </option>
              ))}
            </select>
          </div>
        )}

        {liveSelectedIds.length > 0 && (
          <div className="muse-assets-bulk-bar" role="region" aria-label="Thao tác hàng loạt">
            <div className="muse-assets-bulk-info">
              <strong>Đang chọn {liveSelectedIds.length} tư liệu</strong>
              <span>Giữ Shift khi tick để chọn nhanh một dải</span>
            </div>
            <div className="muse-assets-bulk-actions">
              <select
                className="batch-input batch-select"
                aria-label="Vai trò mới cho mục đã chọn"
                value={bulkRole}
                onChange={(e) => setBulkRole(e.target.value as MuseRefRole)}
              >
                {roleOrder.map((r) => (
                  <option key={r} value={r}>
                    {roles[r].label}
                  </option>
                ))}
              </select>
              <Button size="sm" variant="secondary" onClick={applyBulkRole}>
                Đổi vai trò
              </Button>
              <Button size="sm" variant="danger" onClick={removeSelected}>
                <Icon icon={Trash2} size="sm" />
                <span>Xoá ({liveSelectedIds.length})</span>
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setSelectedIds(new Set())}>
                Bỏ chọn
              </Button>
            </div>
          </div>
        )}

        {assets.length === 0 ? (
          <EmptyState
            icon={Users}
            title="Chưa có tư liệu"
            text="Thêm từng tư liệu hoặc dùng Thêm nhanh từ thư mục để nhập hàng loạt ảnh và 3 file .txt."
            action={
              <div className="muse-assets-header-actions">
                <Button variant="secondary" onClick={() => setBatchOpen(true)}>
                  <Icon icon={FolderPlus} />
                  <span>Thêm nhanh từ thư mục</span>
                </Button>
                <Button variant="primary" onClick={() => setEditing(emptyAsset())}>
                  <Icon icon={Plus} />
                  <span>Thêm tư liệu đầu tiên</span>
                </Button>
              </div>
            }
          />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Search}
            title="Không thấy tư liệu phù hợp"
            text="Thử xoá từ khoá tìm kiếm hoặc đổi bộ lọc vai trò."
            action={
              <Button
                variant="secondary"
                onClick={() => {
                  setSearch('');
                  setRoleFilter('all');
                }}
              >
                Hiện tất cả tư liệu
              </Button>
            }
          />
        ) : (
          <div className="muse-assets-grid">
            {filtered.map((a) => {
              const isSelected = selectedIds.has(a.id);
              return (
                <article
                  key={a.id}
                  className={`muse-asset-card${isSelected ? ' is-selected' : ''}`}
                >
                  <div
                    className={`muse-asset-cover is-${Math.min(a.images.length, 3)}`}
                    onClick={(e) => {
                      if (liveSelectedIds.length > 0 || e.shiftKey || e.ctrlKey || e.metaKey) {
                        toggleSelect(a.id, e.shiftKey);
                      }
                    }}
                  >
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
                    <label
                      className={`muse-asset-check${isSelected ? ' is-on' : ''}`}
                      onClick={(e) => e.stopPropagation()}
                    >
                      <input
                        type="checkbox"
                        checked={isSelected}
                        aria-label={`Chọn @${a.tag}`}
                        onMouseDown={(e) => {
                          shiftHeld.current = e.shiftKey;
                        }}
                        onKeyDown={(e) => {
                          shiftHeld.current = e.shiftKey;
                        }}
                        onChange={() => {
                          const shift = shiftHeld.current;
                          shiftHeld.current = false;
                          toggleSelect(a.id, shift);
                        }}
                      />
                    </label>
                  </div>
                  <div className="muse-asset-body">
                    <div className="muse-asset-name">
                      <strong>@{a.tag}</strong>
                      {a.name && <span>{a.name}</span>}
                    </div>
                    {a.lock && (
                      <p className="muse-asset-lock" title={a.lock}>
                        Khoá: {a.lock}
                      </p>
                    )}
                  </div>
                  <footer className="muse-asset-footer">
                    <IconAction icon={Pencil} label="Sửa" onClick={() => setEditing(a)} />
                    <IconAction
                      icon={Trash2}
                      label="Xoá"
                      onClick={() => {
                        if (
                          !window.confirm(
                            `Xoá @${a.tag}? Ảnh không còn tác vụ nào dùng sẽ được xoá khỏi máy ngay.`,
                          )
                        )
                          return;
                        void act(async () => {
                          unwrap(await api.removeMuseAssets([a.id]));
                          setSelectedIds((prev) => {
                            if (!prev.has(a.id)) return prev;
                            const next = new Set(prev);
                            next.delete(a.id);
                            return next;
                          });
                        });
                      }}
                    />
                  </footer>
                </article>
              );
            })}
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

      {batchOpen && (
        <BatchImportAssetsDialog
          existingAssets={assets}
          onImported={load}
          onClose={() => setBatchOpen(false)}
        />
      )}
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
