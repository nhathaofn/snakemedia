import { useRef, useState } from 'react';
import { Image as ImageIcon } from 'lucide-react';
import { Icon, Pagination } from '../../../components/ui';
import type { MuseAssetType, MuseRefRole } from '../../../types';
import { ASSET_TYPES, defaultTypeForRole, roleOrder, roles } from './assetRoles';
import type { BatchPreviewRow, BatchRowOverride } from './batchImport';

const IMPORT_PAGE_SIZE = 50;

const STATUS_LABELS: Record<BatchPreviewRow['status'], string> = {
  ready: 'Mới',
  rename: 'Đổi tên',
  overwrite: 'Cập nhật',
  skip: 'Bỏ qua',
  excluded: 'Đã bỏ',
  error: 'Lỗi',
};

interface BatchImportTableProps {
  rows: readonly BatchPreviewRow[];
  page: number;
  setPage: (page: number) => void;
  selectableCount: number;
  allSelectableIncluded: boolean;
  toggleRow: (index: number, shiftKey: boolean) => void;
  toggleAllRows: () => void;
  patchOverride: (index: number, patch: Partial<BatchRowOverride>) => void;
}

/** Bounds mounted rows while edits and selection remain indexed over the full scan. */
export function BatchImportTable({
  rows,
  page,
  setPage,
  selectableCount,
  allSelectableIncluded,
  toggleRow,
  toggleAllRows,
  patchOverride,
}: BatchImportTableProps) {
  const shiftHeld = useRef(false);
  return (
    <>
      <Pagination
        label="Phân trang ảnh nhập"
        page={page}
        pageCount={Math.ceil(rows.length / IMPORT_PAGE_SIZE)}
        onPageChange={setPage}
      />
      {rows.length > 0 && (
        <div className="muse-batch-table-wrap">
          <table className="muse-batch-table">
            <thead>
              <tr>
                <th className="col-check">
                  <input
                    type="checkbox"
                    aria-label="Chọn tất cả ảnh"
                    checked={allSelectableIncluded}
                    disabled={selectableCount === 0}
                    onChange={toggleAllRows}
                  />
                </th>
                <th className="col-num">#</th>
                <th className="col-img">Ảnh nguồn</th>
                <th className="col-proj">Dự án / Loại</th>
                <th className="col-tag">@Tag</th>
                <th className="col-meta">Góc / Biến thể / Trạng thái</th>
                <th className="col-role">Vai trò / Khoá</th>
                <th className="col-status">Tên trong Kho</th>
              </tr>
            </thead>
            <tbody>
              {rows.slice(page * IMPORT_PAGE_SIZE, (page + 1) * IMPORT_PAGE_SIZE).map((row) => (
                <tr
                  key={row.image.path || row.image.localUrl || row.index}
                  className={`muse-batch-row is-${row.status}${row.excluded ? ' is-excluded' : ''}`}
                >
                  <td className="col-check">
                    <input
                      type="checkbox"
                      aria-label={`Chọn ${row.image.fileName}`}
                      checked={!row.excluded}
                      disabled={Boolean(row.image.error)}
                      onMouseDown={(e) => {
                        shiftHeld.current = e.shiftKey;
                      }}
                      onKeyDown={(e) => {
                        shiftHeld.current = e.shiftKey;
                      }}
                      onChange={() => {
                        const shift = shiftHeld.current;
                        shiftHeld.current = false;
                        toggleRow(row.index, shift);
                      }}
                    />
                  </td>
                  <td className="col-num">{row.index + 1}</td>
                  <td className="col-img">
                    <div
                      className="muse-batch-file"
                      title={row.image.relPath || row.image.fileName}
                    >
                      <ImportThumbnail src={row.image.previewUrl} />
                      <div className="muse-batch-file-meta">
                        <span className="muse-batch-filename">{row.image.fileName}</span>
                        {row.hint && <span className="muse-batch-hint">{row.hint}</span>}
                      </div>
                    </div>
                  </td>
                  <td className="col-proj">
                    <div className="muse-batch-stack">
                      <input
                        className="batch-input muse-batch-cell-input"
                        value={row.projectInput}
                        placeholder="_chung"
                        aria-label={`Dự án dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { project: e.target.value })}
                      />
                      <select
                        className="batch-input batch-select muse-batch-cell-input"
                        value={row.type}
                        aria-label={`Loại dòng ${row.index + 1}`}
                        onChange={(e) =>
                          patchOverride(row.index, { type: e.target.value as MuseAssetType })
                        }
                      >
                        {ASSET_TYPES.map((t) => (
                          <option key={t.value} value={t.value}>
                            {t.short}
                          </option>
                        ))}
                      </select>
                    </div>
                  </td>
                  <td className="col-tag">
                    <div className="muse-batch-stack">
                      <div className="muse-tag-input">
                        <span>@</span>
                        <input
                          className="batch-input muse-batch-cell-input"
                          value={row.tag}
                          aria-label={`Tag dòng ${row.index + 1}`}
                          onChange={(e) => patchOverride(row.index, { tag: e.target.value })}
                        />
                      </div>
                      <input
                        className="batch-input muse-batch-cell-input"
                        value={row.name}
                        placeholder="Tên (tuỳ chọn)"
                        aria-label={`Tên dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { name: e.target.value })}
                      />
                    </div>
                  </td>
                  <td className="col-meta">
                    <div className="muse-batch-meta-3">
                      <input
                        className="batch-input muse-batch-cell-input"
                        list="batch-angle-suggest"
                        value={row.angleInput}
                        placeholder="Góc"
                        aria-label={`Góc dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { angle: e.target.value })}
                      />
                      <input
                        className="batch-input muse-batch-cell-input"
                        list="batch-variant-suggest"
                        value={row.variantInput}
                        placeholder="Biến thể"
                        aria-label={`Biến thể dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { variant: e.target.value })}
                      />
                      <input
                        className="batch-input muse-batch-cell-input"
                        list="batch-state-suggest"
                        value={row.stateInput}
                        placeholder="Trạng thái"
                        aria-label={`Trạng thái dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { state: e.target.value })}
                      />
                    </div>
                  </td>
                  <td className="col-role">
                    <div className="muse-batch-stack">
                      <select
                        className="batch-input batch-select muse-batch-cell-input"
                        value={row.role}
                        aria-label={`Vai trò dòng ${row.index + 1}`}
                        onChange={(e) => {
                          const nextRole = e.target.value as MuseRefRole;
                          patchOverride(row.index, {
                            role: nextRole,
                            type: defaultTypeForRole(nextRole),
                          });
                        }}
                      >
                        {roleOrder.map((r) => (
                          <option key={r} value={r}>
                            {roles[r].label}
                          </option>
                        ))}
                      </select>
                      <input
                        className="batch-input muse-batch-cell-input"
                        value={row.lock}
                        placeholder="Khoá chi tiết..."
                        aria-label={`Khoá dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { lock: e.target.value })}
                      />
                    </div>
                  </td>
                  <td className="col-status">
                    <span
                      className={`muse-batch-badge is-${row.status}`}
                      title={row.note ?? STATUS_LABELS[row.status]}
                    >
                      {row.note && row.status === 'rename'
                        ? `→ @${row.resolvedTag}`
                        : STATUS_LABELS[row.status]}
                    </span>
                    <code className="muse-batch-note" title={row.canonicalName}>
                      {row.canonicalName}
                    </code>
                    {row.note && row.status !== 'rename' && (
                      <span className="muse-batch-note" title={row.note}>
                        {row.note}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

function ImportThumbnail({ src }: { src?: string }) {
  const [failedSource, setFailedSource] = useState<string>();
  if (src && failedSource !== src) {
    return (
      <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailedSource(src)} />
    );
  }
  return (
    <span className="muse-batch-thumb-fallback">
      <Icon icon={ImageIcon} size="sm" />
    </span>
  );
}
