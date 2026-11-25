import React, { useMemo, useState } from 'react';
import {
  BookmarkPlus,
  Download,
  ExternalLink,
  FolderOpen,
  Play,
  Search,
  Trash2,
  X,
} from 'lucide-react';
import '../shared/Muse.css';
import './MuseLibraryPage.css';
import {
  Button,
  Icon,
  IconAction,
  RevealList,
  SegmentedControl,
  confirmAction,
} from '../../../components/ui';
import { LIST_REVEAL_STEP } from '../../../constants';
import { api, thumbUrl, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseJobKind } from '../../../types';
import type { MuseLibraryItem } from '../../../types';
import { SaveToVaultDialog, type VaultImportSource } from '../assets/SaveToVaultDialog';
import { MuseMediaLightbox, type MuseLightboxItem } from '../shared/MuseMediaLightbox';
import {
  filterAndSortLibrary,
  formatLibraryDate,
  libraryTitle,
  type KindFilter,
  type LibrarySort,
} from './library';
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
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<LibrarySort>('newest');
  const [vaultSources, setVaultSources] = useState<VaultImportSource[] | null>(null);
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);

  const items = useMemo(
    () => filterAndSortLibrary(library.items, filter, search, sort),
    [library.items, filter, search, sort],
  );
  const imageItems = useMemo(
    () => items.filter((item) => item.kind === MuseJobKind.Image),
    [items],
  );

  const itemIndexMap = useMemo(() => new Map(items.map((item, idx) => [item.id, idx])), [items]);
  const lightboxItems = useMemo<MuseLightboxItem[]>(
    () =>
      items.map((item) => {
        const meta: string[] = [];
        const dateText = formatLibraryDate(item.createdAt);
        if (dateText) meta.push(dateText);
        if (item.savedPath) meta.push('Đã lưu ra máy');
        return {
          url: item.localUrl,
          kind: item.kind,
          prompt: item.prompt,
          title: libraryTitle(item),
          meta,
          revealPath: item.savedPath || item.localUrl,
        };
      }),
    [items],
  );

  const act = async (action: () => Promise<void>) => {
    try {
      await action();
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  const forget = (item: MuseLibraryItem) =>
    void act(async () => {
      if (
        !(await confirmAction({
          title: 'Bỏ khỏi Thư viện Muse?',
          message: 'Bỏ tệp này khỏi Thư viện Muse? File vẫn còn trên đĩa.',
          confirmLabel: 'Bỏ khỏi thư viện',
          danger: true,
        }))
      )
        return;
      setLightboxIndex(null);
      unwrap(await api.removeMuseLibraryItems([item.id]));
      await library.reload();
    });

  // Forgets every file the filter shows; files saved to a folder stay there.
  const clearShown = () =>
    void act(async () => {
      const what = filter === 'all' ? 'tệp' : filter === MuseJobKind.Video ? 'video' : 'ảnh';
      if (
        !(await confirmAction({
          title: 'Dọn Thư viện Muse?',
          message: `Bỏ ${items.length} ${what} khỏi Thư viện Muse? Tệp đã lưu ra thư mục vẫn còn trên đĩa.`,
          confirmLabel: 'Dọn thư viện',
          danger: true,
        }))
      )
        return;
      const removed = unwrap(await api.removeMuseLibraryItems(items.map((item) => item.id)));
      await library.reload();
      addToast({ type: 'success', message: `Đã dọn ${removed} ${what} khỏi thư viện.` });
    });

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <div className="muse-lib-heading">
          <h1 className="muse-title">Thư viện Muse</h1>
          <span className="muse-hint">
            {library.items.length} tác phẩm đã tạo (tách riêng khỏi hàng chờ để dọn hàng đợi không
            bị mất)
          </span>
        </div>
        <Button onClick={() => void act(async () => void unwrap(await api.openMuseOutputFolder()))}>
          <Icon icon={FolderOpen} />
          <span>Mở thư mục lưu</span>
        </Button>
      </div>

      <div className="muse-lib-layout">
        <div className="muse-lib-toolbar">
          <div className="muse-lib-filters">
            <SegmentedControl
              aria-label="Lọc thư viện"
              value={filter}
              onChange={(next) => {
                setFilter(next);
                setLightboxIndex(null);
              }}
              options={kindFilters}
            />
            <div className="muse-lib-search">
              <Icon icon={Search} size="sm" />
              <input
                type="text"
                value={search}
                placeholder="Tìm theo prompt hoặc nhãn…"
                aria-label="Tìm kiếm trong thư viện"
                onChange={(e) => {
                  setSearch(e.target.value);
                  setLightboxIndex(null);
                }}
              />
              {search && (
                <button
                  type="button"
                  className="muse-lib-search-clear"
                  aria-label="Xoá tìm kiếm"
                  onClick={() => setSearch('')}
                >
                  <Icon icon={X} size="sm" />
                </button>
              )}
            </div>
            <select
              className="batch-input batch-select muse-lib-sort"
              aria-label="Sắp xếp thư viện"
              value={sort}
              onChange={(e) => setSort(e.target.value as LibrarySort)}
            >
              <option value="newest">Mới nhất</option>
              <option value="oldest">Cũ nhất</option>
              <option value="title">Theo tên / prompt</option>
            </select>
            <span className="muse-hint">{items.length} tệp</span>
          </div>
          <div className="muse-lib-filters">
            {imageItems.length > 0 && (
              <Button
                size="sm"
                variant="secondary"
                title="Lưu toàn bộ ảnh đang lọc vào Kho Tư liệu"
                onClick={() =>
                  setVaultSources(
                    imageItems.map((it) => ({
                      localUrl: it.localUrl,
                      suggestedPrompt: it.prompt,
                      suggestedName: it.label || it.prompt,
                    })),
                  )
                }
              >
                <Icon icon={BookmarkPlus} size="sm" />
                <span>Lưu {imageItems.length} ảnh vào Kho</span>
              </Button>
            )}
            <Button
              size="sm"
              variant="ghost"
              className="muse-lib-clear"
              disabled={items.length === 0}
              onClick={clearShown}
            >
              <Icon icon={Trash2} size="sm" />
              <span>Dọn thư viện</span>
            </Button>
          </div>
        </div>

        {library.error ? (
          <div className="muse-empty">Không tải được thư viện: {library.error}</div>
        ) : items.length === 0 ? (
          <div className="muse-empty">
            {library.loading
              ? 'Đang tải thư viện…'
              : search.trim()
                ? 'Không tìm thấy tác phẩm khớp từ khoá.'
                : 'Chưa có tác phẩm nào hoàn thành.'}
          </div>
        ) : (
          <RevealList
            className="muse-lib-grid"
            items={items}
            keyOf={(item) => item.id}
            step={LIST_REVEAL_STEP}
            resetKey={`${filter}:${sort}:${search}`}
            render={(item) => {
              const dateLabel = formatLibraryDate(item.createdAt);
              return (
                <article className="muse-lib-item">
                  <div className="muse-lib-preview-box">
                    <span className="muse-lib-badge">
                      {item.kind === MuseJobKind.Video ? 'VIDEO' : 'ẢNH'}
                    </span>
                    <button
                      type="button"
                      className="muse-lib-open"
                      aria-label={`Phóng to ${libraryTitle(item)}`}
                      onClick={() => setLightboxIndex(itemIndexMap.get(item.id) ?? 0)}
                    >
                      {item.kind === MuseJobKind.Video ? (
                        <>
                          <video
                            src={item.localUrl}
                            className="muse-lib-media"
                            preload="metadata"
                            muted
                          />
                          <span className="muse-lib-play" aria-hidden="true">
                            <Icon icon={Play} size="lg" />
                          </span>
                        </>
                      ) : (
                        <img
                          src={thumbUrl(item.localUrl)}
                          className="muse-lib-media"
                          alt={libraryTitle(item)}
                          loading="lazy"
                          decoding="async"
                        />
                      )}
                    </button>
                  </div>
                  <div className="muse-lib-meta">
                    <div className="muse-lib-title" title={libraryTitle(item)}>
                      {libraryTitle(item)}
                    </div>
                    <div className="muse-lib-foot">
                      <span className="muse-lib-submeta">
                        {dateLabel || (item.savedPath ? 'Đã lưu' : 'Trong app')}
                      </span>
                      <div className="muse-lib-actions">
                        {item.kind === MuseJobKind.Image && (
                          <IconAction
                            icon={BookmarkPlus}
                            label="Lưu vào Kho Tư liệu Muse"
                            onClick={() =>
                              setVaultSources([
                                {
                                  localUrl: item.localUrl,
                                  suggestedPrompt: item.prompt,
                                  suggestedName: item.label || item.prompt,
                                },
                              ])
                            }
                          />
                        )}
                        <IconAction
                          icon={Download}
                          label="Lưu ra thư mục khác"
                          onClick={() =>
                            void act(async () => {
                              const saved = unwrap(await api.saveMedia(item.localUrl));
                              if (!saved.canceled)
                                addToast({ type: 'success', message: `Đã lưu ${saved.path}` });
                            })
                          }
                        />
                        <IconAction
                          icon={ExternalLink}
                          label="Mở thư mục chứa file"
                          onClick={() =>
                            void act(
                              async () =>
                                void unwrap(await api.revealFile(item.savedPath || item.localUrl)),
                            )
                          }
                        />
                        <IconAction
                          icon={Trash2}
                          label="Bỏ khỏi thư viện (file vẫn còn trên đĩa)"
                          onClick={() => forget(item)}
                        />
                      </div>
                    </div>
                  </div>
                </article>
              );
            }}
          />
        )}
      </div>

      {vaultSources && (
        <SaveToVaultDialog sources={vaultSources} onClose={() => setVaultSources(null)} />
      )}
      {lightboxIndex !== null && lightboxItems[lightboxIndex] && (
        <MuseMediaLightbox
          items={lightboxItems}
          index={lightboxIndex}
          onIndexChange={setLightboxIndex}
          onClose={() => setLightboxIndex(null)}
          onSave={(lb) =>
            void act(async () => {
              const saved = unwrap(await api.saveMedia(lb.url));
              if (!saved.canceled) addToast({ type: 'success', message: `Đã lưu ${saved.path}` });
            })
          }
          onReveal={(lb) =>
            void act(async () => void unwrap(await api.revealFile(lb.revealPath || lb.url)))
          }
          onSaveToVault={(lb) => {
            if (lb.kind !== MuseJobKind.Image) return;
            setLightboxIndex(null);
            setVaultSources([
              { localUrl: lb.url, suggestedPrompt: lb.prompt, suggestedName: lb.title },
            ]);
          }}
          onRemove={() => {
            const target = items[lightboxIndex];
            if (target) forget(target);
          }}
        />
      )}
    </div>
  );
};
