import React, { useEffect, useState } from 'react';
import {
  FolderKanban,
  Image,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Tag,
  Trash2,
  UserCheck,
} from 'lucide-react';
import '../shared/Muse.css';
import './MuseStudioPage.css';
import { Button, Card, Icon } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import type { StudioAsset, StudioData } from '../../../types';

const ROLE_LABELS: Record<string, string> = {
  character_face: 'Khuôn mặt',
  character_full: 'Toàn thân',
  storyboard: 'Storyboard',
  product: 'Sản phẩm',
  other: 'Khác',
};

export const MuseStudioPage: React.FC = () => {
  const [studio, setStudio] = useState<StudioData | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const addToast = useAppStore((s) => s.addToast);

  // Modal / Form state for asset
  const [showAssetForm, setShowAssetForm] = useState(false);
  const [newTag, setNewTag] = useState('');
  const [newName, setNewName] = useState('');
  const [newRole, setNewRole] = useState('character_face');
  const [newLockNotes, setNewLockNotes] = useState('');
  const [newImages, setNewImages] = useState<string[]>([]);

  // Project state
  const [newProjectName, setNewProjectName] = useState('');
  const [showProjectModal, setShowProjectModal] = useState(false);

  const normalizeStudio = (data: StudioData): StudioData => ({
    ...data,
    projects: Array.isArray(data.projects) ? data.projects : [],
    assets: Array.isArray(data.assets) ? data.assets : [],
    modelConfig: {
      on: !!data.modelConfig?.on,
      images: Array.isArray(data.modelConfig?.images) ? data.modelConfig.images : [],
    },
    rotateConfig: {
      on: !!data.rotateConfig?.on,
      images: Array.isArray(data.rotateConfig?.images) ? data.rotateConfig.images : [],
      index: data.rotateConfig?.index ?? 0,
    },
    promptTemplates: Array.isArray(data.promptTemplates) ? data.promptTemplates : [],
  });

  const loadStudio = async () => {
    try {
      const data = unwrap(await api.getMuseStudio());
      if (data) setStudio(normalizeStudio(data));
    } catch (cause) {
      addToast({
        type: 'error',
        message: cause instanceof Error ? cause.message : 'Không tải được dữ liệu Studio',
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    let active = true;
    api
      .getMuseStudio()
      .then((res) => {
        if (active && res.data) setStudio(normalizeStudio(res.data));
      })
      .catch((cause) => {
        if (active) {
          addToast({
            type: 'error',
            message: cause instanceof Error ? cause.message : 'Không tải được dữ liệu Studio',
          });
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [addToast]);

  const run = async (key: string, action: () => Promise<void>) => {
    if (busy) return;
    setBusy(key);
    try {
      await action();
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      setBusy(null);
    }
  };

  const handlePickAssetImages = async () => {
    try {
      const refs = unwrap(await api.pickMuseReferences());
      const urls = refs.map((r) => r.localUrl);
      setNewImages((prev) => [...prev, ...urls].slice(0, 3));
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  const handleSaveAsset = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newTag.trim()) {
      addToast({ type: 'error', message: 'Vui lòng nhập tag định danh (ví dụ: mai, sp_ao)' });
      return;
    }
    if (newImages.length === 0) {
      addToast({ type: 'error', message: 'Vui lòng chọn ít nhất 1 ảnh tư liệu' });
      return;
    }
    void run('save-asset', async () => {
      const asset: StudioAsset = {
        id: '',
        tag: newTag.trim(),
        name: newName.trim() || newTag.trim(),
        role: newRole,
        lockNotes: newLockNotes.trim(),
        images: newImages,
        projectId: studio?.activeProject || 'default',
        createdAt: '',
      };
      await api.addMuseStudioAsset(asset);
      addToast({ type: 'success', message: `Đã thêm tư liệu @${asset.tag}` });
      setShowAssetForm(false);
      setNewTag('');
      setNewName('');
      setNewLockNotes('');
      setNewImages([]);
      await loadStudio();
    });
  };

  const handleDeleteAsset = (id: string, tag: string) => {
    if (!window.confirm(`Xoá tư liệu @${tag}?`)) return;
    void run(`delete-${id}`, async () => {
      unwrap(await api.removeMuseStudioAsset(id));
      addToast({ type: 'info', message: `Đã xoá tư liệu @${tag}` });
      await loadStudio();
    });
  };

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newProjectName.trim()) return;
    void run('create-proj', async () => {
      const proj = unwrap(await api.createMuseStudioProject(newProjectName.trim()));
      addToast({ type: 'success', message: `Đã tạo dự án "${proj.name}"` });
      setNewProjectName('');
      setShowProjectModal(false);
      await loadStudio();
    });
  };

  const handleToggleProjectPaused = async (id: string, current: boolean) => {
    void run(`pause-${id}`, async () => {
      unwrap(await api.setMuseProjectPaused(id, !current));
      addToast({
        type: 'info',
        message: !current ? 'Đã tạm dừng hàng đợi của dự án' : 'Đã tiếp tục hàng đợi dự án',
      });
      await loadStudio();
    });
  };

  const handleSelectActiveProject = async (id: string) => {
    void run('set-active-proj', async () => {
      unwrap(await api.setMuseActiveProject(id));
      await loadStudio();
    });
  };

  const handlePickModelImages = async () => {
    if (!studio) return;
    try {
      const refs = unwrap(await api.pickMuseReferences());
      const urls = refs.map((r) => r.localUrl).slice(0, 3);
      const updated = { ...studio, modelConfig: { ...studio.modelConfig, images: urls } };
      unwrap(await api.saveMuseStudio(updated));
      setStudio(updated);
      addToast({ type: 'success', message: 'Đã cập nhật ảnh người mẫu cố định' });
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  const handleToggleModel = async () => {
    if (!studio) return;
    const updated = {
      ...studio,
      modelConfig: { ...studio.modelConfig, on: !studio.modelConfig.on },
    };
    unwrap(await api.saveMuseStudio(updated));
    setStudio(updated);
  };

  const handlePickRotateImages = async () => {
    if (!studio) return;
    try {
      const refs = unwrap(await api.pickMuseReferences());
      const urls = refs.map((r) => r.localUrl).slice(0, 20);
      const updated = { ...studio, rotateConfig: { ...studio.rotateConfig, images: urls } };
      unwrap(await api.saveMuseStudio(updated));
      setStudio(updated);
      addToast({ type: 'success', message: 'Đã cập nhật danh sách ảnh luân phiên' });
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  const handleToggleRotate = async () => {
    if (!studio) return;
    const updated = {
      ...studio,
      rotateConfig: { ...studio.rotateConfig, on: !studio.rotateConfig.on },
    };
    unwrap(await api.saveMuseStudio(updated));
    setStudio(updated);
  };

  if (loading || !studio) {
    return (
      <div className="page-shell muse-page">
        <div className="muse-loading">Đang tải Studio & Tư liệu...</div>
      </div>
    );
  }

  const activeProj =
    studio.projects.find((p) => p.id === studio.activeProject) || studio.projects[0];

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <div>
          <h1 className="muse-title">Studio & Tư liệu</h1>
          <p className="muse-subtitle">
            Quản lý dự án, tư liệu nhân vật/sản phẩm gắn @tag, người mẫu cố định và ảnh luân phiên.
          </p>
        </div>
      </div>

      <div className="muse-studio-layout">
        {/* Project Selector Bar */}
        <div className="muse-project-bar">
          <div className="muse-project-select">
            <Icon icon={FolderKanban} />
            <span>Dự án đang làm việc:</span>
            <select
              value={studio.activeProject}
              onChange={(e) => handleSelectActiveProject(e.target.value)}
            >
              {studio.projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} {p.paused ? '(Đang tạm dừng)' : ''}
                </option>
              ))}
            </select>
            {activeProj && (
              <Button
                size="sm"
                variant={activeProj.paused ? 'primary' : 'ghost'}
                onClick={() => handleToggleProjectPaused(activeProj.id, activeProj.paused)}
              >
                <Icon icon={activeProj.paused ? Play : Pause} size="sm" />
                <span>{activeProj.paused ? 'Tiếp tục dự án' : 'Tạm dừng dự án'}</span>
              </Button>
            )}
          </div>
          <Button size="sm" onClick={() => setShowProjectModal(true)}>
            <Icon icon={Plus} size="sm" />
            <span>Tạo dự án mới</span>
          </Button>
        </div>

        {showProjectModal && (
          <Card className="muse-panel">
            <form className="muse-form" onSubmit={handleCreateProject}>
              <div className="muse-form-title">Tạo dự án mới</div>
              <label className="muse-field">
                <span>Tên dự án</span>
                <input
                  className="muse-input"
                  placeholder="Ví dụ: Chiến dịch Tết 2027"
                  value={newProjectName}
                  onChange={(e) => setNewProjectName(e.target.value)}
                  autoFocus
                />
              </label>
              <div className="muse-actions">
                <Button type="submit" variant="primary">
                  Tạo
                </Button>
                <Button type="button" variant="ghost" onClick={() => setShowProjectModal(false)}>
                  Huỷ
                </Button>
              </div>
            </form>
          </Card>
        )}

        {/* Tagged Assets Section */}
        <div>
          <div className="muse-studio-section-title">
            <h2>
              <Icon icon={Tag} />
              <span>Tư liệu ảnh gắn @tag ({studio.assets?.length ?? 0})</span>
            </h2>
            <Button
              variant="primary"
              size="sm"
              onClick={() => setShowAssetForm(!showAssetForm)}
              disabled={!!busy}
            >
              <Icon icon={Plus} size="sm" />
              <span>Thêm tư liệu mới</span>
            </Button>
          </div>

          {showAssetForm && (
            <Card className="muse-panel" style={{ marginBottom: '1.25rem' }}>
              <form className="muse-form" onSubmit={handleSaveAsset}>
                <div className="muse-form-title">Thêm tư liệu ảnh gắn @tag</div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                  <label className="muse-field">
                    <span>
                      Tag viết tắt (dùng <code>@tag</code> trong prompt)
                    </span>
                    <input
                      className="muse-input"
                      placeholder="ví dụ: mai, linh, ao_polo"
                      value={newTag}
                      onChange={(e) => setNewTag(e.target.value)}
                      autoFocus
                    />
                  </label>
                  <label className="muse-field">
                    <span>Tên hiển thị gợi nhớ</span>
                    <input
                      className="muse-input"
                      placeholder="ví dụ: Nhân vật Mai - Áo dài"
                      value={newName}
                      onChange={(e) => setNewName(e.target.value)}
                    />
                  </label>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 2fr', gap: '1rem' }}>
                  <label className="muse-field">
                    <span>Vai trò tư liệu</span>
                    <select
                      className="muse-input"
                      value={newRole}
                      onChange={(e) => setNewRole(e.target.value)}
                    >
                      <option value="character_face">Khuôn mặt (Identity Lock)</option>
                      <option value="character_full">Toàn thân nhân vật (Outfit & Body)</option>
                      <option value="storyboard">Storyboard (Góc quay & Bố cục)</option>
                      <option value="product">Sản phẩm (Chi tiết & Màu sắc)</option>
                      <option value="other">Tư liệu chung</option>
                    </select>
                  </label>
                  <label className="muse-field">
                    <span>Ghi chú cố định (Lock notes - giữ chính xác)</span>
                    <input
                      className="muse-input"
                      placeholder="ví dụ: nốt ruồi dưới mắt trái, áo sơ mi xanh thêu hoa"
                      value={newLockNotes}
                      onChange={(e) => setNewLockNotes(e.target.value)}
                    />
                  </label>
                </div>

                <div className="muse-field">
                  <span>Ảnh đính kèm (tối đa 3 ảnh):</span>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                    <Button type="button" size="sm" onClick={handlePickAssetImages}>
                      <Icon icon={Image} size="sm" />
                      <span>Chọn ảnh từ máy ({newImages.length}/3)</span>
                    </Button>
                    {newImages.length > 0 && (
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() => setNewImages([])}
                      >
                        Xoá hết ảnh đã chọn
                      </Button>
                    )}
                  </div>
                  <div className="muse-asset-images" style={{ marginTop: '0.5rem' }}>
                    {newImages.map((src, i) => (
                      <img key={i} src={src} className="muse-asset-thumb" alt="preview" />
                    ))}
                  </div>
                </div>

                <div className="muse-actions">
                  <Button type="submit" variant="primary" isLoading={busy === 'save-asset'}>
                    Lưu tư liệu
                  </Button>
                  <Button type="button" variant="ghost" onClick={() => setShowAssetForm(false)}>
                    Huỷ
                  </Button>
                </div>
              </form>
            </Card>
          )}

          {(studio.assets?.length ?? 0) === 0 ? (
            <div className="muse-empty">
              Chưa có tư liệu gắn tag nào. Bấm "Thêm tư liệu mới" để tạo nhân vật hoặc sản phẩm.
            </div>
          ) : (
            <div className="muse-assets-grid">
              {(studio.assets ?? []).map((asset) => (
                <div key={asset.id} className="muse-asset-card">
                  <div className="muse-asset-card-head">
                    <div>
                      <span className="muse-asset-tag">@{asset.tag}</span>
                      <div style={{ fontWeight: 600, marginTop: '0.25rem' }}>{asset.name}</div>
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <span className="muse-asset-role">
                        {ROLE_LABELS[asset.role] || asset.role}
                      </span>
                      <Button
                        size="sm"
                        variant="ghost"
                        title="Xoá tư liệu"
                        onClick={() => handleDeleteAsset(asset.id, asset.tag)}
                      >
                        <Icon icon={Trash2} size="sm" />
                      </Button>
                    </div>
                  </div>
                  {asset.lockNotes && (
                    <div className="muse-lock-note">
                      <strong>Lock:</strong> {asset.lockNotes}
                    </div>
                  )}
                  <div className="muse-asset-images">
                    {asset.images.map((src, idx) => (
                      <img key={idx} src={src} className="muse-asset-thumb" alt={asset.tag} />
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Model & Rotate Config Section */}
        <div className="muse-config-grid">
          <Card className="muse-panel">
            <div className="muse-form-title">
              <Icon icon={UserCheck} />
              <span>Người mẫu cố định</span>
            </div>
            <p className="muse-hint" style={{ marginBottom: '0.75rem' }}>
              Khi bật, ảnh người mẫu sẽ tự động được gửi kèm đầu tiên ở mọi prompt.
            </p>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '1rem',
              }}
            >
              <Button
                size="sm"
                variant={studio.modelConfig?.on ? 'primary' : 'ghost'}
                onClick={handleToggleModel}
              >
                <span>{studio.modelConfig?.on ? 'Đang BẬT' : 'Đang TẮT'}</span>
              </Button>
              <Button size="sm" onClick={handlePickModelImages}>
                <Icon icon={Image} size="sm" />
                <span>Chọn ảnh ({studio.modelConfig?.images?.length ?? 0}/3)</span>
              </Button>
            </div>
            <div className="muse-asset-images">
              {(studio.modelConfig?.images ?? []).map((src, i) => (
                <img key={i} src={src} className="muse-asset-thumb" alt="model" />
              ))}
            </div>
          </Card>

          <Card className="muse-panel">
            <div className="muse-form-title">
              <Icon icon={RefreshCw} />
              <span>Ảnh luân phiên theo lô</span>
            </div>
            <p className="muse-hint" style={{ marginBottom: '0.75rem' }}>
              Khi bật, mỗi prompt trong lô sẽ lần lượt lấy 1 ảnh từ danh sách xoay vòng.
            </p>
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                marginBottom: '1rem',
              }}
            >
              <Button
                size="sm"
                variant={studio.rotateConfig?.on ? 'primary' : 'ghost'}
                onClick={handleToggleRotate}
              >
                <span>{studio.rotateConfig?.on ? 'Đang BẬT' : 'Đang TẮT'}</span>
              </Button>
              <Button size="sm" onClick={handlePickRotateImages}>
                <Icon icon={Image} size="sm" />
                <span>Chọn ảnh ({studio.rotateConfig?.images?.length ?? 0}/20)</span>
              </Button>
            </div>
            <div className="muse-asset-images">
              {(studio.rotateConfig?.images ?? []).map((src, i) => (
                <img key={i} src={src} className="muse-asset-thumb" alt="rotate" />
              ))}
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
};
