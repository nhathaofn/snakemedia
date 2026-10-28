import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { ChevronDown, Film, X } from 'lucide-react';
import { Icon } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseRefMode } from '../../../types';
import type { MuseDraft } from './museDraft';
import { liveAssetIds, useMuseStudioStore } from './museStudioStore';
import { useMuseStore } from './museStore';

/** Loads the asset library and saved prompts once a Muse screen needs them. */
export function useMuseStudio() {
  const state = useMuseStudioStore();
  const { ensure } = state;
  useEffect(() => ensure(), [ensure]);
  return state;
}

type DraftUpdate = (change: Partial<MuseDraft>) => void;

/** How many consistency options a draft uses, shown on the folded group. */
export function activeExtras(draft: MuseDraft): number {
  return (
    (draft.assetIds.length > 0 ? 1 : 0) +
    (draft.boardFrames > 0 ? 1 : 0) +
    (draft.sourceVideo ? 1 : 0) +
    (draft.refMode ? 1 : 0)
  );
}

/**
 * The consistency tools of a create panel (assets, storyboard, source video,
 * templates) as one titled group that folds away when not needed.
 */
export function ExtrasGroup({ active, children }: { active: number; children: ReactNode }) {
  return (
    <details className="muse-extras" open={active > 0 || undefined}>
      <summary className="muse-extras-summary">
        <span>Đồng nhất nhân vật</span>
        {active > 0 && <span className="muse-extras-badge">{active}</span>}
        <Icon icon={ChevronDown} size="sm" className="muse-extras-chevron" />
      </summary>
      <div className="muse-extras-body">{children}</div>
    </details>
  );
}

/**
 * Assets of the Tư liệu screen attached to every prompt (a fixed model or
 * product). Assets a prompt names with @tag are attached to that prompt alone.
 */
export function FixedAssetsField({ draft, update }: { draft: MuseDraft; update: DraftUpdate }) {
  const { assets } = useMuseStudio();
  const chosen = draft.assetIds.filter((id) => assets.some((a) => a.id === id));
  if (assets.length === 0) {
    return (
      <span className="studio-field-hint">
        Chưa có tư liệu. Thêm ở Muse → Tư liệu rồi gọi bằng @tag.
      </span>
    );
  }
  return (
    <div className="studio-field-group">
      <span className="studio-field-label">Gắn vào mọi prompt</span>
      <div className="muse-asset-chips">
        {assets.map((a) => {
          const on = chosen.includes(a.id);
          return (
            <button
              key={a.id}
              type="button"
              className={`muse-asset-chip${on ? ' is-on' : ''}`}
              aria-pressed={on}
              title={a.name || a.tag}
              onClick={() =>
                update({
                  assetIds: on ? chosen.filter((id) => id !== a.id) : [...chosen, a.id],
                })
              }
            >
              {a.images[0] && <img src={a.images[0]} alt="" />}
              <span>@{a.tag}</span>
            </button>
          );
        })}
      </div>
      <span className="studio-field-hint">Mỗi prompt còn gọi riêng bằng @tag hoặc đúng tên.</span>
    </div>
  );
}

/** How a batch's own reference images go to its prompts. */
export function RefModeField({ draft, update }: { draft: MuseDraft; update: DraftUpdate }) {
  if (draft.references.length === 0) return null;
  return (
    <label className="studio-field-group">
      <span className="studio-field-label">Cách gắn ảnh</span>
      <select
        className="batch-input batch-select"
        value={draft.refMode || MuseRefMode.Shared}
        onChange={(e) => update({ refMode: e.target.value as MuseRefMode })}
      >
        <option value={MuseRefMode.Shared}>Dùng chung: mọi prompt</option>
        <option value={MuseRefMode.Pair}>Ghép cặp: prompt thứ n lấy ảnh thứ n</option>
        <option value={MuseRefMode.Rotate}>Luân phiên: mỗi prompt một ảnh, quay vòng</option>
      </select>
    </label>
  );
}

/**
 * Storyboard: Muse draws the opening frame of each scene first, then makes
 * the video from them, which keeps people, outfit, setting and product the
 * same across scenes at the cost of one more generation.
 */
export function StoryboardField({ draft, update }: { draft: MuseDraft; update: DraftUpdate }) {
  const maxFrames = useMuseStore((s) => s.options?.maxBoardFrames ?? 0);
  if (maxFrames === 0) return null;
  return (
    <div className="studio-field-group">
      <label className="studio-field-group">
        <span className="studio-field-label">Storyboard trước</span>
        <select
          className="batch-input batch-select"
          value={draft.boardFrames}
          onChange={(e) => update({ boardFrames: Number(e.target.value) })}
        >
          <option value={0}>Không, làm video thẳng</option>
          {Array.from({ length: maxFrames }, (_, i) => i + 1).map((n) => (
            <option key={n} value={n}>
              {n} khung mở đầu
            </option>
          ))}
        </select>
      </label>
      {draft.boardFrames > 0 && (
        <label className="muse-check">
          <input
            type="checkbox"
            checked={draft.boardReview}
            onChange={(e) => update({ boardReview: e.target.checked })}
          />
          <span>Chờ duyệt storyboard rồi mới làm video</span>
        </label>
      )}
      {draft.boardFrames > 0 && (
        <span className="studio-field-hint">
          Tốn thêm 1 lượt ảnh mỗi prompt; video gắn các khung theo thứ tự cảnh.
        </span>
      )}
    </div>
  );
}

/** A video whose camera, timing and motion every prompt recreates (v2v). */
export function SourceVideoField({ draft, update }: { draft: MuseDraft; update: DraftUpdate }) {
  const addToast = useAppStore((s) => s.addToast);
  const pick = async () => {
    try {
      const picked = unwrap(await api.pickMuseSourceVideo());
      if (picked.localUrl) update({ sourceVideo: picked });
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };
  return (
    <div className="studio-field-group">
      <span className="studio-field-label">Video mẫu</span>
      <div className="muse-video-pick">
        {draft.sourceVideo ? (
          <div className="muse-thumb" title={draft.sourceVideo.fileName}>
            <video src={draft.sourceVideo.localUrl} muted preload="metadata" />
            <button
              type="button"
              className="muse-thumb-remove"
              aria-label="Bỏ video mẫu"
              onClick={() => update({ sourceVideo: null })}
            >
              <Icon icon={X} size="sm" />
            </button>
          </div>
        ) : (
          <button
            type="button"
            className="muse-thumb muse-thumb-add"
            aria-label="Chọn video mẫu"
            onClick={() => void pick()}
          >
            <Icon icon={Film} size="lg" />
          </button>
        )}
        <span className="studio-field-hint">
          {draft.sourceVideo
            ? draft.sourceVideo.fileName
            : 'Muse làm lại theo góc máy, nhịp và chuyển động của video này.'}
        </span>
      </div>
    </div>
  );
}

/**
 * Saved prompts. With onInsert (Đơn) the text goes into the prompt; a batch
 * box keeps its own text, so the template is copied for pasting.
 */
export function TemplateField({ onInsert }: { onInsert?: (text: string) => void }) {
  const { templates } = useMuseStudio();
  const addToast = useAppStore((s) => s.addToast);
  if (templates.length === 0) return null;
  return (
    <label className="studio-field-group">
      <span className="studio-field-label">Mẫu prompt</span>
      <select
        className="batch-input batch-select"
        value=""
        onChange={(e) => {
          const t = templates.find((x) => x.id === e.target.value);
          if (!t) return;
          if (onInsert) {
            onInsert(t.text);
            return;
          }
          void navigator.clipboard
            .writeText(t.text)
            .then(() =>
              addToast({ type: 'success', message: `Đã chép mẫu "${t.name}", dán vào ô prompt.` }),
            )
            .catch(() => addToast({ type: 'error', message: 'Không chép được mẫu.' }));
        }}
      >
        <option value="">Chọn mẫu…</option>
        {templates.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name}
          </option>
        ))}
      </select>
    </label>
  );
}

/** What the Đơn prompt will attach from the library, and hints that never block it. */
export function PromptCheckHint({ prompt, assetIds }: { prompt: string; assetIds: string[] }) {
  const { assets } = useMuseStudio();
  // The answer is kept with the prompt it was for, so a stale one is never shown.
  const [check, setCheck] = useState<{ key: string; tags: string[]; warnings: string[] } | null>(
    null,
  );
  const ids = assetIds.join(',');
  const key = `${ids}|${prompt}`;
  const active = assets.length > 0 && prompt.trim() !== '';
  useEffect(() => {
    if (!active) return;
    const timer = window.setTimeout(() => {
      api
        .checkMusePrompt(prompt, liveAssetIds(ids ? ids.split(',') : []))
        .then((res) => setCheck(res.success && res.data ? { key, ...res.data } : null))
        .catch(() => setCheck(null));
    }, 400);
    return () => window.clearTimeout(timer);
  }, [active, key, prompt, ids]);
  if (!active || check?.key !== key) return null;
  if (check.tags.length === 0 && check.warnings.length === 0) return null;
  return (
    <div className="studio-field-group">
      {check.tags.length > 0 && (
        <span className="studio-field-hint">Gắn tư liệu: @{check.tags.join(', @')}</span>
      )}
      {check.warnings.map((w) => (
        <span key={w} className="studio-field-hint is-warning">
          {w}
        </span>
      ))}
    </div>
  );
}
