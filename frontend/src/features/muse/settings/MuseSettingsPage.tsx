import React, { useState } from 'react';
import { FolderOpen } from 'lucide-react';
import '../shared/Muse.css';
import './MuseSettingsPage.css';
import { Button, Card, Icon, SegmentedControl } from '../../../components/ui';
import { useConfigUpdate } from '../../../hooks';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseMode } from '../../../types';
import type { MuseConfig } from '../../../types';

const booleanOptions = [
  { value: 'true', label: 'Bật' },
  { value: 'false', label: 'Tắt' },
];

const modeOptions = [
  { value: MuseMode.Rotate, label: 'Luân phiên (Rotate)' },
  { value: MuseMode.Sequential, label: 'Tuần tự (Sequential)' },
];

export const MuseSettingsPage: React.FC = () => {
  const config = useAppStore((s) => s.config);
  const addToast = useAppStore((s) => s.addToast);
  const { update, saving } = useConfigUpdate();

  const muse = config?.muse;
  const [outputDir, setOutputDir] = useState(muse?.outputDir ?? '');
  const [ffmpegPath, setFfmpegPath] = useState(muse?.ffmpegPath ?? '');

  if (!muse) {
    return <div className="page-shell muse-settings-page">Đang tải cài đặt…</div>;
  }

  const updateMuse = (patch: Partial<MuseConfig>) => {
    void update({ muse: { ...muse, ...patch } as MuseConfig });
  };

  const pickFolder = async () => {
    try {
      const res = unwrap(await api.chooseImageOutputDirectory(outputDir));
      if (!res.canceled && res.path) {
        setOutputDir(res.path);
        updateMuse({ outputDir: res.path });
      }
    } catch (e) {
      addToast({
        type: 'error',
        message: e instanceof Error ? e.message : 'Không chọn được thư mục',
      });
    }
  };

  return (
    <div className="page-shell muse-settings-page">
      <h1 className="muse-settings-title">Cài đặt Muse</h1>

      {/* Phân bổ & Luồng */}
      <section className="muse-settings-section">
        <h2 className="muse-settings-section-title">Phân bổ & Luồng chạy</h2>
        <Card className="muse-settings-card">
          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Chế độ phân bổ tài khoản</div>
              <div className="muse-settings-row-hint">
                Luân phiên ưu tiên tài khoản ít bận và còn nhiều quota; Tuần tự dùng theo thứ tự
                lưu.
              </div>
            </div>
            <SegmentedControl
              aria-label="Chế độ phân bổ"
              options={modeOptions}
              value={muse.mode}
              onChange={(m) => updateMuse({ mode: m as MuseMode })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Chạy ẩn trình duyệt (Headless)</div>
              <div className="muse-settings-row-hint">
                Chạy Chrome không hiện cửa sổ khi làm việc; lúc đăng nhập vẫn mở cửa sổ.
              </div>
            </div>
            <SegmentedControl
              aria-label="Chạy ẩn Chrome"
              options={booleanOptions}
              value={String(muse.headless)}
              onChange={(v) => updateMuse({ headless: v === 'true' })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Số tài khoản chạy cùng lúc</div>
              <div className="muse-settings-row-hint">
                Giới hạn số tài khoản hoạt động đồng thời (1–50).
              </div>
            </div>
            <input
              type="number"
              min={1}
              max={50}
              className="muse-settings-input"
              value={muse.maxAccounts}
              onChange={(e) => updateMuse({ maxAccounts: Number(e.target.value) })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Tổng số luồng đồng thời</div>
              <div className="muse-settings-row-hint">
                Giới hạn số tác vụ chạy cùng lúc trên tất cả tài khoản (1–100).
              </div>
            </div>
            <input
              type="number"
              min={1}
              max={100}
              className="muse-settings-input"
              value={muse.totalThreads}
              onChange={(e) => updateMuse({ totalThreads: Number(e.target.value) })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Số luồng trên mỗi tài khoản</div>
              <div className="muse-settings-row-hint">
                Số tác vụ chạy song song trên 1 tài khoản (1–5).
              </div>
            </div>
            <input
              type="number"
              min={1}
              max={5}
              className="muse-settings-input"
              value={muse.threadsPerAccount}
              onChange={(e) => updateMuse({ threadsPerAccount: Number(e.target.value) })}
              disabled={saving}
            />
          </div>
        </Card>
      </section>

      {/* Thời gian & Thử lại */}
      <section className="muse-settings-section">
        <h2 className="muse-settings-section-title">Thời gian & Thử lại</h2>
        <Card className="muse-settings-card">
          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Số lần thử lại tối đa</div>
              <div className="muse-settings-row-hint">
                Số lần gửi lại hoặc dò lại phiên khi gặp lỗi (1–5).
              </div>
            </div>
            <input
              type="number"
              min={1}
              max={5}
              className="muse-settings-input"
              value={muse.maxAttempts}
              onChange={(e) => updateMuse({ maxAttempts: Number(e.target.value) })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Giới hạn thời gian tạo ảnh (phút)</div>
              <div className="muse-settings-row-hint">
                Tối đa chờ Muse render xong ảnh (1–60 phút).
              </div>
            </div>
            <input
              type="number"
              min={1}
              max={60}
              className="muse-settings-input"
              value={muse.imageTimeoutMinutes}
              onChange={(e) => updateMuse({ imageTimeoutMinutes: Number(e.target.value) })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Giới hạn thời gian tạo video (phút)</div>
              <div className="muse-settings-row-hint">
                Tối đa chờ Muse render xong video (5–120 phút).
              </div>
            </div>
            <input
              type="number"
              min={5}
              max={120}
              className="muse-settings-input"
              value={muse.videoTimeoutMinutes}
              onChange={(e) => updateMuse({ videoTimeoutMinutes: Number(e.target.value) })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Giãn cách giữa 2 lần gửi (giây)</div>
              <div className="muse-settings-row-hint">
                Nghỉ ngẫu nhiên giữa tối thiểu và tối đa trên cùng 1 tài khoản để tránh bị khóa.
              </div>
            </div>
            <div className="muse-settings-picker-row">
              <input
                type="number"
                min={0}
                max={300}
                className="muse-settings-input"
                style={{ width: 80 }}
                value={muse.sendGapMinSeconds}
                onChange={(e) => updateMuse({ sendGapMinSeconds: Number(e.target.value) })}
                disabled={saving}
              />
              <span>–</span>
              <input
                type="number"
                min={0}
                max={600}
                className="muse-settings-input"
                style={{ width: 80 }}
                value={muse.sendGapMaxSeconds}
                onChange={(e) => updateMuse({ sendGapMaxSeconds: Number(e.target.value) })}
                disabled={saving}
              />
              <span>giây</span>
            </div>
          </div>
        </Card>
      </section>

      {/* Hạn mức & Quota */}
      <section className="muse-settings-section">
        <h2 className="muse-settings-section-title">Hạn mức & Nghỉ ngơi</h2>
        <Card className="muse-settings-card">
          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Dùng lượt nạp thêm (Topup)</div>
              <div className="muse-settings-row-hint">
                Tiếp tục chạy bằng token nạp thêm sau khi hết hạn mức tuần.
              </div>
            </div>
            <SegmentedControl
              aria-label="Dùng token nạp thêm"
              options={booleanOptions}
              value={String(muse.useTopup)}
              onChange={(v) => updateMuse({ useTopup: v === 'true' })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Vẫn chạy khi hết quota (Keep Going)</div>
              <div className="muse-settings-row-hint">
                Bỏ qua kiểm tra quota đã lưu và để Muse tự quyết định.
              </div>
            </div>
            <SegmentedControl
              aria-label="Vẫn chạy khi hết quota"
              options={booleanOptions}
              value={String(muse.keepGoing)}
              onChange={(v) => updateMuse({ keepGoing: v === 'true' })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Thời gian nghỉ khi hết lượt (phút)</div>
              <div className="muse-settings-row-hint">
                Tạm nghỉ tài khoản khi Muse báo chạm hạn mức mà không đọc được ngày reset.
              </div>
            </div>
            <input
              type="number"
              min={1}
              max={10080}
              className="muse-settings-input"
              value={muse.usagePauseMinutes}
              onChange={(e) => updateMuse({ usagePauseMinutes: Number(e.target.value) })}
              disabled={saving}
            />
          </div>
        </Card>
      </section>

      {/* Lưu trữ & Công cụ */}
      <section className="muse-settings-section">
        <h2 className="muse-settings-section-title">Lưu trữ & Công cụ ngoài</h2>
        <Card className="muse-settings-card">
          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">
                Tự động sao lưu file ra ổ đĩa (AutoSave)
              </div>
              <div className="muse-settings-row-hint">
                Tự động lưu một bản copy của video/ảnh đã tạo vào thư mục chỉ định theo ngày.
              </div>
            </div>
            <SegmentedControl
              aria-label="Tự động sao lưu"
              options={booleanOptions}
              value={String(muse.autoSave)}
              onChange={(v) => updateMuse({ autoSave: v === 'true' })}
              disabled={saving}
            />
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Thư mục lưu trữ hoàn tất</div>
              <div className="muse-settings-row-hint">
                Mặc định: Videos/SnakeMedia Muse/YYYY-MM-DD/
              </div>
            </div>
            <div className="muse-settings-picker-row">
              <input
                type="text"
                className="muse-settings-input muse-settings-input-wide"
                placeholder="Mặc định: ~/Videos/SnakeMedia Muse"
                value={outputDir}
                onChange={(e) => setOutputDir(e.target.value)}
                onBlur={() => updateMuse({ outputDir })}
                disabled={saving}
              />
              <Button size="sm" onClick={pickFolder} disabled={saving}>
                <Icon icon={FolderOpen} size="sm" />
                <span>Chọn</span>
              </Button>
            </div>
          </div>

          <div className="muse-settings-row">
            <div className="muse-settings-row-text">
              <div className="muse-settings-row-label">Đường dẫn FFmpeg tuỳ chỉnh</div>
              <div className="muse-settings-row-hint">
                Để trống để ứng dụng tự tìm trong thư mục bin hoặc PATH hệ thống.
              </div>
            </div>
            <input
              type="text"
              className="muse-settings-input muse-settings-input-wide"
              placeholder="Để trống nếu đã cài vào PATH"
              value={ffmpegPath}
              onChange={(e) => setFfmpegPath(e.target.value)}
              onBlur={() => updateMuse({ ffmpegPath })}
              disabled={saving}
            />
          </div>
        </Card>
      </section>
    </div>
  );
};
