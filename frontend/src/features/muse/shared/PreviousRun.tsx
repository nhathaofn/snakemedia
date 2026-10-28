import { useEffect, useMemo, useRef } from 'react';
import { PreviousRunToggle } from '../../../components/feedback';
import { RUN_START, usePreviousRunStore } from '../../../stores';
import { MuseJobKind } from '../../../types';
import { fromPreviousRun, screenKind } from './format';
import { useMuseStore } from './museStore';

/**
 * How many jobs, images and videos earlier runs of the app left in the Muse
 * queue. A storyboard's frames count with the video they open, as on screen.
 */
function useMusePreviousRun() {
  const jobs = useMuseStore((s) => s.jobs);
  return useMemo(() => {
    const previous = jobs.filter((job) => fromPreviousRun(job, RUN_START));
    return {
      total: previous.length,
      images: previous.filter((j) => screenKind(j) === MuseJobKind.Image).length,
      videos: previous.filter((j) => screenKind(j) === MuseJobKind.Video).length,
    };
  }, [jobs]);
}

/**
 * Loads the Muse queue on start and tells the start question what the earlier
 * run left, once: jobs ending while the question is up do not change its text.
 */
export function MusePreviousRunReport() {
  const load = useMuseStore((s) => s.load);
  const loaded = useMuseStore((s) => s.loaded);
  const report = usePreviousRunStore((s) => s.report);
  const muse = useMusePreviousRun();
  const reported = useRef(false);
  useEffect(() => {
    if (!loaded) {
      report('muse', null);
      void load();
      return;
    }
    if (reported.current) return;
    reported.current = true;
    report(
      'muse',
      [
        muse.images > 0 && `${muse.images} ảnh Muse`,
        muse.videos > 0 && `${muse.videos} video Muse`,
      ].filter((p): p is string => !!p),
    );
  }, [loaded, load, report, muse.images, muse.videos]);
  return null;
}

/** The Muse screens' "Phiên cũ" button. */
export function MusePreviousRunToggle() {
  return <PreviousRunToggle count={useMusePreviousRun().total} />;
}
