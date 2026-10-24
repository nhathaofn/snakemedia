import React from 'react';
import '../shared/FlowStudio.css';
import './FlowEditPage.css';
import { api } from '../../../services/api';
import { EditVideoRequest } from '../../../types';
import {
  FlowModePanel,
  MediaGallery,
  PromptBar,
  SelectField,
  VIDEO_GRID_CLASS,
  VideoCard,
  selectedModelOf,
  useGeneration,
  useModelCatalog,
  useSaveMedia,
} from '../shared';
import { SourceVideoPanel } from './SourceVideoPanel';
import { trimRequest, useFlowEditStore } from './store/flowEditStore';

/**
 * FlowEditPage - Edit video: the source video and model on the left, the edited
 * results and the prompt describing the change on the right. Like Flow's own
 * editor it makes one video whose ratio and quality follow the source.
 */
export const FlowEditPage: React.FC = () => (
  <div className="flow-studio-page">
    <aside className="flow-studio-sidebar flow-edit-panel" aria-label="Edit video tools">
      <EditSettings />
    </aside>
    <section className="flow-studio-content" aria-label="Edit video workspace">
      <EditWorkspace />
    </section>
  </div>
);

function EditSettings() {
  const setModels = useFlowEditStore((s) => s.setModels);
  const model = useFlowEditStore(selectedModelOf);
  const store = useFlowEditStore();
  const { loading, error } = useModelCatalog(api.getEditModels, setModels);

  return (
    <FlowModePanel>
      <SourceVideoPanel />

      <SelectField
        id="flow-edit-model"
        label="Model"
        value={store.selectedModel}
        options={store.models.map((m) => ({ value: m.id, label: m.name }))}
        onChange={store.setSelectedModel}
        loading={loading}
        error={error ? { title: 'Không tải được danh sách model', message: error } : null}
      />

      {model && (
        <p className="flow-edit-note">
          Tạo 1 video dài {model.duration} giây, tỉ lệ và chất lượng theo video gốc.
        </p>
      )}
    </FlowModePanel>
  );
}

function EditWorkspace() {
  const store = useFlowEditStore();
  const model = useFlowEditStore(selectedModelOf);
  const generate = useGeneration(useFlowEditStore, 'video');
  const saveMedia = useSaveMedia();
  const {
    prompt,
    items,
    source,
    sourceWidth,
    sourceHeight,
    sourceDuration,
    uploadedMediaId,
    isSourceLoading,
    sourceError,
  } = store;

  const blockedReason = !model
    ? 'Chưa có model chỉnh sửa video: kiểm tra tài khoản ở mục Account.'
    : !source
      ? 'Chọn video gốc ở cột bên trái.'
      : isSourceLoading || !uploadedMediaId || sourceDuration <= 0
        ? 'Đang tải video lên Google Flow và xử lý thông số, vui lòng đợi trong giây lát...'
        : sourceError
          ? sourceError
          : undefined;

  const onGenerate = () =>
    void generate(() =>
      api.editVideos(
        EditVideoRequest.createFrom({
          prompt: prompt.trim(),
          modelId: store.selectedModel,
          sourceUrl: source?.localUrl ?? '',
          mediaId: store.uploadedMediaId ?? '',
          width: store.sourceWidth,
          height: store.sourceHeight,
          ...trimRequest(store),
        }),
      ),
    );

  return (
    <div className="flow-workspace-container">
      <MediaGallery
        isEmpty={items.length === 0}
        emptyTitle="Chỉnh sửa video với Google Flow"
        emptyText="Chọn video gốc, rồi mô tả thay đổi bạn muốn (góc quay, ánh sáng, trang phục…) và nhấn Enter."
        pending={store.isGenerating ? 1 : 0}
        pendingRatio={sourceWidth > 0 && sourceHeight > 0 ? `${sourceWidth}:${sourceHeight}` : ''}
        pendingLabel="Đang chỉnh sửa video"
        gridClassName={VIDEO_GRID_CLASS}
      >
        {items.map((video) => (
          <VideoCard key={video.id} video={video} onSave={(url) => void saveMedia(url)} />
        ))}
      </MediaGallery>

      <PromptBar
        prompt={prompt}
        onPromptChange={store.setPrompt}
        onGenerate={onGenerate}
        placeholder="Mô tả thay đổi cho video gốc"
        generating={store.isGenerating}
        blockedReason={blockedReason}
      />
    </div>
  );
}
