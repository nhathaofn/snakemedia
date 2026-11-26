import { useState } from 'react';
import { Copy, FileText, Pencil, Plus, Trash2 } from 'lucide-react';
import { Button, Icon, IconAction, Modal, confirmAction } from '../../../components/ui';
import { useAppStore } from '../../../stores';
import { MusePromptTemplate } from '../../../types';

const emptyTemplate = (): MusePromptTemplate =>
  MusePromptTemplate.createFrom({ id: '', name: '', text: '', createdAt: '' });

interface TemplateSectionProps {
  templates: readonly MusePromptTemplate[];
  onSave: (template: MusePromptTemplate) => Promise<boolean>;
  onRemove: (id: string) => Promise<boolean>;
}

/** Saved prompt templates section + create/edit dialog, with quick copy-to-clipboard action. */
export function TemplateSection({ templates, onSave, onRemove }: TemplateSectionProps) {
  const [editing, setEditing] = useState<MusePromptTemplate | null>(null);
  const addToast = useAppStore((s) => s.addToast);

  const copyText = (t: MusePromptTemplate) => {
    void navigator.clipboard
      .writeText(t.text)
      .then(() => addToast({ type: 'success', message: `Đã chép mẫu "${t.name}".` }))
      .catch(() => addToast({ type: 'error', message: 'Không chép được mẫu.' }));
  };

  return (
    <section className="muse-assets-section">
      <div className="muse-header">
        <h2 className="muse-assets-section-title">
          Mẫu prompt <span className="muse-assets-count">{templates.length}</span>
        </h2>
        <Button size="sm" onClick={() => setEditing(emptyTemplate())}>
          <Icon icon={Plus} size="sm" />
          <span>Thêm mẫu</span>
        </Button>
      </div>

      {templates.length === 0 ? (
        <div className="muse-assets-empty">
          <Icon icon={FileText} size="lg" />
          <strong>Chưa có mẫu prompt</strong>
          <span>Lưu những prompt hay dùng để chèn nhanh ở màn Đơn hoặc Hàng loạt.</span>
        </div>
      ) : (
        <div className="muse-templates-grid">
          {templates.map((t) => (
            <article key={t.id} className="muse-template-card">
              <div className="muse-template-head">
                <strong>{t.name}</strong>
                <div className="muse-template-actions">
                  <IconAction icon={Copy} label="Chép nội dung" onClick={() => copyText(t)} />
                  <IconAction icon={Pencil} label="Sửa" onClick={() => setEditing(t)} />
                  <IconAction
                    icon={Trash2}
                    label="Xoá"
                    onClick={() => {
                      void (async () => {
                        if (
                          !(await confirmAction({
                            title: 'Xoá mẫu prompt?',
                            message: `Xoá mẫu "${t.name}"?`,
                            confirmLabel: 'Xoá',
                            danger: true,
                          }))
                        )
                          return;
                        await onRemove(t.id);
                      })();
                    }}
                  />
                </div>
              </div>
              <p className="muse-template-text">{t.text}</p>
            </article>
          ))}
        </div>
      )}

      {editing && (
        <TemplateDialog
          template={editing}
          onClose={() => setEditing(null)}
          onSave={async (t) => {
            const ok = await onSave(MusePromptTemplate.createFrom(t));
            if (ok) setEditing(null);
            return ok;
          }}
        />
      )}
    </section>
  );
}

/** Add or edit one saved prompt. */
export function TemplateDialog({
  template,
  onSave,
  onClose,
}: {
  template: MusePromptTemplate;
  onSave: (template: MusePromptTemplate) => Promise<boolean>;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState(template);
  const [saving, setSaving] = useState(false);
  const handleSave = async () => {
    if (saving) return;
    setSaving(true);
    try {
      await onSave(draft);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal
      title={template.id ? 'Sửa mẫu prompt' : 'Thêm mẫu prompt'}
      onClose={() => !saving && onClose()}
      footer={
        <div className="muse-dialog-actions">
          <Button onClick={onClose} disabled={saving}>
            Huỷ
          </Button>
          <Button variant="primary" disabled={saving} onClick={() => void handleSave()}>
            {saving ? 'Đang lưu…' : 'Lưu mẫu'}
          </Button>
        </div>
      }
    >
      <div className="muse-asset-form">
        <label className="studio-field-group">
          <span className="studio-field-label">Tên mẫu</span>
          <input
            className="batch-input"
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          />
        </label>
        <label className="studio-field-group">
          <span className="studio-field-label">Nội dung (có thể chứa @tag)</span>
          <textarea
            className="batch-input"
            rows={8}
            value={draft.text}
            onChange={(e) => setDraft({ ...draft, text: e.target.value })}
          />
        </label>
      </div>
    </Modal>
  );
}
