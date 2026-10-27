import React, { useMemo, useState } from 'react';
import { Download, ExternalLink, FolderOpen, Trash2 } from 'lucide-react';
import '../shared/Muse.css';
import './MuseLibraryPage.css';
import { Button, Icon, RevealList, SegmentedControl } from '../../../components/ui';
import { LIST_REVEAL_STEP } from '../../../constants';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseJobKind } from '../../../types';
import type { MuseLibraryItem } from '../../../types';
import { filterLibrary, libraryTitle, type KindFilter } from './library';
import { useMuseLibrary } from './useMuseLibrary';

const kindFilters: readonly { value: KindFilter; label: string }[] = [
  { value: 'all', label: 'Tất cả' },
  { value: MuseJobKind.Image, label: 'Ảnh' },
  { value: MuseJobKind.Video, label: 'Video' },
];

/**
 * Every file finished Muse jobs produced, newest first. The library is kept
 * apart from the queue, so clearing finished jobs leaves it as it is.
 */
export const MuseLibraryPage: React.FC = () => {
  const library = useMuseLibrary();
  const addToast = useAppStore((s) => s.addToast);
  const [filter, setFilter] = useState<KindFilter>('all');

  const items = useMemo(() => filterLibrary(library.items, filter), [library.items, filter]);

  const act = async (action: () => Promise<void>) => {
    try {
      await action();
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  const forget = (item: MuseLibraryItem) =>
    void act(async () => {
      if (!window.confirm('Bỏ tệp này khỏi Thư viện Muse? File vẫn còn trên đĩa.')) return;
      unwrap(await api.removeMuseLibraryItems([item.id]));
      await library.reload();
    });

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <h1 className="muse-title">Thư viện Muse</h1>
        <Button onClick={() => void act(async () => void unwrap(await api.openMuseOutputFolder()))}>
          <Icon icon={FolderOpen} />
          <span>Mở thư mục lưu</span>
        </Button>
      </div>

      <div className="muse-lib-layout">
        <div className="muse-lib-toolbar">
          <SegmentedControl
            aria-label="Lọc thư viện"
            value={filter}
            onChange={setFilter}
            options={kindFilters}
          />
          <span className="muse-hint">{items.length} tệp</span>
        </div>

        {library.error ? (
          <div className="muse-empty">Không tải được thư viện: {library.error}</div>
        ) : items.length === 0 ? (
          <div className="muse-empty">
            {library.loading ? 'Đang tải thư viện…' : 'Chưa có tác phẩm nào hoàn thành.'}
          </div>
        ) : (
          <RevealList
            className="muse-lib-grid"
            items={items}
            keyOf={(item) => item.id}
            step={LIST_REVEAL_STEP}
            resetKey={filter}
            render={(item) => (
              <div className="muse-lib-item">
                <div className="muse-lib-preview-box">
                  <span className="muse-lib-badge">
                    {item.kind === MuseJobKind.Video ? 'VIDEO' : 'ẢNH'}
                  </span>
                  {item.kind === MuseJobKind.Video ? (
                    <video src={item.localUrl} className="muse-lib-media" controls preload="none" />
                  ) : (
                    <img
                      src={item.localUrl}
                      className="muse-lib-media"
                      alt={libraryTitle(item)}
                      loading="lazy"
                      decoding="async"
                    />
                  )}
                </div>
                <div className="muse-lib-meta">
                  <div className="muse-lib-title" title={libraryTitle(item)}>
                    {libraryTitle(item)}
                  </div>
                  <div className="muse-lib-actions">
                    <Button
                      size="sm"
                      variant="ghost"
                      title="Lưu ra thư mục khác"
                      onClick={() =>
                        void act(async () => {
                          const saved = unwrap(await api.saveMedia(item.localUrl));
                          if (!saved.canceled)
                            addToast({ type: 'success', message: `Đã lưu ${saved.path}` });
                        })
                      }
                    >
                      <Icon icon={Download} size="sm" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      title="Mở thư mục chứa file"
                      onClick={() =>
                        void act(
                          async () =>
                            void unwrap(await api.revealMuseFile(item.savedPath || item.localUrl)),
                        )
                      }
                    >
                      <Icon icon={ExternalLink} size="sm" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      title="Bỏ khỏi thư viện (file vẫn còn trên đĩa)"
                      onClick={() => forget(item)}
                    >
                      <Icon icon={Trash2} size="sm" />
                    </Button>
                  </div>
                </div>
              </div>
            )}
          />
        )}
      </div>
    </div>
  );
};
