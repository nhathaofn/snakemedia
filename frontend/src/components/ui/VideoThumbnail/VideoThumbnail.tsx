import { useEffect, useRef, useState } from 'react';

/** Loads video metadata when the thumbnail approaches the visible area. */
export function VideoThumbnail({ src, className }: { src: string; className?: string }) {
  const ref = useRef<HTMLVideoElement>(null);
  const [nearViewport, setNearViewport] = useState(
    () => typeof IntersectionObserver === 'undefined',
  );

  useEffect(() => {
    if (nearViewport || !ref.current) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setNearViewport(true);
          observer.disconnect();
        }
      },
      { rootMargin: '240px' },
    );
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [nearViewport]);

  return (
    <video
      ref={ref}
      src={nearViewport ? src : undefined}
      className={className}
      preload="metadata"
      muted
    />
  );
}
