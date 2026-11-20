import { useMemo, useRef, useState } from 'react';
import type { DragEvent, ReactNode } from 'react';
import { FileUp, FolderOpen, Image as ImageIcon, X } from 'lucide-react';
import { Button, Icon, Modal } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import {
  MuseAssetBatchConflict,
  MuseAssetBatchItem,
  MuseAssetBatchRequest,
  MuseRefRole,
} from '../../../types';
import type { MuseAsset, MuseFolderImage } from '../../../types';
import { roleOrder, roles } from './assetRoles';
import {
  buildBatchRows,
  parseAliasLines,
  parseLockList,
  parseRoleLines,
  type BatchPreviewRow,
  type BatchRowOverride,
  type LockSplitMode,
} from './batchImport';

const CONFLICT_OPTIONS: readonly { value: MuseAssetBatchConflict; label: string }[] = [
  { value: MuseAssetBatchConflict.Skip, label: 'Bỏ qua tag trùng' },
  { value: MuseAssetBatchConflict.Overwrite, label: 'Ghi đè tư liệu cũ' },
  { value: MuseAssetBatchConflict.Rename, label: 'Tự đổi tên (_2, _3)' },
];

const LOCK_MODE_OPTIONS: readonly { value: LockSplitMode; label: string }[] = [
  { value: 'line', label: 'Từng dòng' },
  { value: 'positional', label: 'Giữ dòng trống' },
  { value: 'block', label: 'Theo đoạn' },
];

const STATUS_LABELS: Record<BatchPreviewRow['status'], string> = {
  ready: 'Mới',
  rename: 'Đổi tên',
  overwrite: 'Ghi đè',
  skip: 'Bỏ qua',
  excluded: 'Đã bỏ',
  error: 'Lỗi',
};

function summaryMessage(added: number, updated: number, skipped: number): string {
  const parts: string[] = [];
  if (added > 0) parts.push(`thêm ${added}`);
  if (updated > 0) parts.push(`cập nhật ${updated}`);
  if (skipped > 0) parts.push(`bỏ qua ${skipped} trùng`);
  return parts.length > 0 ? `Đã ${parts.join(', ')} tư liệu.` : 'Không có tư liệu nào được nhập.';
}

/**
 * Batch import dialog for adding a flat image folder + 3 top-to-bottom .txt
 * lists (alias/tag, role, lock prompt) with live preview and quick edits.
 */
export function BatchImportAssetsDialog({
  existingAssets,
  onImported,
  onClose,
}: {
  existingAssets: readonly MuseAsset[];
  onImported: () => Promise<void>;
  onClose: () => void;
}) {
  const addToast = useAppStore((s) => s.addToast);
  const [picking, setPicking] = useState(false);
  const [importing, setImporting] = useState(false);
  const [folderDir, setFolderDir] = useState('');
  const [images, setImages] = useState<MuseFolderImage[]>([]);
  const [aliasText, setAliasText] = useState('');
  const [roleText, setRoleText] = useState('');
  const [lockText, setLockText] = useState('');
  const [lockMode, setLockMode] = useState<LockSplitMode>('line');
  const [defaultRole, setDefaultRole] = useState<MuseRefRole>(MuseRefRole.Character);
  const [conflict, setConflict] = useState<MuseAssetBatchConflict>(MuseAssetBatchConflict.Skip);
  const [overrides, setOverrides] = useState<Map<number, BatchRowOverride>>(() => new Map());
  const lastClickedRow = useRef<number | null>(null);
  const shiftHeld = useRef(false);

  const aliases = useMemo(() => parseAliasLines(aliasText), [aliasText]);
  const parsedRoles = useMemo(() => parseRoleLines(roleText), [roleText]);
  const locks = useMemo(() => parseLockList(lockText, lockMode), [lockText, lockMode]);

  const { rows, warnings } = useMemo(
    () =>
      buildBatchRows({
        images,
        aliases,
        roles: parsedRoles,
        locks,
        defaultRole,
        conflict,
        existingAssets,
        overrides,
      }),
    [images, aliases, parsedRoles, locks, defaultRole, conflict, existingAssets, overrides],
  );

  const actionableRows = useMemo(
    () =>
      rows.filter((r) => r.status === 'ready' || r.status === 'rename' || r.status === 'overwrite'),
    [rows],
  );
  const errorCount = useMemo(() => rows.filter((r) => r.status === 'error').length, [rows]);
  const skippedCount = useMemo(() => rows.filter((r) => r.status === 'skip').length, [rows]);
  const selectableRows = useMemo(() => rows.filter((r) => !r.image.error), [rows]);
  const allSelectableIncluded =
    selectableRows.length > 0 && selectableRows.every((r) => !r.excluded);

  const patchOverride = (index: number, patch: Partial<BatchRowOverride>) => {
    setOverrides((prev) => {
      const next = new Map(prev);
      next.set(index, { ...next.get(index), ...patch });
      return next;
    });
  };

  const clearOverrideField = (field: keyof BatchRowOverride) => {
    setOverrides((prev) => {
      if (prev.size === 0) return prev;
      const next = new Map<number, BatchRowOverride>();
      for (const [idx, ov] of prev.entries()) {
        const rest = { ...ov };
        delete rest[field];
        if (Object.keys(rest).length > 0) next.set(idx, rest);
      }
      return next;
    });
  };

  const pickFolder = async () => {
    if (picking || importing) return;
    setPicking(true);
    try {
      const scan = unwrap(await api.pickMuseAssetFolder());
      if (scan.canceled) return;
      setFolderDir(scan.dir);
      setImages(scan.images ?? []);
      setOverrides(new Map());
      lastClickedRow.current = null;
      if ((scan.images ?? []).length === 0) {
        addToast({ type: 'warning', message: 'Thư mục này không có ảnh nào Muse nhận.' });
      }
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      setPicking(false);
    }
  };

  const toggleRow = (index: number, shiftKey: boolean) => {
    const currentExcluded = rows[index]?.excluded ?? false;
    const nextExcluded = !currentExcluded;
    setOverrides((prev) => {
      const next = new Map(prev);
      if (shiftKey && lastClickedRow.current !== null) {
        const from = Math.min(lastClickedRow.current, index);
        const to = Math.max(lastClickedRow.current, index);
        for (let i = from; i <= to; i++) {
          if (!rows[i]?.image.error) {
            next.set(i, { ...next.get(i), excluded: nextExcluded });
          }
        }
      } else {
        next.set(index, { ...next.get(index), excluded: nextExcluded });
      }
      return next;
    });
    lastClickedRow.current = index;
  };

  const toggleAllRows = () => {
    const nextExcluded = allSelectableIncluded;
    setOverrides((prev) => {
      const next = new Map(prev);
      for (const r of selectableRows) {
        next.set(r.index, { ...next.get(r.index), excluded: nextExcluded });
      }
      return next;
    });
  };

  const submit = async () => {
    if (importing || actionableRows.length === 0) return;
    setImporting(true);
    try {
      const items = actionableRows.map((row) =>
        MuseAssetBatchItem.createFrom({
          sourcePath: row.image.path,
          tag: row.tag,
          name: row.name,
          role: row.role,
          lock: row.lock,
        }),
      );
      const res = unwrap(
        await api.importMuseAssetBatch(MuseAssetBatchRequest.createFrom({ items, conflict })),
      );
      await onImported();
      addToast({
        type: 'success',
        message: summaryMessage(res.added, res.updated, res.skipped + skippedCount),
      });
      onClose();
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      setImporting(false);
    }
  };

  return (
    <Modal
      title="Thêm nhanh tư liệu từ thư mục"
      subtitle="Ảnh nằm chung 1 thư mục (tự xếp thứ tự như Windows Explorer) khớp từ trên xuống với 3 danh sách .txt"
      className="muse-batch-dialog"
      onClose={onClose}
      footer={
        <div className="muse-batch-footer">
          <span className="muse-batch-summary">
            {images.length === 0
              ? 'Chọn thư mục ảnh để bắt đầu'
              : `Sẵn sàng nhập ${actionableRows.length}/${images.length} ảnh${
                  skippedCount > 0 ? ` · bỏ qua ${skippedCount} trùng` : ''
                }${errorCount > 0 ? ` · ${errorCount} lỗi` : ''}`}
          </span>
          <div className="muse-dialog-actions">
            <Button onClick={onClose} disabled={importing}>
              Huỷ
            </Button>
            <Button
              variant="primary"
              disabled={importing || actionableRows.length === 0}
              onClick={() => void submit()}
            >
              {importing ? 'Đang nhập…' : `Nhập ${actionableRows.length} tư liệu`}
            </Button>
          </div>
        </div>
      }
    >
      <div className="muse-batch-body">
        <div className="muse-batch-folder-row">
          <div className="batch-folder-control">
            <output className={folderDir ? undefined : 'is-empty'} title={folderDir}>
              {folderDir || 'Chưa chọn thư mục ảnh tư liệu'}
            </output>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              disabled={picking || importing}
              onClick={() => void pickFolder()}
            >
              <Icon icon={FolderOpen} size="sm" />
              <span>{picking ? 'Đang quét…' : 'Chọn thư mục…'}</span>
            </Button>
          </div>
          {images.length > 0 && (
            <span className="studio-field-hint">
              Tìm thấy {images.length} ảnh theo thứ tự tên tự nhiên (1, 2, …, 10).
            </span>
          )}
        </div>

        <div className="muse-batch-txt-grid">
          <TxtInputBox
            id="batch-alias"
            label="1. Alias / Tag"
            count={aliases.length}
            value={aliasText}
            placeholder={'lan\nhop | Hộp quà Tết\nCô gái áo đỏ'}
            hint="Mỗi dòng 1 tag hoặc `tag | Tên`. Trống = lấy theo tên file."
            onChange={(next) => {
              setAliasText(next);
              clearOverrideField('tag');
              clearOverrideField('name');
            }}
          />
          <TxtInputBox
            id="batch-role"
            label="2. Vai trò"
            count={parsedRoles.length}
            value={roleText}
            placeholder={'nhân vật\nsản phẩm\nkhuôn mặt'}
            hint="Vd: nhân vật, mặt, sản phẩm, đạo cụ, bối cảnh, phong cách, storyboard."
            onChange={(next) => {
              setRoleText(next);
              clearOverrideField('role');
            }}
          />
          <TxtInputBox
            id="batch-lock"
            label="3. Prompts khoá"
            count={locks.length}
            value={lockText}
            placeholder={'nốt ruồi má trái, áo dài trắng\nchữ in rõ nét, nắp vàng'}
            hint="Thứ tự khớp từ trên xuống tương ứng với từng ảnh."
            headerExtra={
              <select
                aria-label="Kiểu tách prompt khoá"
                className="batch-input batch-select muse-batch-mini-select"
                value={lockMode}
                onChange={(e) => {
                  setLockMode(e.target.value as LockSplitMode);
                  clearOverrideField('lock');
                }}
              >
                {LOCK_MODE_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            }
            onChange={(next) => {
              setLockText(next);
              clearOverrideField('lock');
            }}
          />
        </div>

        <div className="muse-batch-options">
          <label className="studio-field-group">
            <span className="studio-field-label">Vai trò mặc định (khi dòng trống)</span>
            <select
              className="batch-input batch-select"
              value={defaultRole}
              onChange={(e) => setDefaultRole(e.target.value as MuseRefRole)}
            >
              {roleOrder.map((r) => (
                <option key={r} value={r}>
                  {roles[r].label}
                </option>
              ))}
            </select>
          </label>
          <label className="studio-field-group">
            <span className="studio-field-label">Khi trùng @tag</span>
            <select
              className="batch-input batch-select"
              value={conflict}
              onChange={(e) => setConflict(e.target.value as MuseAssetBatchConflict)}
            >
              {CONFLICT_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </label>
        </div>

        {warnings.length > 0 && (
          <div className="muse-alert muse-alert-warning" role="status">
            <span>{warnings.join(' ')}</span>
          </div>
        )}

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
                      disabled={selectableRows.length === 0}
                      onChange={toggleAllRows}
                    />
                  </th>
                  <th className="col-num">#</th>
                  <th className="col-img">Ảnh</th>
                  <th className="col-tag">Tag</th>
                  <th className="col-name">Tên (alias)</th>
                  <th className="col-role">Vai trò</th>
                  <th className="col-lock">Prompt khoá</th>
                  <th className="col-status">Trạng thái</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr
                    key={row.image.path}
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
                      <div className="muse-batch-file" title={row.image.fileName}>
                        {row.image.previewUrl ? (
                          <img src={row.image.previewUrl} alt="" decoding="async" />
                        ) : (
                          <span className="muse-batch-thumb-fallback">
                            <Icon icon={ImageIcon} size="sm" />
                          </span>
                        )}
                        <span className="muse-batch-filename">{row.image.fileName}</span>
                      </div>
                    </td>
                    <td className="col-tag">
                      <div className="muse-tag-input">
                        <span>@</span>
                        <input
                          className="batch-input muse-batch-cell-input"
                          value={row.tag}
                          aria-label={`Tag dòng ${row.index + 1}`}
                          onChange={(e) => patchOverride(row.index, { tag: e.target.value })}
                        />
                      </div>
                    </td>
                    <td className="col-name">
                      <input
                        className="batch-input muse-batch-cell-input"
                        value={row.name}
                        placeholder="Tuỳ chọn"
                        aria-label={`Tên dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { name: e.target.value })}
                      />
                    </td>
                    <td className="col-role">
                      <select
                        className="batch-input batch-select muse-batch-cell-input"
                        value={row.role}
                        aria-label={`Vai trò dòng ${row.index + 1}`}
                        onChange={(e) =>
                          patchOverride(row.index, { role: e.target.value as MuseRefRole })
                        }
                      >
                        {roleOrder.map((r) => (
                          <option key={r} value={r}>
                            {roles[r].label}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td className="col-lock">
                      <input
                        className="batch-input muse-batch-cell-input"
                        value={row.lock}
                        placeholder="Không khoá"
                        aria-label={`Khoá dòng ${row.index + 1}`}
                        onChange={(e) => patchOverride(row.index, { lock: e.target.value })}
                      />
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
      </div>
    </Modal>
  );
}

function TxtInputBox({
  id,
  label,
  count,
  value,
  placeholder,
  hint,
  headerExtra,
  onChange,
}: {
  id: string;
  label: string;
  count: number;
  value: string;
  placeholder: string;
  hint: string;
  headerExtra?: ReactNode;
  onChange: (value: string) => void;
}) {
  const fileRef = useRef<HTMLInputElement>(null);
  const addToast = useAppStore((s) => s.addToast);
  const [dragging, setDragging] = useState(false);

  const loadFile = async (file: File | undefined) => {
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.txt')) {
      addToast({ type: 'error', message: 'Chỉ hỗ trợ tệp .txt.' });
      return;
    }
    try {
      onChange(await file.text());
    } catch {
      addToast({ type: 'error', message: `Không đọc được tệp ${file.name}.` });
    }
  };

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setDragging(false);
    void loadFile(event.dataTransfer.files?.[0]);
  };

  return (
    <div
      className={`muse-batch-txt-box${dragging ? ' is-dragging' : ''}`}
      onDragOver={(e) => {
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={handleDrop}
    >
      <div className="muse-batch-txt-head">
        <label className="studio-field-label" htmlFor={id}>
          {label}
          <span className="muse-assets-count">{count}</span>
        </label>
        <div className="muse-batch-txt-actions">
          {headerExtra}
          <input
            ref={fileRef}
            type="file"
            accept=".txt,text/plain"
            hidden
            onChange={(e) => {
              const file = e.currentTarget.files?.[0];
              e.currentTarget.value = '';
              void loadFile(file);
            }}
          />
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => fileRef.current?.click()}
            title="Chọn file .txt (hoặc kéo thả vào ô)"
          >
            <Icon icon={FileUp} size="sm" />
            <span>.txt</span>
          </Button>
          {value && (
            <button
              type="button"
              className="muse-batch-clear-btn"
              aria-label={`Xoá ${label}`}
              onClick={() => onChange('')}
            >
              <Icon icon={X} size="sm" />
            </button>
          )}
        </div>
      </div>
      <textarea
        id={id}
        className="batch-input batch-textarea"
        rows={4}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
      />
      <span className="studio-field-hint">{hint}</span>
    </div>
  );
}
