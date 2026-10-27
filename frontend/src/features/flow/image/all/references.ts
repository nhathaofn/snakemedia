import { api } from '../../../../services/api';
import { FlowImageTaskStatus } from '../../../../types';
import type { FlowImageTask, FlowImageTaskReference, FlowModel } from '../../../../types';

/** Tasks that can still take references: no request has left for them yet. */
export function referenceTargets(tasks: readonly FlowImageTask[]): FlowImageTask[] {
  return tasks.filter(
    (task) =>
      !task.requestId &&
      (task.status === FlowImageTaskStatus.Queued ||
        task.status === FlowImageTaskStatus.Failed ||
        task.status === FlowImageTaskStatus.Cancelled),
  );
}

/**
 * The first target task whose model cannot take the references added to it,
 * or null when all can.
 */
export function overCapacity(
  targets: readonly FlowImageTask[],
  added: readonly FlowImageTaskReference[],
  models: readonly FlowModel[],
): FlowImageTask | null {
  for (const task of targets) {
    const model = models.find((entry) => entry.id === task.modelId);
    const merged = new Set([...task.references, ...added].map((ref) => ref.localUrl));
    if (!model || merged.size > model.maxReferenceImages) return task;
  }
  return null;
}

/** Adds references to every task that can still take them; returns the error, if any. */
export async function applyToTasks(
  targets: readonly FlowImageTask[],
  refs: FlowImageTaskReference[],
): Promise<string | null> {
  if (targets.length === 0 || refs.length === 0) return null;
  const response = await api.addFlowImageTaskReferences(
    targets.map((task) => task.id),
    refs,
  );
  return response.success ? null : (response.error ?? 'Không rõ lỗi.');
}
