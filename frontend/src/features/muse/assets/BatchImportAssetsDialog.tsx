import { useEffect, useMemo, useRef, useState } from 'react';
import type { DragEvent, ReactNode } from 'react';
import '../../../components/batch/BatchForm.css';
import '../../../components/studio/Studio.css';
import './MuseAssetsPage.css';
import { FileUp, FolderOpen, Images, X } from 'lucide-react';
import { Button, Icon, Modal } from '../../../components/ui';
import { api, thumbUrl, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import {
  MuseAssetBatchConflict,
  MuseAssetBatchItem,
  MuseAssetBatchRequest,
  MuseFolderImage,
  MuseRefRole,
} from '../../../types';
import type { MuseAsset } from '../../../types';
import {
  ANGLE_SUGGESTIONS,
  STATE_SUGGESTIONS,
  VARIANT_SUGGESTIONS,
  roleOrder,
  roles,
} from './assetRoles';
import {
  buildBatchRows,
  parseAliasLines,
  parseLockList,
  parseRoleLines,
  type BatchPreviewRow,
  type BatchRowOverride,
  type LockSplitMode,
} from './batchImport';
import { BatchImportTable } from './BatchImportTable';
import type { VaultImportSource } from './SaveToVaultDialog';

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

function summaryMessage(added: number, updated: number, skipped: number): string {
  const parts: string[] = [];
  if (added > 0) parts.push(`thêm ${added}`);
  if (updated > 0) parts.push(`cập nhật ${updated}`);
  if (skipped > 0) parts.push(`bỏ qua ${skipped} trùng`);
  return parts.length > 0 ? `Đã ${parts.join(', ')} tư liệu.` : 'Không có tư liệu nào được nhập.';
}

function initialImagesFromMediaSources(sources?: readonly VaultImportSource[]): MuseFolderImage[] {
  if (!sources?.length) return [];
  return sources.map((s, idx) =>
    MuseFolderImage.createFrom({
      path: '',
      localUrl: s.localUrl,
      fileName: s.suggestedName || `media_${idx + 1}.png`,
      size: 0,
      previewUrl: thumbUrl(s.localUrl),
    }),
  );
}

/**
 * Batch import dialog for adding images (folder with optional subfolders,
 * multiple picked OS files, or in-app generated media URLs) into the Muse
 * Asset Vault, auto-suggesting convention fields from filenames or 3 .txt lists.
 */
export function BatchImportAssetsDialog({
  existingAssets,
  initialMediaSources,
  defaultProject: initialDefaultProject = '',
  onImported,
  onClose,
}: {
  existingAssets: readonly MuseAsset[];
  initialMediaSources?: readonly VaultImportSource[];
  defaultProject?: string;
  onImported: () => Promise<void>;
  onClose: () => void;
}) {
  const addToast = useAppStore((s) => s.addToast);
  const [picking, setPicking] = useState(false);
  const [page, setPage] = useState(0);
  const scanRequest = useRef<string | null>(null);
  useEffect(
    () => () => {
      const id = scanRequest.current;
      if (id) {
        scanRequest.current = null;
        void api.cancelGeneration(id);
      }
    },
    [],
  );
  const [importing, setImporting] = useState(false);
  const [recursive, setRecursive] = useState(true);
  const [folderDir, setFolderDir] = useState('');
  const [images, setImages] = useState<MuseFolderImage[]>(() =>
    initialImagesFromMediaSources(initialMediaSources),
  );
  const [aliasText, setAliasText] = useState('');
  const [roleText, setRoleText] = useState('');
  const [lockText, setLockText] = useState('');
  const [lockMode, setLockMode] = useState<LockSplitMode>('line');
  const [defaultProject, setDefaultProject] = useState(initialDefaultProject);
  const [defaultRole, setDefaultRole] = useState<MuseRefRole>(MuseRefRole.Character);
  const [conflict, setConflict] = useState<MuseAssetBatchConflict>(MuseAssetBatchConflict.Skip);
  const [mergeByAlias, setMergeByAlias] = useState(true);
  const [overrides, setOverrides] = useState<Map<number, BatchRowOverride>>(() => new Map());

  // Bulk assignment controls for checked rows
  const [bulkProject, setBulkProject] = useState('');
  const [bulkTag, setBulkTag] = useState('');
  const [bulkAngle, setBulkAngle] = useState('');

  const lastClickedRow = useRef<number | null>(null);

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
        defaultProject,
        defaultRole,
        conflict,
        mergeByAlias,
        existingAssets,
        overrides,
      }),
    [
      images,
      aliases,
      parsedRoles,
      locks,
      defaultProject,
      defaultRole,
      conflict,
      mergeByAlias,
      existingAssets,
      overrides,
    ],
  );

  const actionableRows = useMemo(
    () =>
      rows.filter((r) => r.status === 'ready' || r.status === 'rename' || r.status === 'overwrite'),
    [rows],
  );
  const errorCount = useMemo(
    () => rows.filter((r) => !r.excluded && r.status === 'error').length,
    [rows],
  );
  const skippedCount = useMemo(
    () => rows.filter((r) => !r.excluded && r.status === 'skip').length,
    [rows],
  );
  const selectableRows = useMemo(() => rows.filter((r) => !r.image.error), [rows]);
  const includedRows = useMemo(() => rows.filter((r) => !r.excluded && !r.image.error), [rows]);
  const allSelectableIncluded =
    selectableRows.length > 0 && selectableRows.every((r) => !r.excluded);

  const patchOverride = (index: number, patch: Partial<BatchRowOverride>) => {
    setOverrides((prev) => {
      const next = new Map(prev);
      next.set(index, { ...next.get(index), ...patch });
      return next;
    });
  };

  const applyToIncludedRows = (patchFn: (row: BatchPreviewRow) => Partial<BatchRowOverride>) => {
    setOverrides((prev) => {
      const next = new Map(prev);
      for (const r of includedRows) {
        next.set(r.index, { ...next.get(r.index), ...patchFn(r) });
      }
      return next;
    });
  };

  const cancelScan = () => {
    const id = scanRequest.current;
    if (!id) return;
    scanRequest.current = null;
    setPicking(false);
    void api.cancelGeneration(id);
  };

  const pickImages = async (fromFolder: boolean) => {
    if (scanRequest.current || importing) return;
    const id = crypto.randomUUID();
    scanRequest.current = id;
    setPicking(true);
    try {
      const scan = unwrap(
        await (fromFolder ? api.pickMuseAssetFolder(recursive, id) : api.pickMuseAssetFiles(id)),
      );
      if (scanRequest.current !== id || scan.canceled) return;
      setFolderDir(scan.dir);
      setImages(scan.images ?? []);
      setOverrides(new Map());
      setPage(0);
      lastClickedRow.current = null;
      if ((scan.images ?? []).length === 0) {
        addToast({ type: 'warning', message: 'Không tìm thấy ảnh nào Muse nhận.' });
      }
    } catch (cause) {
      if (scanRequest.current === id) {
        addToast({
          type: 'error',
          message: cause instanceof Error ? cause.message : String(cause),
        });
      }
    } finally {
      if (scanRequest.current === id) {
        scanRequest.current = null;
        setPicking(false);
      }
    }
  };

  const closeDialog = () => {
    if (importing) return;
    cancelScan();
    onClose();
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
          sourcePath: row.image.path || undefined,
          sourceUrl: row.image.localUrl || undefined,
          project: row.project,
          type: row.type,
          tag: row.resolvedTag || row.tag,
          name: row.name,
          role: row.role,
          lock: row.lock,
          angle: row.angle,
          variant: row.variant,
          state: row.state,
          version: row.version,
          primary: row.primary,
          rejected: row.rejected,
        }),
      );
      const res = unwrap(
        await api.importMuseAssetBatch(
          MuseAssetBatchRequest.createFrom({ items, conflict, mergeByAlias }),
        ),
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
      title="Nhập hàng loạt vào Kho Tư liệu"
      subtitle="Chọn thư mục hoặc nhiều ảnh; app tự nhận quy ước tên (hoặc dán .txt / gán nhanh) rồi tự chuẩn hoá tên khi lưu vào Kho"
      className="muse-batch-dialog"
      onClose={closeDialog}
      footer={
        <div className="muse-batch-footer">
          <span className="muse-batch-summary">
            {images.length === 0
              ? 'Chọn thư mục hoặc nhiều ảnh để bắt đầu'
              : `Sẵn sàng nhập ${actionableRows.length}/${images.length} ảnh${
                  skippedCount > 0 ? ` · bỏ qua ${skippedCount} trùng` : ''
                }${errorCount > 0 ? ` · ${errorCount} lỗi` : ''}`}
          </span>
          <div className="muse-dialog-actions">
            <Button onClick={closeDialog} disabled={importing}>
              Huỷ
            </Button>
            <Button
              variant="primary"
              disabled={picking || importing || actionableRows.length === 0}
              onClick={() => void submit()}
            >
              {importing ? 'Đang nhập…' : `Nhập ${actionableRows.length} ảnh vào Kho`}
            </Button>
          </div>
        </div>
      }
    >
      <div className="muse-batch-body">
        {!initialMediaSources?.length && (
          <div className="muse-batch-folder-row">
            <div className="batch-folder-control">
              <output className={folderDir ? undefined : 'is-empty'} title={folderDir}>
                {folderDir || 'Chưa chọn thư mục hoặc ảnh bên ngoài'}
              </output>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={picking || importing}
                onClick={() => void pickImages(true)}
              >
                <Icon icon={FolderOpen} size="sm" />
                <span>{picking ? 'Đang quét…' : 'Chọn thư mục…'}</span>
              </Button>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={picking || importing}
                onClick={() => void pickImages(false)}
              >
                <Icon icon={Images} size="sm" />
                <span>Chọn nhiều ảnh…</span>
              </Button>
            </div>
            <div className="muse-batch-folder-meta">
              <label className="muse-check">
                <input
                  type="checkbox"
                  checked={recursive}
                  onChange={(e) => setRecursive(e.target.checked)}
                />
                <span>Quét cả thư mục con (lọc theo dự án / góc)</span>
              </label>
              {images.length > 0 && (
                <span className="studio-field-hint">Tìm thấy {images.length} ảnh.</span>
              )}
            </div>
          </div>
        )}

        <div className="muse-batch-txt-grid">
          <TxtInputBox
            id="batch-alias"
            label="1. Alias / Tag"
            count={aliases.length}
            value={aliasText}
            placeholder={'lan\nhop | Hộp quà Tết\nCô gái áo đỏ'}
            hint="Mỗi dòng 1 tag hoặc `tag | Tên`. Trống = lấy theo tên file."
            onChange={(next) => setAliasText(next)}
          />
          <TxtInputBox
            id="batch-role"
            label="2. Vai trò"
            count={parsedRoles.length}
            value={roleText}
            placeholder={'nhân vật\nsản phẩm\nkhuôn mặt'}
            hint="Vd: nhân vật, mặt, thú, sản phẩm, đạo cụ, xe, tài liệu, bối cảnh, phong cách."
            onChange={(next) => setRoleText(next)}
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
                onChange={(e) => setLockMode(e.target.value as LockSplitMode)}
              >
                {LOCK_MODE_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            }
            onChange={(next) => setLockText(next)}
          />
        </div>

        <div className="muse-batch-options">
          <label className="studio-field-group">
            <span className="studio-field-label">Dự án mặc định</span>
            <input
              className="batch-input"
              value={defaultProject}
              placeholder="Vd: OLIVA (để trống = _chung)"
              onChange={(e) => setDefaultProject(e.target.value)}
            />
          </label>
          <label className="studio-field-group">
            <span className="studio-field-label">Vai trò mặc định</span>
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
            <span className="studio-field-label">Khi trùng @tag (nếu không gộp)</span>
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

        <label className="muse-check">
          <input
            type="checkbox"
            checked={mergeByAlias}
            onChange={(e) => setMergeByAlias(e.target.checked)}
          />
          <span>
            Gộp các ảnh cùng Dự án + @tag thành nhiều góc / biến thể của 1 tư liệu (khuyên dùng cho
            bộ nhân vật / bối cảnh)
          </span>
        </label>

        {includedRows.length > 0 && (
          <div className="muse-batch-quick-bar">
            <span className="studio-field-label">Gán nhanh {includedRows.length} ảnh đã chọn:</span>
            <div className="muse-batch-quick-controls">
              <input
                className="batch-input muse-batch-quick-input"
                placeholder="Dự án (OLIVA)"
                value={bulkProject}
                onChange={(e) => setBulkProject(e.target.value)}
              />
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={() => applyToIncludedRows(() => ({ project: bulkProject }))}
              >
                Gán dự án
              </Button>
              <input
                className="batch-input muse-batch-quick-input"
                placeholder="Tag chung (jimena)"
                value={bulkTag}
                onChange={(e) => setBulkTag(e.target.value)}
              />
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={!bulkTag.trim()}
                onClick={() => applyToIncludedRows(() => ({ tag: bulkTag.trim() }))}
              >
                Gán @tag
              </Button>
              <input
                className="batch-input muse-batch-quick-input"
                placeholder="Góc (front/close)"
                list="batch-angle-suggest"
                value={bulkAngle}
                onChange={(e) => setBulkAngle(e.target.value)}
              />
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={() => applyToIncludedRows(() => ({ angle: bulkAngle }))}
              >
                Gán góc
              </Button>
            </div>
          </div>
        )}

        <datalist id="batch-angle-suggest">
          {ANGLE_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <datalist id="batch-variant-suggest">
          {VARIANT_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <datalist id="batch-state-suggest">
          {STATE_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>

        {warnings.length > 0 && (
          <div className="muse-alert muse-alert-warning" role="status">
            <span>{warnings.join(' ')}</span>
          </div>
        )}

        {picking && (
          <Button size="sm" variant="secondary" onClick={cancelScan}>
            Huỷ quét ảnh
          </Button>
        )}

        <BatchImportTable
          rows={rows}
          page={page}
          setPage={setPage}
          selectableCount={selectableRows.length}
          allSelectableIncluded={allSelectableIncluded}
          toggleRow={toggleRow}
          toggleAllRows={toggleAllRows}
          patchOverride={patchOverride}
        />
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
        rows={3}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
      />
      <span className="studio-field-hint">{hint}</span>
    </div>
  );
}
