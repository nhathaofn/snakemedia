import { StudioLayout } from '../../components/studio';
import { MuseJobKind, MuseJobMode } from '../../types';
import { MuseBatchPanel, MuseQueueWorkspace } from './batch/MuseBatch';
import './shared/Muse.css';
import { useMuseLive } from './shared/useMuseLive';
import { MuseSinglePanel, MuseSingleWorkspace } from './single/MuseSingle';

const labels: Record<MuseJobKind, string> = {
  [MuseJobKind.Image]: 'Muse image',
  [MuseJobKind.Video]: 'Muse video',
};

/**
 * One Muse kind's studio: Đơn (one prompt at a time, results as a gallery) and
 * Hàng loạt (a batch form and the queue). Both feed the same backend queue;
 * a job's mode decides which screen lists it.
 */
export function MuseStudio({ kind }: { kind: MuseJobKind }) {
  useMuseLive();
  return (
    <StudioLayout
      label={labels[kind]}
      modes={[
        { value: MuseJobMode.Single, label: 'Đơn', content: <MuseSinglePanel kind={kind} /> },
        { value: MuseJobMode.Batch, label: 'Hàng loạt', content: <MuseBatchPanel kind={kind} /> },
      ]}
      renderWorkspace={(mode) =>
        mode === MuseJobMode.Single ? (
          <MuseSingleWorkspace kind={kind} />
        ) : (
          <MuseQueueWorkspace kind={kind} />
        )
      }
    />
  );
}
