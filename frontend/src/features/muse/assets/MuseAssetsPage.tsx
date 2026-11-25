import { useMemo, useRef, useState } from 'react';
import {
  CheckSquare,
  FolderOpen,
  FolderPlus,
  Plus,
  Search,
  Square,
  Trash2,
  Users,
  X,
} from 'lucide-react';
import '../../../components/batch/BatchForm.css';
import '../../../components/studio/Studio.css';
import '../shared/Muse.css';
import './MuseAssetsPage.css';
import { Button, Icon, confirmAction } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseAsset, MuseAssetType, MuseRefRole } from '../../../types';
import { useMuseStudio } from '../shared/StudioExtras';
import { AssetCard } from './AssetCard';
import { AssetDialog } from './AssetDialog';
import { ASSET_TYPES, defaultTypeForRole, roleOrder, roles } from './assetRoles';
import { BatchImportAssetsDialog } from './BatchImportAssetsDialog';
import { TemplateSection } from './TemplateSection';

const emptyAsset = (defaultProject = ''): MuseAsset =>
  MuseAsset.createFrom({
    id: '',
    project: defaultProject,
    type: MuseAssetType.Char,
    tag: '',
    name: '',
    role: MuseRefRole.Character,
    lock: '',
    images: [],
    items: [],
    createdAt: '',
  });

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
  const [search, setSearch] = useState('');
  const [projectFilter, setProjectFilter] = useState<'all' | '_chung' | string>('all');
  const [typeFilter, setTypeFilter] = useState<'all' | MuseAssetType>('all');
  const [roleFilter, setRoleFilter] = useState<'all' | MuseRefRole>('all');
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(() => new Set());
  const [bulkRole, setBulkRole] = useState<MuseRefRole>(MuseRefRole.Character);
  const lastClickedId = useRef<string | null>(null);

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

  const projects = useMemo(
    () =>
      Array.from(
        new Set(assets.map((a) => (a.project ?? '').trim()).filter((p) => p !== '')),
      ).sort(),
    [assets],
  );

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
      const proj = (a.project ?? '').trim();
      if (projectFilter === '_chung' && proj !== '') return false;
      if (projectFilter !== 'all' && projectFilter !== '_chung' && proj !== projectFilter) {
        return false;
      }
      const effType = a.type || defaultTypeForRole(a.role);
      if (typeFilter !== 'all' && effType !== typeFilter) return false;
      if (roleFilter !== 'all' && a.role !== roleFilter) return false;
      if (!q) return true;
      return (
        a.tag.toLowerCase().includes(q) ||
        a.name.toLowerCase().includes(q) ||
        proj.toLowerCase().includes(q) ||
        (a.lock ?? '').toLowerCase().includes(q)
      );
    });
  }, [assets, projectFilter, typeFilter, roleFilter, search]);

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
    void act(async () => {
      if (
        !(await confirmAction({
          title: 'Xoá tư liệu đã chọn?',
          message: `Xoá ${liveSelectedIds.length} tư liệu đã chọn? Ảnh không còn tác vụ nào dùng sẽ được xoá khỏi máy ngay.`,
          confirmLabel: 'Xoá',
          danger: true,
        }))
      ) {
        return;
      }
      const n = unwrap(await api.removeMuseAssets(liveSelectedIds));
      setSelectedIds(new Set());
      addToast({ type: 'success', message: `Đã xoá ${n} tư liệu.` });
    });
  };

  const removeOne = (asset: MuseAsset) => {
    void act(async () => {
      if (
        !(await confirmAction({
          title: `Xoá @${asset.tag}?`,
          message: `Xoá @${asset.tag}? Ảnh không còn tác vụ nào dùng sẽ được xoá khỏi Kho ngay.`,
          confirmLabel: 'Xoá',
          danger: true,
        }))
      )
        return;
      unwrap(await api.removeMuseAssets([asset.id]));
      setSelectedIds((prev) => {
        if (!prev.has(asset.id)) return prev;
        const next = new Set(prev);
        next.delete(asset.id);
        return next;
      });
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
          <div className="muse-assets-title-row">
            <h1 className="muse-title">Kho Tư liệu Muse</h1>
            <span className="muse-assets-count">
              {assets.length} tư liệu · {templates.length} mẫu
            </span>
          </div>
          <p className="muse-assets-lead">
            Gọi trong prompt bằng <code>@tag</code> hoặc <code>@tag_góc_trạng_thái</code> (mặc định
            lấy ảnh chính). File gốc lưu nguyên vẹn trong Kho và tự đổi tên theo chuẩn.
          </p>
        </div>
        <div className="muse-assets-header-actions">
          <Button
            variant="ghost"
            onClick={() =>
              void api
                .openMuseAssetVaultFolder()
                .then((res) => {
                  if (!res.success)
                    addToast({ type: 'error', message: res.error || 'Lỗi mở thư mục' });
                })
                .catch(() => {})
            }
          >
            <Icon icon={FolderOpen} />
            <span>Mở kho ảnh</span>
          </Button>
          <Button variant="secondary" onClick={() => setBatchOpen(true)}>
            <Icon icon={FolderPlus} />
            <span>Nhập hàng loạt</span>
          </Button>
          <Button
            variant="primary"
            onClick={() =>
              setEditing(
                emptyAsset(
                  projectFilter !== 'all' && projectFilter !== '_chung' ? projectFilter : '',
                ),
              )
            }
          >
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
                  void act(async () => {
                    if (
                      !(await confirmAction({
                        title: 'Xoá tất cả tư liệu?',
                        message: `Xoá tất cả ${assets.length} tư liệu? Ảnh không còn tác vụ nào dùng sẽ được xoá khỏi máy ngay.`,
                        confirmLabel: 'Xoá tất cả',
                        danger: true,
                      }))
                    )
                      return;
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
          <div className="muse-assets-controls">
            {projects.length > 0 && (
              <div className="muse-vault-project-tabs" role="tablist" aria-label="Lọc theo dự án">
                <button
                  type="button"
                  className={`muse-vault-tab${projectFilter === 'all' ? ' is-on' : ''}`}
                  onClick={() => setProjectFilter('all')}
                >
                  Tất cả ({assets.length})
                </button>
                <button
                  type="button"
                  className={`muse-vault-tab${projectFilter === '_chung' ? ' is-on' : ''}`}
                  onClick={() => setProjectFilter('_chung')}
                >
                  _chung ({assets.filter((a) => !(a.project ?? '').trim()).length})
                </button>
                {projects.map((proj) => (
                  <button
                    key={proj}
                    type="button"
                    className={`muse-vault-tab${projectFilter === proj ? ' is-on' : ''}`}
                    onClick={() => setProjectFilter(proj)}
                  >
                    {proj} ({assets.filter((a) => (a.project ?? '').trim() === proj).length})
                  </button>
                ))}
              </div>
            )}
            <div className="muse-assets-toolbar">
              <div className="muse-assets-search">
                <Icon icon={Search} size="sm" />
                <input
                  type="text"
                  value={search}
                  placeholder="Tìm theo dự án, @tag, tên hoặc prompt khoá…"
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
                className="batch-input batch-select muse-assets-type-filter"
                aria-label="Lọc theo loại"
                value={typeFilter}
                onChange={(e) => setTypeFilter(e.target.value as 'all' | MuseAssetType)}
              >
                <option value="all">Tất cả loại</option>
                {ASSET_TYPES.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
              </select>
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
          <div className="muse-assets-empty">
            <Icon icon={Users} size="lg" />
            <strong>Chưa có tư liệu</strong>
            <span>
              Thêm từng tư liệu hoặc dùng Thêm nhanh từ thư mục để nhập hàng loạt ảnh và 3 file
              .txt.
            </span>
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
          </div>
        ) : filtered.length === 0 ? (
          <div className="muse-assets-empty">
            <Icon icon={Search} size="lg" />
            <strong>Không thấy tư liệu phù hợp</strong>
            <span>Thử xoá từ khoá tìm kiếm hoặc đổi bộ lọc dự án / loại / vai trò.</span>
            <Button
              variant="secondary"
              onClick={() => {
                setSearch('');
                setProjectFilter('all');
                setTypeFilter('all');
                setRoleFilter('all');
              }}
            >
              Hiện tất cả tư liệu
            </Button>
          </div>
        ) : (
          <div className="muse-assets-grid">
            {filtered.map((a) => (
              <AssetCard
                key={a.id}
                asset={a}
                selected={selectedIds.has(a.id)}
                hasSelection={liveSelectedIds.length > 0}
                onToggleSelect={toggleSelect}
                onEdit={setEditing}
                onDelete={removeOne}
              />
            ))}
          </div>
        )}
      </section>

      <TemplateSection
        templates={templates}
        onSave={(t) => act(async () => void unwrap(await api.saveMuseTemplate(t)))}
        onRemove={(id) => act(async () => void unwrap(await api.removeMuseTemplate(id)))}
      />

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
    </div>
  );
}
