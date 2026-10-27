import { useEffect, useState } from 'react';
import { Button, Modal } from '../../../../components/ui';
import { api } from '../../../../services/api';
import { FlowImageProjectMode } from '../../../../types';
import type { FlowImageProject, FlowImageTaskReference, FlowProjectImage } from '../../../../types';

interface ReferenceBankDialogProps {
  onClose: () => void;
  /** Receives the chosen project images, already copied into the media folder. */
  onApply: (references: FlowImageTaskReference[], errors: string[]) => void;
  applyLabel: string;
}

const projectKey = (project: FlowImageProject) => `${project.accountId}/${project.projectId}`;

/** Picks images already uploaded to a Flow project to use as references. */
export function ReferenceBankDialog({ onClose, onApply, applyLabel }: ReferenceBankDialogProps) {
  const [projects, setProjects] = useState<FlowImageProject[]>([]);
  const [images, setImages] = useState<FlowProjectImage[]>([]);
  const [key, setKey] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [projectsLoading, setProjectsLoading] = useState(true);
  // The project (and refresh) whose images are shown; anything else is loading.
  const [imagesFor, setImagesFor] = useState('');
  const [error, setError] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [applying, setApplying] = useState(false);
  const project = projects.find((entry) => projectKey(entry) === key);
  const imagesKey = `${key}#${refresh}`;
  const imagesLoading = Boolean(project) && imagesFor !== imagesKey;

  useEffect(() => {
    let active = true;
    void api.listFlowImageProjects(FlowImageProjectMode.All).then((response) => {
      if (!active) return;
      setProjectsLoading(false);
      if (!response.success || !response.data) {
        setError(response.error ?? 'Không tải được danh sách project Flow.');
        setProjects([]);
        setKey('');
        return;
      }
      setProjects(response.data);
      setKey(response.data[0] ? projectKey(response.data[0]) : '');
      setError('');
    });
    return () => {
      active = false;
    };
  }, [refresh]);

  useEffect(() => {
    if (!project) return;
    let active = true;
    void api
      .listFlowProjectImages(project.accountId, project.projectId, project.mode)
      .then((response) => {
        if (!active) return;
        setImagesFor(imagesKey);
        if (!response.success || !response.data) {
          setError(response.error ?? 'Không tải được tệp ảnh trong project Flow.');
          setImages([]);
          return;
        }
        setImages(response.data);
      });
    return () => {
      active = false;
    };
  }, [project, imagesKey]);

  const apply = async () => {
    if (!project || selected.length === 0) return;
    setApplying(true);
    const results = await Promise.all(
      selected.map((mediaID) =>
        api.importFlowProjectImageReference(
          project.accountId,
          project.projectId,
          project.mode,
          mediaID,
        ),
      ),
    );
    setApplying(false);
    onApply(
      results.flatMap((res) => (res.success && res.data ? [res.data] : [])),
      results.flatMap((res) => (res.success ? [] : [res.error ?? 'Không tải được ảnh từ Flow.'])),
    );
  };

  const retry = () => {
    setProjectsLoading(true);
    setError('');
    setRefresh((value) => value + 1);
  };

  return (
    <Modal
      title="Kho ảnh"
      subtitle="Chọn project để xem ảnh đã tải lên Flow và áp dụng cho các prompt chưa gửi yêu cầu."
      onClose={onClose}
      footer={
        <>
          <span>{selected.length} ảnh được chọn</span>
          <div>
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              Hủy
            </Button>
            <Button
              type="button"
              variant="primary"
              size="sm"
              disabled={selected.length === 0 || applying}
              onClick={() => void apply()}
            >
              {applying ? 'Đang tải ảnh…' : applyLabel}
            </Button>
          </div>
        </>
      }
    >
      {projectsLoading ? (
        <div className="flow-image-all-reference-bank-empty" role="status">
          <p>Đang tải project Flow…</p>
        </div>
      ) : projects.length === 0 ? (
        <div className="flow-image-all-reference-bank-empty">
          <p>{error || 'Không tìm thấy project Flow nào.'}</p>
          <Button type="button" variant="outline" size="sm" onClick={retry}>
            Tải lại
          </Button>
        </div>
      ) : (
        <div className="flow-image-all-flow-library">
          <label className="flow-image-all-flow-project-select">
            <span>Project Flow</span>
            <select
              value={key}
              onChange={(event) => {
                setKey(event.currentTarget.value);
                setSelected([]);
                setError('');
              }}
            >
              {projects.map((entry) => (
                <option key={projectKey(entry)} value={projectKey(entry)}>
                  {entry.accountEmail} · {entry.title || entry.projectId}
                </option>
              ))}
            </select>
          </label>
          {imagesLoading ? (
            <div className="flow-image-all-reference-bank-empty" role="status">
              <p>Đang tải ảnh của project…</p>
            </div>
          ) : error ? (
            <div className="flow-image-all-reference-bank-empty" role="alert">
              <p>{error}</p>
              <Button type="button" variant="outline" size="sm" onClick={retry}>
                Tải lại
              </Button>
            </div>
          ) : images.length > 0 ? (
            <div className="flow-image-all-reference-bank-grid">
              {images.map((image) => (
                <label className="flow-image-all-reference-bank-item" key={image.id}>
                  <input
                    type="checkbox"
                    checked={selected.includes(image.id)}
                    onChange={(event) => {
                      const checked = event.currentTarget.checked;
                      setSelected((current) =>
                        checked ? [...current, image.id] : current.filter((id) => id !== image.id),
                      );
                    }}
                  />
                  <img src={image.previewUrl} alt={image.name} loading="lazy" />
                  <span title={image.name}>{image.name}</span>
                </label>
              ))}
            </div>
          ) : (
            <div className="flow-image-all-reference-bank-empty">
              <p>Project này chưa có ảnh được Flow trả về.</p>
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}
