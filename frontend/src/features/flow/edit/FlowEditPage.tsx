import React from 'react';
import '../shared/FlowStudio.css';
import './FlowEditPage.css';
import { useAppStore } from '../../../stores';
import { api } from '../../../services/api';
import { EditVideoRequest } from '../../../types';
import {
  FlowModePanel,
  ConversationGallery,
  PromptBar,
  SelectField,
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
    <aside className="flow-studio-sidebar" aria-label="Edit video tools">
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
  const loadModels = React.useCallback(async () => {
    const result = await api.getEditModels();
    const latest = useFlowEditStore.getState();
    return latest.uploadedSourceID ? { ...result, success: true, data: latest.models } : result;
  }, []);
  const applyCatalog = React.useCallback(
    (models: Parameters<typeof setModels>[0]) => {
      if (!useFlowEditStore.getState().uploadedSourceID) setModels(models);
    },
    [setModels],
  );
  const { loading, error } = useModelCatalog(loadModels, applyCatalog);

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
          Tạo 1 video từ đoạn đã chọn, tỉ lệ và chất lượng theo nguồn.
        </p>
      )}
    </FlowModePanel>
  );
}

function EditWorkspace() {
  const store = useFlowEditStore();
  const model = useFlowEditStore(selectedModelOf);
  const generate = useGeneration(
    {
      getState: () => {
        const current = useFlowEditStore.getState();
        return { ...current, addItems: current.upsertItems };
      },
    },
    'video',
  );
  const notify = useAppStore((s) => s.addToast);
  const resume = async (id: string) => {
    const current = useFlowEditStore.getState();
    if (current.isGenerating) return;
    current.beginGeneration();
    try {
      const result = await api.resumeVideoEdit(id);
      if (result.success && result.data) current.upsertItems(result.data);
      else
        notify({
          type: 'error',
          title: 'Chưa kiểm tra được video',
          message: result.error ?? 'Không rõ lỗi.',
        });
    } finally {
      useFlowEditStore.getState().finishGeneration();
    }
  };
  const saveMedia = useSaveMedia();
  const {
    prompt,
    items,
    turns,
    source,
    sourceDuration,
    uploadedSourceID,
    isSourceLoading,
    sourceError,
  } = store;

  const blockedReason = !model
    ? 'Chưa có model chỉnh sửa video: kiểm tra tài khoản ở mục Account.'
    : !source
      ? 'Chọn video gốc ở cột bên trái.'
      : sourceError
        ? sourceError
        : isSourceLoading || !uploadedSourceID || sourceDuration <= 0
          ? 'Đang tải video lên Google Flow và xử lý thông số, vui lòng đợi trong giây lát...'
          : undefined;

  const onGenerate = () => {
    const current = useFlowEditStore.getState();
    const turn = {
      id: crypto.randomUUID(),
      prompt: current.prompt.trim(),
      attachments: current.source
        ? [
            {
              id: current.source.localUrl,
              kind: 'video' as const,
              name: current.source.fileName,
              url: current.source.localUrl,
            },
          ]
        : [],
      pendingCount: 1,
      pendingRatio:
        current.sourceWidth > 0 && current.sourceHeight > 0
          ? `${current.sourceWidth}:${current.sourceHeight}`
          : '',
      pendingLabel: 'Đang chỉnh sửa video',
    };
    const request = EditVideoRequest.createFrom({
      prompt: current.prompt.trim(),
      modelId: current.selectedModel,
      sourceUrl: current.source?.localUrl ?? '',
      sourceId: current.uploadedSourceID ?? '',
      ...trimRequest(current),
    });
    void generate(
      async () => {
        request.requestId = current.requestIDFor(request);
        const result = await api.editVideos(request);
        if (result.success) current.acceptAttempt(request.requestId);
        return result;
      },
      {
        onStart: () => useFlowEditStore.getState().beginTurn(turn),
        onSuccess: (results) => useFlowEditStore.getState().completeTurn(turn.id, results),
        onFailure: (message, promptRestored) => {
          const current = useFlowEditStore.getState();
          if (promptRestored) current.removeTurn(turn.id);
          else current.failTurn(turn.id, message);
        },
      },
    );
  };

  return (
    <div className="flow-workspace-container">
      <ConversationGallery
        turns={turns}
        items={items}
        emptyTitle="Chỉnh sửa video với Google Flow"
        emptyText="Chọn video gốc, rồi mô tả thay đổi bạn muốn (góc quay, ánh sáng, trang phục…) và nhấn Enter."
        renderItem={(video) => (
          <VideoCard
            video={video}
            showPrompt={false}
            onSave={(url) => void saveMedia(url)}
            onDismiss={() => useFlowEditStore.getState().dismissItem(video.jobId || video.id)}
            onResume={video.jobId ? () => void resume(video.jobId!) : undefined}
            busy={store.isGenerating}
          />
        )}
      />

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
