import React, { useMemo, useState } from 'react';
import { ExternalLink, FolderOpen, Merge, Tag } from 'lucide-react';
import '../shared/Muse.css';
import './MuseLibraryPage.css';
import { Button, Card, Icon, SegmentedControl } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import type { StudioAsset } from '../../../types';
import { useMuseStore } from '../shared/museStore';
import { useMuseLive } from '../shared/useMuseLive';

interface LibItem {
  id: string;
  url: string;
  name: string;
  kind: 'video' | 'image';
  jobId: string;
  projectId: string;
  prompt: string;
  createdAt: string;
}

export const MuseLibraryPage: React.FC = () => {
  useMuseLive();
  const jobs = useMuseStore((s) => s.jobs);
  const addToast = useAppStore((s) => s.addToast);

  const [filterType, setFilterType] = useState<'all' | 'video' | 'image'>('all');
  const [filterProject, setFilterProject] = useState<string>('all');
  const [selectedUrls, setSelectedUrls] = useState<string[]>([]);
  const [merging, setMerging] = useState(false);
  const [mergeName, setMergeName] = useState('');
  const [showMergeModal, setShowMergeModal] = useState(false);

  // Asset modal state
  const [assetImage, setAssetImage] = useState<string | null>(null);
  const [assetTag, setAssetTag] = useState('');
  const [assetRole, setAssetRole] = useState('character_face');

  // Extract all output files from finished jobs
  const allItems = useMemo<LibItem[]>(() => {
    const items: LibItem[] = [];
    const seen = new Set<string>();

    for (const job of jobs || []) {
      const files = [...(job.files || []), ...(job.savedFiles || [])];
      for (const f of files) {
        if (!f || seen.has(f)) continue;
        seen.add(f);
        const lower = f.toLowerCase();
        const isVid = lower.endsWith('.mp4') || lower.endsWith('.webm') || lower.endsWith('.mov');
        const isImg =
          lower.endsWith('.png') ||
          lower.endsWith('.jpg') ||
          lower.endsWith('.jpeg') ||
          lower.endsWith('.webp');
        if (!isVid && !isImg) continue;

        const name = f.split(/[\\/]/).pop() || f;
        items.push({
          id: `${job.id}-${f}`,
          url: f,
          name,
          kind: isVid ? 'video' : 'image',
          jobId: job.id,
          projectId: job.projectId || 'default',
          prompt: job.prompt || '',
          createdAt: job.createdAt || '',
        });
      }
    }
    // Newest first
    return items.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  }, [jobs]);

  const uniqueProjects = useMemo(() => {
    const p = new Set<string>();
    for (const it of allItems) {
      if (it.projectId) p.add(it.projectId);
    }
    return Array.from(p);
  }, [allItems]);

  const filteredItems = useMemo(() => {
    return allItems.filter((it) => {
      if (filterType === 'video' && it.kind !== 'video') return false;
      if (filterType === 'image' && it.kind !== 'image') return false;
      if (filterProject !== 'all' && it.projectId !== filterProject) return false;
      return true;
    });
  }, [allItems, filterType, filterProject]);

  const handleToggleSelect = (url: string) => {
    setSelectedUrls((prev) =>
      prev.includes(url) ? prev.filter((u) => u !== url) : [...prev, url],
    );
  };

  const handleReveal = async (path: string) => {
    try {
      unwrap(await api.revealMuseFile(path));
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  const handleMergeSelected = async () => {
    if (selectedUrls.length < 2) {
      addToast({ type: 'error', message: 'Vui lòng chọn ít nhất 2 video để ghép' });
      return;
    }
    setMerging(true);
    try {
      const outPath = unwrap(await api.mergeMuseVideos(selectedUrls, mergeName.trim()));
      addToast({ type: 'success', message: `Ghép thành công: ${outPath}` });
      setShowMergeModal(false);
      setSelectedUrls([]);
      setMergeName('');
    } catch (cause) {
      addToast({
        type: 'error',
        message: cause instanceof Error ? cause.message : 'Ghép video thất bại',
      });
    } finally {
      setMerging(false);
    }
  };

  const handleAddToStudio = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!assetImage || !assetTag.trim()) return;
    try {
      const asset: StudioAsset = {
        id: '',
        tag: assetTag.trim(),
        name: assetTag.trim(),
        role: assetRole,
        images: [assetImage],
        projectId: 'default',
        createdAt: '',
      };
      unwrap(await api.addMuseStudioAsset(asset));
      addToast({ type: 'success', message: `Đã thêm ảnh vào tư liệu @${asset.tag}` });
      setAssetImage(null);
      setAssetTag('');
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <div>
          <h1 className="muse-title">Thư viện tác phẩm</h1>
          <p className="muse-subtitle">
            Duyệt toàn bộ ảnh và video đã tạo trên Muse, ghép các video ngắn và thêm ảnh vào kho tư
            liệu.
          </p>
        </div>
        <div className="muse-actions">
          {selectedUrls.length > 0 && (
            <Button variant="primary" onClick={() => setShowMergeModal(true)}>
              <Icon icon={Merge} />
              <span>Ghép {selectedUrls.length} video đã chọn</span>
            </Button>
          )}
          <Button onClick={() => void api.openMuseOutputFolder()}>
            <Icon icon={FolderOpen} />
            <span>Mở thư mục lưu</span>
          </Button>
        </div>
      </div>

      <div className="muse-lib-layout">
        {/* Toolbar Filters */}
        <div className="muse-lib-toolbar">
          <div className="muse-lib-filters">
            <SegmentedControl
              aria-label="Lọc định dạng thư viện"
              value={filterType}
              onChange={(val) => setFilterType(val as 'all' | 'video' | 'image')}
              options={[
                { value: 'all', label: 'Tất cả' },
                { value: 'video', label: 'Chỉ Video' },
                { value: 'image', label: 'Chỉ Ảnh' },
              ]}
            />
            {uniqueProjects.length > 1 && (
              <select
                className="muse-input"
                style={{ padding: '0.375rem 0.75rem', width: 'auto' }}
                value={filterProject}
                onChange={(e) => setFilterProject(e.target.value)}
              >
                <option value="all">Tất cả dự án</option>
                {uniqueProjects.map((p) => (
                  <option key={p} value={p}>
                    Dự án: {p}
                  </option>
                ))}
              </select>
            )}
          </div>
          <div style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)' }}>
            Hiển thị {filteredItems.length} tệp
          </div>
        </div>

        {/* Modal Merge Videos */}
        {showMergeModal && (
          <Card className="muse-panel">
            <div className="muse-form-title">
              <Icon icon={Merge} />
              <span>Ghép {selectedUrls.length} video thành 1 video nối liền</span>
            </div>
            <p className="muse-hint" style={{ marginBottom: '1rem' }}>
              FFmpeg sẽ tự động nối các đoạn video đã chọn theo thứ tự và lưu ra thư mục kết quả.
            </p>
            <label className="muse-field">
              <span>Tên file kết quả (tuỳ chọn)</span>
              <input
                className="muse-input"
                placeholder="Ví dụ: Video_Tong_Hop_01"
                value={mergeName}
                onChange={(e) => setMergeName(e.target.value)}
                autoFocus
              />
            </label>
            <div className="muse-actions">
              <Button variant="primary" isLoading={merging} onClick={handleMergeSelected}>
                Bắt đầu ghép
              </Button>
              <Button variant="ghost" onClick={() => setShowMergeModal(false)}>
                Huỷ
              </Button>
            </div>
          </Card>
        )}

        {/* Modal Add To Studio Asset */}
        {assetImage && (
          <Card className="muse-panel">
            <form className="muse-form" onSubmit={handleAddToStudio}>
              <div className="muse-form-title">
                <Icon icon={Tag} />
                <span>Thêm ảnh vào kho Tư liệu Studio</span>
              </div>
              <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
                <img src={assetImage} alt="preview" className="muse-thumb-preview" />
                <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                  <label className="muse-field">
                    <span>Tag viết tắt (@tag)</span>
                    <input
                      className="muse-input"
                      placeholder="ví dụ: mai, sp_ao"
                      value={assetTag}
                      onChange={(e) => setAssetTag(e.target.value)}
                      autoFocus
                    />
                  </label>
                  <label className="muse-field">
                    <span>Vai trò</span>
                    <select
                      className="muse-input"
                      value={assetRole}
                      onChange={(e) => setAssetRole(e.target.value)}
                    >
                      <option value="character_face">Khuôn mặt (Identity Lock)</option>
                      <option value="character_full">Toàn thân nhân vật (Full Body)</option>
                      <option value="storyboard">Storyboard (Bố cục)</option>
                      <option value="product">Sản phẩm (Product)</option>
                      <option value="other">Tư liệu chung</option>
                    </select>
                  </label>
                </div>
              </div>
              <div className="muse-actions">
                <Button type="submit" variant="primary">
                  Lưu vào Tư liệu
                </Button>
                <Button type="button" variant="ghost" onClick={() => setAssetImage(null)}>
                  Huỷ
                </Button>
              </div>
            </form>
          </Card>
        )}

        {/* Items Grid */}
        {filteredItems.length === 0 ? (
          <div className="muse-empty">Chưa có tác phẩm nào hoàn thành.</div>
        ) : (
          <div className="muse-lib-grid">
            {filteredItems.map((item) => {
              const isSelected = selectedUrls.includes(item.url);
              return (
                <div key={item.id} className={`muse-lib-item ${isSelected ? 'selected' : ''}`}>
                  <div className="muse-lib-preview-box">
                    {item.kind === 'video' && (
                      <input
                        type="checkbox"
                        className="muse-lib-check"
                        title="Chọn để ghép video"
                        checked={isSelected}
                        onChange={() => handleToggleSelect(item.url)}
                      />
                    )}
                    <span className="muse-lib-badge">
                      {item.kind === 'video' ? 'VIDEO' : 'ẢNH'}
                    </span>
                    {item.kind === 'video' ? (
                      <video src={item.url} className="muse-lib-media" preload="metadata" />
                    ) : (
                      <img src={item.url} className="muse-lib-media" alt={item.name} />
                    )}
                  </div>
                  <div className="muse-lib-meta">
                    <div className="muse-lib-title" title={item.name}>
                      {item.name}
                    </div>
                    <div className="muse-lib-actions">
                      <Button
                        size="sm"
                        variant="ghost"
                        title="Mở thư mục chứa file"
                        onClick={() => handleReveal(item.url)}
                      >
                        <Icon icon={ExternalLink} size="sm" />
                      </Button>
                      {item.kind === 'image' && (
                        <Button
                          size="sm"
                          variant="ghost"
                          title="Thêm vào Tư liệu Studio"
                          onClick={() => {
                            setAssetImage(item.url);
                            setAssetTag('');
                          }}
                        >
                          <Icon icon={Tag} size="sm" />
                        </Button>
                      )}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};
