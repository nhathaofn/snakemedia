import { useCallback, useMemo } from 'react';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseJobRequest } from '../../../types';
import type { MuseJob, MuseJobKind, MuseJobMode } from '../../../types';
import { mayBePaid } from './format';
import type { MuseDraft } from './museDraft';
import { useMuseStore } from './museStore';

const RETRY_PAID_CONFIRM = 'Muse có thể đã nhận tác vụ này. Chạy lại sẽ tốn thêm 1 lượt?';

/** What a job tile or row can do; one stable object shared by every item. */
export interface JobActions {
  cancel: (id: string) => void;
  retry: (job: MuseJob) => void;
  run: (id: string) => void;
  editPrompt: (id: string, prompt: string) => void;
  remove: (id: string) => void;
  save: (url: string) => void;
  reveal: (url: string) => void;
}

/**
 * Runs backend calls with errors shown as toasts, plus the per-job actions.
 * Job changes come back through MuseJobUpdated; the calls also apply the job
 * they return so the screen does not wait for the event.
 */
export function useMuseActions() {
  const setJob = useMuseStore((s) => s.setJob);
  const dropJob = useMuseStore((s) => s.dropJob);
  const addToast = useAppStore((s) => s.addToast);

  const report = useCallback(
    (cause: unknown) =>
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) }),
    [addToast],
  );

  const act = useCallback(
    async (action: () => Promise<void>) => {
      try {
        await action();
      } catch (cause) {
        report(cause);
      }
    },
    [report],
  );

  const jobActions = useMemo<JobActions>(
    () => ({
      cancel: (id) => void act(async () => setJob(unwrap(await api.cancelMuseJob(id)))),
      retry: (job) => {
        if (mayBePaid(job) && !window.confirm(RETRY_PAID_CONFIRM)) return;
        void act(async () => setJob(unwrap(await api.retryMuseJob(job.id))));
      },
      // The backend reports the started job through MuseJobUpdated.
      run: (id) => void act(async () => void unwrap(await api.runMuseJobs([id]))),
      editPrompt: (id, prompt) =>
        void act(async () => setJob(unwrap(await api.editAndRetryMuseJob(id, prompt)))),
      remove: (id) =>
        void act(async () => {
          unwrap(await api.removeMuseJob(id));
          dropJob(id);
        }),
      save: (url) =>
        void act(async () => {
          const saved = unwrap(await api.saveMedia(url));
          if (!saved.canceled) addToast({ type: 'success', message: `Đã lưu ${saved.path}` });
        }),
      reveal: (url) => void act(async () => void unwrap(await api.revealFile(url))),
    }),
    [act, addToast, dropJob, setJob],
  );

  return { act, report, jobActions };
}

/**
 * Queues prompts of one kind and mode with the draft's references; a batch
 * also names the folder its files are saved into. Resolves true when they
 * were added.
 */
export function useMuseSubmit(kind: MuseJobKind, mode: MuseJobMode) {
  const setJob = useMuseStore((s) => s.setJob);
  const addToast = useAppStore((s) => s.addToast);
  return useCallback(
    async (
      prompts: string[],
      repeat: number,
      held: boolean,
      draft: Pick<MuseDraft, 'references' | 'ratio' | 'duration'>,
      outputDir = '',
    ) => {
      try {
        const created = unwrap(
          await api.submitMuseJobs(
            MuseJobRequest.createFrom({
              kind,
              mode,
              prompts,
              repeat,
              references: draft.references.map((r) => r.localUrl),
              label: '',
              held,
              outputDir,
              ratio: draft.ratio,
              duration: draft.duration,
            }),
          ),
        );
        created.forEach(setJob);
        return created.length > 0;
      } catch (cause) {
        addToast({
          type: 'error',
          message: cause instanceof Error ? cause.message : String(cause),
        });
        return false;
      }
    },
    [addToast, kind, mode, setJob],
  );
}
