import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { FileUp, X } from 'lucide-react';
import { Button } from '../../../../components/ui';
import { parsePromptList } from './promptList';
import './PromptListModal.css';

export function PromptListModal({
  onClose,
  onAdd,
}: {
  onClose: () => void;
  onAdd: (prompts: string[]) => void;
}) {
  const [text, setText] = useState('');
  const [fileError, setFileError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const textAreaRef = useRef<HTMLTextAreaElement>(null);
  const prompts = parsePromptList(text);

  useEffect(() => {
    textAreaRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

  const onFileChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = '';
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.txt')) {
      setFileError('Chỉ hỗ trợ tệp .txt.');
      return;
    }
    try {
      setText(await file.text());
      setFileError(null);
    } catch {
      setFileError('Không đọc được tệp. Hãy thử lại.');
    }
  };

  return createPortal(
    <div className="prompt-list-modal-overlay" onMouseDown={onClose}>
      <section
        className="prompt-list-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="flow-image-prompt-list-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header className="prompt-list-modal-header">
          <div>
            <h2 id="flow-image-prompt-list-title">Nhập danh sách prompt</h2>
            <p>Mỗi prompt cách nhau bằng một dòng trắng.</p>
          </div>
          <button
            type="button"
            className="prompt-list-modal-close"
            aria-label="Đóng"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </header>

        <div className="prompt-list-modal-body">
          <input
            ref={fileInputRef}
            type="file"
            accept=".txt,text/plain"
            hidden
            onChange={(event) => void onFileChange(event)}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => fileInputRef.current?.click()}
          >
            <FileUp size={16} />
            <span>Tải từ file (.txt)</span>
          </Button>
          {fileError && (
            <p className="prompt-list-modal-error" role="alert">
              {fileError}
            </p>
          )}
          <label className="prompt-list-textarea-label" htmlFor="flow-image-prompt-list">
            Danh sách prompt
          </label>
          <textarea
            ref={textAreaRef}
            id="flow-image-prompt-list"
            className="prompt-list-textarea"
            value={text}
            onChange={(event) => setText(event.currentTarget.value)}
            placeholder={'Ví dụ:\nPrompt 1 dòng 1\nPrompt 1 dòng 2\n\nPrompt 2...'}
            aria-describedby="flow-image-prompt-list-help"
          />
          <div className="prompt-list-modal-help" id="flow-image-prompt-list-help">
            <span>Một dòng trống sẽ bắt đầu prompt mới.</span>
            <span>{prompts.length} prompt</span>
          </div>
        </div>

        <footer className="prompt-list-modal-footer">
          <Button type="button" variant="outline" onClick={onClose}>
            Hủy
          </Button>
          <Button
            type="button"
            variant="primary"
            disabled={prompts.length === 0}
            onClick={() => onAdd(prompts)}
          >
            Thêm vào Queue
          </Button>
        </footer>
      </section>
    </div>,
    document.body,
  );
}
