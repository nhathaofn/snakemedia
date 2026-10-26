import React, { useEffect, useMemo, useState } from 'react';
import { Check, Clock, ExternalLink, Film, Plus, RotateCcw, Sparkles, Trash2 } from 'lucide-react';
import '../shared/Muse.css';
import './MuseScriptPage.css';
import { Button, Card, Icon } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import type { Production, SceneItem, ScriptConfig, StudioData } from '../../../types';
import { useMuseStore } from '../shared/museStore';
import { useMuseLive } from '../shared/useMuseLive';

export const MuseScriptPage: React.FC = () => {
  useMuseLive();
  const options = useMuseStore((s) => s.options);
  const addToast = useAppStore((s) => s.addToast);

  const [studio, setStudio] = useState<StudioData | null>(null);
  const [productions, setProductions] = useState<Production[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);

  // Script editor state
  const [scriptText, setScriptText] = useState(
    'Cảnh 1: Một buổi sáng mùa thu tại Hà Nội\nCảnh 2: Cô gái dạo bước bên bờ hồ Hoàn Kiếm\nCảnh 3: Nụ cười rạng rỡ khi ngắm nhìn lá vàng rơi',
  );
  const [prefix, setPrefix] = useState('Tạo video chân thực');
  const [ratio, setRatio] = useState('9:16');
  const [duration, setDuration] = useState('each'); // "each" | "10s" | "15s" | "20s" | "30s"
  const [voice, setVoice] = useState('Chipper Tambourine');
  const [host, setHost] = useState(''); // "" | "nam" | "nữ"
  const [boardMode, setBoardMode] = useState('review'); // "" | "auto" | "review"
  const [characterTag, setCharacterTag] = useState('');
  const [merge, setMerge] = useState(true);
  const [music, setMusic] = useState(true);

  const loadData = async () => {
    try {
      const [sData, pList] = await Promise.all([
        api
          .getMuseStudio()
          .then(unwrap)
          .catch(() => null),
        api
          .listMuseProductions()
          .then(unwrap)
          .catch(() => []),
      ]);
      setStudio(sData);
      const safeList = Array.isArray(pList) ? [...pList] : [];
      setProductions(safeList.sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
    } catch (cause) {
      addToast({
        type: 'error',
        message: cause instanceof Error ? cause.message : 'Không tải được danh sách kịch bản',
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    let active = true;
    Promise.all([api.getMuseStudio(), api.listMuseProductions()])
      .then(([sRes, pRes]) => {
        if (active && sRes.data) setStudio(sRes.data);
        if (active && pRes.data) {
          setProductions(pRes.data.sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
        }
      })
      .catch((cause) => {
        if (active) {
          addToast({
            type: 'error',
            message: cause instanceof Error ? cause.message : 'Không tải được danh sách kịch bản',
          });
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    const interval = setInterval(() => {
      api
        .listMuseProductions()
        .then((res) => {
          if (active && res.data) {
            setProductions(res.data.sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
          }
        })
        .catch(() => {});
    }, 4000);
    return () => {
      active = false;
      clearInterval(interval);
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

  // Parse batch scripts: paragraphs separated by blank lines = 1 video, each line = 1 scene
  const parsedVideos = useMemo(() => {
    const raw = scriptText.trim();
    if (!raw) return [];
    const paragraphs = raw.split(/\n\s*\n/);
    return paragraphs
      .map((p, idx) => {
        const lines = p
          .split('\n')
          .map((l) => l.trim())
          .filter(Boolean);
        const scenes: SceneItem[] = lines.map((line) => {
          // Check if format has voiceover: "Visual :: Voiceover"
          if (line.includes('::')) {
            const [v, vo] = line.split('::');
            return { text: v.trim(), voiceover: vo.trim() };
          }
          return { text: line };
        });
        return {
          title: lines[0] ? lines[0].slice(0, 40) : `Video ${idx + 1}`,
          scenes,
        };
      })
      .filter((v) => v.scenes.length > 0);
  }, [scriptText]);

  // Estimate duration
  const estMin = useMemo(() => {
    let totalScenes = 0;
    for (const v of parsedVideos) {
      totalScenes += v.scenes.length;
    }
    // Around 1.5 - 2 minutes per scene render on Muse
    return Math.max(1, Math.round(totalScenes * 1.8));
  }, [parsedVideos]);

  const handleQueueBatch = async () => {
    if (parsedVideos.length === 0) {
      addToast({ type: 'error', message: 'Vui lòng nhập kịch bản' });
      return;
    }
    void run('queue', async () => {
      const cfg: ScriptConfig = {
        prefix,
        ratio,
        duration,
        voice,
        host,
        style: '',
        audio: '',
        merge,
        music,
        board: boardMode,
        characterTag,
      };

      for (const v of parsedVideos) {
        const prod: Production = {
          id: '',
          title: v.title,
          config: cfg,
          scenes: v.scenes,
          refs: [],
          status: 'queued',
          currentSceneIdx: 0,
          createdAt: '',
          updatedAt: '',
        };
        unwrap(await api.submitMuseProduction(prod));
      }
      addToast({
        type: 'success',
        message: `Đã đưa ${parsedVideos.length} video (${parsedVideos.reduce((acc, v) => acc + v.scenes.length, 0)} cảnh) vào hàng đợi!`,
      });
      await loadData();
    });
  };

  const handleApproveBoard = async (id: string) => {
    void run(`approve-${id}`, async () => {
      unwrap(await api.approveMuseProductionBoard(id));
      addToast({ type: 'success', message: 'Đã duyệt storyboard. Tiến trình video bắt đầu chạy!' });
      await loadData();
    });
  };

  const handleRedrawBoard = async (id: string) => {
    void run(`redraw-${id}`, async () => {
      unwrap(await api.redrawMuseProductionBoard(id));
      addToast({ type: 'info', message: 'Đang yêu cầu vẽ lại storyboard mới...' });
      await loadData();
    });
  };

  const handleRetry = async (id: string) => {
    void run(`retry-${id}`, async () => {
      unwrap(await api.retryMuseProduction(id));
      addToast({ type: 'info', message: 'Đã thử lại production' });
      await loadData();
    });
  };

  const handleRemove = async (id: string) => {
    if (!window.confirm('Xoá video này khỏi hàng đợi?')) return;
    void run(`remove-${id}`, async () => {
      unwrap(await api.removeMuseProduction(id));
      await loadData();
    });
  };

  if (loading) {
    return (
      <div className="page-shell muse-page">
        <div className="muse-loading">Đang tải trang Kịch bản & Production...</div>
      </div>
    );
  }

  const voiceList = options?.voices || [];

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <div>
          <h1 className="muse-title">Kịch bản & Production</h1>
          <p className="muse-subtitle">
            Tạo video nhiều cảnh theo kịch bản: vẽ storyboard trước để giữ bối cảnh/người mẫu, tạo
            từng cảnh, tự động ghép nối và lồng nhạc nền.
          </p>
        </div>
      </div>

      <div className="muse-script-layout">
        <div className="muse-script-grid">
          {/* Script Editor Card */}
          <Card className="muse-panel">
            <div className="muse-form-title">
              <Icon icon={Film} />
              <span>Soạn kịch bản theo lô</span>
            </div>
            <p className="muse-hint" style={{ marginBottom: '0.75rem' }}>
              Mỗi đoạn cách nhau bằng dòng trống = 1 video. Mỗi dòng = 1 cảnh (dùng{' '}
              <code>Hình :: Lời</code> nếu muốn chỉ định lời thoại riêng).
            </p>
            <textarea
              className="muse-script-textarea"
              value={scriptText}
              onChange={(e) => setScriptText(e.target.value)}
            />
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                marginTop: '0.75rem',
              }}
            >
              <div
                style={{
                  fontSize: '0.8125rem',
                  color: 'var(--text-secondary)',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.375rem',
                }}
              >
                <Icon icon={Clock} size="sm" />
                <span>
                  {parsedVideos.length} video ·{' '}
                  {parsedVideos.reduce((acc, v) => acc + v.scenes.length, 0)} cảnh · Ước tính ~
                  {estMin} phút
                </span>
              </div>
              <Button
                variant="primary"
                isLoading={busy === 'queue'}
                disabled={parsedVideos.length === 0}
                onClick={handleQueueBatch}
              >
                <Icon icon={Plus} />
                <span>Đưa vào hàng đợi Production</span>
              </Button>
            </div>
          </Card>

          {/* Config Settings Card */}
          <Card className="muse-panel">
            <div className="muse-form-title">
              <Icon icon={Sparkles} />
              <span>Cấu hình Video & Giọng đọc</span>
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <label className="muse-field">
                <span>Tiền tố chỉ đạo</span>
                <input
                  className="muse-input"
                  value={prefix}
                  onChange={(e) => setPrefix(e.target.value)}
                />
              </label>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
                <label className="muse-field">
                  <span>Tỉ lệ khung hình</span>
                  <select
                    className="muse-input"
                    value={ratio}
                    onChange={(e) => setRatio(e.target.value)}
                  >
                    <option value="9:16">9:16 (Dọc TikTok/Reel)</option>
                    <option value="16:9">16:9 (Ngang YouTube)</option>
                    <option value="1:1">1:1 (Vuông)</option>
                  </select>
                </label>

                <label className="muse-field">
                  <span>Chế độ thời lượng</span>
                  <select
                    className="muse-input"
                    value={duration}
                    onChange={(e) => setDuration(e.target.value)}
                  >
                    <option value="each">Từng cảnh 10s (Tự ghép)</option>
                    <option value="15s">Gộp 1 video 15s</option>
                    <option value="20s">Gộp 1 video 20s</option>
                    <option value="30s">Gộp 1 video 30s</option>
                    <option value="60s">Gộp 1 video 60s</option>
                  </select>
                </label>
              </div>

              <label className="muse-field">
                <span>Giọng đọc thuyết minh</span>
                <select
                  className="muse-input"
                  value={voice}
                  onChange={(e) => setVoice(e.target.value)}
                >
                  {voiceList.map((v) => (
                    <option key={v.value} value={v.value}>
                      {v.label}
                    </option>
                  ))}
                </select>
              </label>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
                <label className="muse-field">
                  <span>Người dẫn</span>
                  <select
                    className="muse-input"
                    value={host}
                    onChange={(e) => setHost(e.target.value)}
                  >
                    <option value="">Tự động theo giọng</option>
                    <option value="nam">Nam</option>
                    <option value="nữ">Nữ</option>
                  </select>
                </label>

                <label className="muse-field">
                  <span>Storyboard</span>
                  <select
                    className="muse-input"
                    value={boardMode}
                    onChange={(e) => setBoardMode(e.target.value)}
                  >
                    <option value="review">Vẽ trước, chờ duyệt</option>
                    <option value="auto">Vẽ trước, tự làm video</option>
                    <option value="">Không vẽ storyboard</option>
                  </select>
                </label>
              </div>

              {studio && (studio.assets?.length ?? 0) > 0 && (
                <label className="muse-field">
                  <span>Gắn nhân vật dẫn cố định từ Studio (tuỳ chọn)</span>
                  <select
                    className="muse-input"
                    value={characterTag}
                    onChange={(e) => setCharacterTag(e.target.value)}
                  >
                    <option value="">Không chọn nhân vật riêng</option>
                    {(studio.assets ?? [])
                      .filter((a) => a.role === 'character_face' || a.role === 'character_full')
                      .map((a) => (
                        <option key={a.id} value={a.tag}>
                          @{a.tag} ({a.name})
                        </option>
                      ))}
                  </select>
                </label>
              )}

              <div style={{ display: 'flex', gap: '1rem', marginTop: '0.5rem' }}>
                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.375rem',
                    cursor: 'pointer',
                    fontSize: '0.875rem',
                  }}
                >
                  <input
                    type="checkbox"
                    checked={merge}
                    onChange={(e) => setMerge(e.target.checked)}
                  />
                  <span>Tự động ghép các cảnh</span>
                </label>
                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.375rem',
                    cursor: 'pointer',
                    fontSize: '0.875rem',
                  }}
                >
                  <input
                    type="checkbox"
                    checked={music}
                    onChange={(e) => setMusic(e.target.checked)}
                  />
                  <span>Nhạc nền ngẫu nhiên</span>
                </label>
              </div>
            </div>
          </Card>
        </div>

        {/* Productions Queue List */}
        <Card className="muse-panel">
          <div className="muse-form-title">
            <Icon icon={Film} />
            <span>Hàng đợi Production ({productions.length})</span>
          </div>

          {productions.length === 0 ? (
            <div className="muse-empty">
              Chưa có video nào trong hàng đợi. Soạn kịch bản ở trên rồi bấm "Đưa vào hàng đợi
              Production".
            </div>
          ) : (
            <div>
              {productions.map((prod, idx) => (
                <div key={prod.id} className="muse-prod-row">
                  <div className="muse-prod-meta" style={{ flex: 1 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <span style={{ color: 'var(--text-secondary)' }}>#{idx + 1}</span>
                      <span className="muse-prod-title">{prod.title}</span>
                      <span
                        className={`muse-tag ${
                          prod.status === 'done'
                            ? 'muse-tag-success'
                            : prod.status === 'error'
                              ? 'muse-tag-danger'
                              : prod.status === 'review'
                                ? 'muse-tag-warning'
                                : ''
                        }`}
                      >
                        {prod.status === 'review'
                          ? 'Chờ duyệt Storyboard'
                          : prod.status === 'running'
                            ? 'Đang làm'
                            : prod.status === 'done'
                              ? 'Hoàn thành'
                              : prod.status === 'error'
                                ? 'Lỗi'
                                : 'Chờ xử lý'}
                      </span>
                    </div>

                    <div className="muse-prod-step">
                      {prod.step ||
                        `${prod.scenes?.length ?? 0} cảnh · ${prod.config?.duration ?? ''}`}
                      {prod.error && <span className="muse-text-danger">— Lỗi: {prod.error}</span>}
                    </div>

                    {/* Show storyboard preview when available */}
                    {(prod.boardFiles?.length ?? 0) > 0 && (
                      <div className="muse-script-preview-board">
                        {(prod.boardFiles ?? []).map((bFile, bIdx) => (
                          <img
                            key={bIdx}
                            src={bFile}
                            className="muse-script-board-img"
                            alt={`Board ${bIdx + 1}`}
                          />
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="muse-prod-actions">
                    {prod.status === 'review' && (
                      <>
                        <Button
                          variant="primary"
                          size="sm"
                          isLoading={busy === `approve-${prod.id}`}
                          onClick={() => handleApproveBoard(prod.id)}
                        >
                          <Icon icon={Check} size="sm" />
                          <span>Duyệt làm video</span>
                        </Button>
                        <Button
                          size="sm"
                          isLoading={busy === `redraw-${prod.id}`}
                          onClick={() => handleRedrawBoard(prod.id)}
                        >
                          <Icon icon={RotateCcw} size="sm" />
                          <span>Vẽ lại board</span>
                        </Button>
                      </>
                    )}

                    {(prod.status === 'error' || prod.status === 'stopped') && (
                      <Button
                        size="sm"
                        isLoading={busy === `retry-${prod.id}`}
                        onClick={() => handleRetry(prod.id)}
                      >
                        <Icon icon={RotateCcw} size="sm" />
                        <span>Thử lại</span>
                      </Button>
                    )}

                    {prod.finalVideoFile && (
                      <Button
                        size="sm"
                        onClick={() => void api.revealMuseFile(prod.finalVideoFile!)}
                      >
                        <Icon icon={ExternalLink} size="sm" />
                        <span>Mở video</span>
                      </Button>
                    )}

                    {prod.status !== 'running' && (
                      <Button
                        size="sm"
                        variant="ghost"
                        title="Xoá khỏi danh sách"
                        onClick={() => handleRemove(prod.id)}
                      >
                        <Icon icon={Trash2} size="sm" />
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </div>
  );
};
