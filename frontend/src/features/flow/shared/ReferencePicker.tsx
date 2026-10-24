import { useRef } from 'react';
import { ImagePlus, X } from 'lucide-react';
import { useAppStore } from '../../../stores';
import type { StudioConfig } from '../../../types';
import { formatBytes } from '../../../utils';

/** A local file picked as reference, kept until generation uploads it. */
export interface ReferenceImage {
  base64: string;
  name: string;
  mimeType: string;
  /** Blob URL owned by the store, released when the image is removed. */
  previewUrl: string;
}

/** Reference images in a studio store (image and video share them). */
export interface ReferenceSlice {
  referenceImages: ReferenceImage[];
  addReferenceImages: (images: ReferenceImage[]) => void;
  removeReferenceImage: (previewUrl: string) => void;
}

type Setter<S> = (partial: Partial<S> | ((state: S) => Partial<S>)) => void;

export function createReferenceSlice<S extends ReferenceSlice>(set: Setter<S>): ReferenceSlice {
  return {
    referenceImages: [],
    addReferenceImages: (images) =>
      set((state) => ({ referenceImages: [...state.referenceImages, ...images] }) as Partial<S>),
    removeReferenceImage: (previewUrl) =>
      set((state) => {
        URL.revokeObjectURL(previewUrl);
        return {
          referenceImages: state.referenceImages.filter((img) => img.previewUrl !== previewUrl),
        } as Partial<S>;
      }),
  };
}

const readReference = (file: File): Promise<ReferenceImage> =>
  new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      resolve({
        base64: result.slice(result.indexOf(',') + 1),
        name: file.name,
        mimeType: file.type,
        previewUrl: URL.createObjectURL(file),
      });
    };
    reader.onerror = () => reject(reader.error ?? new Error(`Không đọc được ${file.name}`));
    reader.readAsDataURL(file);
  });

/**
 * Reference image field: thumbnails, remove buttons and a picker limited by the
 * selected model. Formats and file size come from the backend's studio config.
 * Hidden when the model takes no references and none are left to remove.
 */
export function ReferencePicker({
  config,
  limit,
  images,
  onAdd,
  onRemove,
}: {
  config: StudioConfig;
  /** References the selected model accepts; 0 = none. */
  limit: number;
  images: ReferenceImage[];
  onAdd: (images: ReferenceImage[]) => void;
  onRemove: (previewUrl: string) => void;
}) {
  const addToast = useAppStore((s) => s.addToast);
  const inputRef = useRef<HTMLInputElement>(null);
  const overLimit = images.length - limit;

  if (limit === 0 && images.length === 0) return null;

  const addFiles = async (files: File[]) => {
    const skipped: string[] = [];
    const accepted: File[] = [];
    for (const file of files) {
      if (!config.referenceMimeTypes.includes(file.type))
        skipped.push(`${file.name}: định dạng không hỗ trợ`);
      else if (file.size > config.maxReferenceBytes)
        skipped.push(`${file.name}: lớn hơn ${formatBytes(config.maxReferenceBytes, 0)}`);
      else if (images.length + accepted.length >= limit) skipped.push(`${file.name}: đã đủ ảnh`);
      else accepted.push(file);
    }
    try {
      onAdd(await Promise.all(accepted.map(readReference)));
    } catch (cause) {
      skipped.push(cause instanceof Error ? cause.message : String(cause));
    }
    if (skipped.length > 0) {
      addToast({
        type: 'warning',
        title: 'Một số ảnh không được thêm',
        message: skipped.join('; '),
      });
    }
  };

  return (
    <div className="flow-field-group">
      <label className="flow-field-label">
        <span>Ảnh tham chiếu</span>
        <span className="flow-field-badge">
          {images.length}/{limit}
        </span>
      </label>
      <input
        ref={inputRef}
        type="file"
        accept={config.referenceMimeTypes.join(',')}
        multiple
        hidden
        onChange={(e) => {
          const files = Array.from(e.target.files ?? []);
          e.target.value = '';
          void addFiles(files);
        }}
      />
      {images.length > 0 && (
        <div className="reference-grid">
          {images.map((img) => (
            <div key={img.previewUrl} className="reference-thumb" title={img.name}>
              <img src={img.previewUrl} alt={img.name} />
              <button
                type="button"
                className="reference-thumb-remove"
                onClick={() => onRemove(img.previewUrl)}
                title={`Bỏ ${img.name}`}
              >
                <X size={12} />
              </button>
            </div>
          ))}
        </div>
      )}
      {images.length < limit && (
        <button
          type="button"
          className="reference-dropzone"
          onClick={() => inputRef.current?.click()}
        >
          <ImagePlus size={20} className="reference-dropzone-icon" />
          <span className="reference-dropzone-text">Thêm ảnh tham chiếu</span>
        </button>
      )}
      {overLimit > 0 && (
        <p className="reference-limit-warning" role="alert">
          {limit > 0
            ? `Model này nhận tối đa ${limit} ảnh tham chiếu; hãy bỏ bớt ${overLimit} ảnh.`
            : 'Model này không nhận ảnh tham chiếu; hãy bỏ các ảnh đã chọn.'}
        </p>
      )}
    </div>
  );
}
