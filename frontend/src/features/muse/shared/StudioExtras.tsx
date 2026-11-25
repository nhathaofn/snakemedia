import { thumbUrl } from '../../../services';
import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { ChevronDown } from 'lucide-react';
import { Icon } from '../../../components/ui';
import { api } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseJobKind, MuseRefMode } from '../../../types';
import type { MuseAsset } from '../../../types';
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
    (draft.assetIds.length > 0 ? 1 : 0) + (draft.boardFrames > 0 ? 1 : 0) + (draft.refMode ? 1 : 0)
  );
}

/** Summarizes reference image files in Tư liệu (and fixed selections) for collapsed headers. */
export function referenceFileSummary(
  assets: readonly MuseAsset[],
  chosenIds: readonly string[],
): string {
  const totalFiles = assets.reduce((sum, a) => sum + a.images.length, 0);
  const chosenSet = new Set(chosenIds);
  const chosenFiles = assets
    .filter((a) => chosenSet.has(a.id))
    .reduce((sum, a) => sum + a.images.length, 0);
  if (chosenFiles > 0) return `${chosenFiles}/${totalFiles} file tham chiếu`;
  return `${totalFiles} file tham chiếu`;
}

/**
 * The consistency tools of a create panel (assets, storyboard, templates)
 * as one titled group that folds away when not needed.
 */
export function ExtrasGroup({
  active,
  summary,
  children,
}: {
  active: number;
  summary?: ReactNode;
  children: ReactNode;
}) {
  return (
    <details className="muse-extras" open={active > 0 || undefined}>
      <summary className="muse-extras-summary">
        <span>Đồng nhất nhân vật</span>
        {active > 0 && <span className="muse-extras-badge">{active}</span>}
        {summary && <span className="muse-extras-summary-meta">{summary}</span>}
        <Icon icon={ChevronDown} size="sm" className="muse-extras-chevron" />
      </summary>
      <div className="muse-extras-body">{children}</div>
    </details>
  );
}

/** Selects an Asset Vault project scope so @tag resolution prefers that project's assets. */
export function ProjectScopeField({ draft, update }: { draft: MuseDraft; update: DraftUpdate }) {
  const { assets } = useMuseStudio();
  const projects = Array.from(
    new Set(assets.map((a) => (a.project ?? '').trim()).filter((p) => p !== '')),
  ).sort();
  if (projects.length === 0) return null;
  return (
    <label className="studio-field-group">
      <span className="studio-field-label">Dự án tư liệu</span>
      <select
        className="batch-input batch-select"
        aria-label="Dự án tư liệu"
        value={draft.project}
        onChange={(e) => update({ project: e.target.value })}
      >
        <option value="">Tất cả dự án</option>
        {projects.map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </select>
    </label>
  );
}

/**
 * Assets of the Tư liệu screen attached to every prompt (a fixed model or
 * product). Assets a prompt names with @tag are attached to that prompt alone.
 */
export function FixedAssetsField({ draft, update }: { draft: MuseDraft; update: DraftUpdate }) {
  const { assets } = useMuseStudio();
  const [filter, setFilter] = useState('');
  const chosen = draft.assetIds.filter((id) => assets.some((a) => a.id === id));
  if (assets.length === 0) {
    return (
      <span className="studio-field-hint">
        Chưa có tư liệu. Thêm ở Muse → Tư liệu rồi gọi bằng @tag.
      </span>
    );
  }
  const activeProject = (draft.project ?? '').trim().toUpperCase();
  const scoped = activeProject
    ? assets.filter(
        (a) => chosen.includes(a.id) || !a.project || a.project.toUpperCase() === activeProject,
      )
    : assets;
  const q = filter.trim().toLowerCase().replace(/^@/, '');
  const shown = q
    ? scoped.filter(
        (a) =>
          chosen.includes(a.id) ||
          a.tag.toLowerCase().includes(q) ||
          a.name.toLowerCase().includes(q) ||
          (a.project ?? '').toLowerCase().includes(q),
      )
    : scoped;
  return (
    <div className="studio-field-group">
      <span className="studio-field-label">
        Gắn vào mọi prompt{chosen.length > 0 ? ` (${chosen.length})` : ''}
      </span>
      {scoped.length > 10 && (
        <input
          type="text"
          className="batch-input muse-asset-filter-input"
          value={filter}
          placeholder="Lọc nhanh @tag…"
          aria-label="Lọc tư liệu gắn vào mọi prompt"
          onChange={(e) => setFilter(e.target.value)}
        />
      )}
      <div className="muse-asset-chips">
        {shown.map((a) => {
          const on = chosen.includes(a.id);
          const count = a.images.length;
          return (
            <button
              key={a.id}
              type="button"
              className={`muse-asset-chip${on ? ' is-on' : ''}`}
              aria-pressed={on}
              title={`${a.project ? `[${a.project}] ` : ''}${a.name || a.tag}${count > 1 ? ` (${count} góc)` : ''}`}
              onClick={() =>
                update({
                  assetIds: on ? chosen.filter((id) => id !== a.id) : [...chosen, a.id],
                })
              }
            >
              {a.images[0] && (
                <img src={thumbUrl(a.images[0])} alt="" loading="lazy" decoding="async" />
              )}
              <span>
                {a.project ? `${a.project}/` : ''}@{a.tag}
              </span>
            </button>
          );
        })}
      </div>
      <span className="studio-field-hint">
        Gọi @tag hoặc @tag_góc (ví dụ @jimena_close_soot); không ghi góc sẽ lấy ảnh chính.
      </span>
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
 * Storyboard: Muse draws one image per scene first, then makes the video
 * with them as references (Muse has no image-to-video), which keeps people,
 * outfit, setting and product the same across scenes at the cost of one more
 * generation.
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
              {n} ảnh cảnh
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
          Tốn thêm 1 lượt ảnh mỗi prompt; video dùng các ảnh cảnh làm tham chiếu theo thứ tự cảnh,
          không bắt đầu đúng từ ảnh.
        </span>
      )}
    </div>
  );
}

/**
 * Saved prompts. Passes the selected template text to onInsert when provided,
 * falling back to clipboard copy otherwise.
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
export function PromptCheckHint({
  kind = MuseJobKind.Video,
  prompt,
  project = '',
  assetIds,
  boardFrames = 0,
}: {
  kind?: MuseJobKind;
  prompt: string;
  project?: string;
  assetIds: string[];
  boardFrames?: number;
}) {
  const { assets } = useMuseStudio();
  // The answer is kept with the prompt it was for, so a stale one is never shown.
  const [check, setCheck] = useState<{
    key: string;
    tags: string[];
    images: number;
    warnings: string[];
    estimatedBytes?: number;
    budgetBytes?: number;
  } | null>(null);
  const ids = assetIds.join(',');
  const frames = kind === MuseJobKind.Video ? boardFrames : 0;
  const key = `${kind}|${frames}|${project}|${ids}|${prompt}`;
  const active = (assets.length > 0 || frames > 0) && prompt.trim() !== '';
  useEffect(() => {
    if (!active) return;
    const timer = window.setTimeout(() => {
      api
        .checkMusePrompt(prompt, liveAssetIds(ids ? ids.split(',') : []), project, kind, frames)
        .then((res) => setCheck(res.success && res.data ? { key, ...res.data } : null))
        .catch(() => setCheck(null));
    }, 400);
    return () => window.clearTimeout(timer);
  }, [active, key, prompt, project, ids, kind, frames]);
  if (!active || check?.key !== key) return null;
  if (check.tags.length === 0 && check.warnings.length === 0) return null;
  const estMb = check.estimatedBytes ? (check.estimatedBytes / (1024 * 1024)).toFixed(1) : null;
  const budgetMb = check.budgetBytes ? Math.round(check.budgetBytes / (1024 * 1024)) : 8;
  return (
    <div className="studio-field-group">
      {check.tags.length > 0 && (
        <span className="studio-field-hint">
          Gắn tư liệu: @{check.tags.join(', @')} ({check.images} ảnh
          {estMb ? ` · ~${estMb}/${budgetMb} MB JPEG` : ''})
        </span>
      )}
      {check.warnings.map((w) => (
        <span key={w} className="studio-field-hint is-warning">
          {w}
        </span>
      ))}
    </div>
  );
}
