import { useState } from 'react';
import { ImagePlus, X } from 'lucide-react';
import { BatchForm } from '../../../components/batch';
import { Button, Icon } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseJobKind, MuseJobRequest } from '../../../types';
import type { MuseOptions, MuseReference } from '../../../types';
import { formatBytes } from '../../../utils';
import { useMuseStore } from './museStore';

const kindText: Record<MuseJobKind, { noun: string; placeholder: string }> = {
  [MuseJobKind.Image]: {
    noun: 'ảnh',
    placeholder: 'Chân dung cô gái dưới mưa đèn neon, ảnh dọc',
  },
  [MuseJobKind.Video]: {
    noun: 'video',
    placeholder: 'Một chú mèo chạy trên bãi biển lúc hoàng hôn, video dọc 9:16',
  },
};

/**
 * Muse's batch form: the shared BatchForm (one job per prompt, each in its own
 * Muse chat) plus a file label and reference images. What Muse accepts as
 * references comes from the limits read on the accounts (MuseOptions).
 */
export function MuseBatchForm({
  kind,
  options,
}: {
  kind: MuseJobKind;
  options: MuseOptions | null;
}) {
  const setJob = useMuseStore((s) => s.setJob);
  const addToast = useAppStore((s) => s.addToast);
  const [label, setLabel] = useState('');
  const [references, setReferences] = useState<MuseReference[]>([]);
  const words = kindText[kind];
  const maxFiles = options?.known ? options.maxFiles : 0;

  const report = (cause: unknown) =>
    addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });

  const pickReferences = async () => {
    try {
      const picked = unwrap(await api.pickMuseReferences());
      setReferences((list) => [...list, ...picked].slice(0, maxFiles));
    } catch (cause) {
      report(cause);
    }
  };

  const submit = async (prompts: string[], repeat: number, held: boolean) => {
    try {
      const created = unwrap(
        await api.submitMuseJobs(
          MuseJobRequest.createFrom({
            kind,
            prompts,
            repeat,
            references: references.map((r) => r.localUrl),
            label,
            held,
          }),
        ),
      );
      created.forEach(setJob);
      addToast({
        type: 'success',
        message: `Đã thêm ${created.length} tác vụ${held ? ' (tạm giữ)' : ''} vào hàng đợi.`,
      });
      return true;
    } catch (cause) {
      report(cause);
      return false;
    }
  };

  return (
    <BatchForm
      id={`muse-${kind}-batch`}
      placeholder={words.placeholder}
      repeatLabel="Số lần mỗi prompt"
      hint={`Mỗi tác vụ là một chat Muse riêng. Công cụ tạo ${words.noun} của Muse chỉ nhận prompt và ảnh tham chiếu (không có tham số tỉ lệ hay thời lượng): muốn khung hình hay độ dài nào thì ghi trong prompt.`}
      onSubmit={submit}
    >
      <label className="muse-inline-field">
        <span>Tên (đặt tên file lưu)</span>
        <input
          className="muse-input"
          value={label}
          placeholder="Để trống: dùng prompt"
          onChange={(e) => setLabel(e.target.value)}
        />
      </label>

      <div className="muse-references">
        {references.map((ref) => (
          <div key={ref.localUrl} className="muse-thumb" title={ref.fileName}>
            <img src={ref.localUrl} alt={ref.fileName} />
            <button
              type="button"
              className="muse-thumb-remove"
              aria-label={`Bỏ ${ref.fileName}`}
              onClick={() =>
                setReferences((list) => list.filter((r) => r.localUrl !== ref.localUrl))
              }
            >
              <Icon icon={X} size="sm" />
            </button>
          </div>
        ))}
        {references.length < maxFiles && (
          <Button type="button" size="sm" onClick={() => void pickReferences()}>
            <Icon icon={ImagePlus} size="sm" />
            <span>Ảnh tham chiếu</span>
          </Button>
        )}
      </div>
      <div className="muse-hint">
        {options?.known
          ? `Muse nhận ${options.imageExtensions.join(', ')}; tối đa ${options.maxFiles} ảnh mỗi tin nhắn, mỗi ảnh ≤ ${formatBytes(options.maxFileBytes, 0)}.`
          : 'Chưa đọc được giới hạn đính kèm của Muse: hãy kiểm tra một tài khoản Muse đã đăng nhập để thêm ảnh tham chiếu.'}
      </div>
    </BatchForm>
  );
}
