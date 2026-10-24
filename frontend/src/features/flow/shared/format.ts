/** '16:9' → '16 / 9' for the CSS `aspect-ratio` property. */
export const cssAspectRatio = (ratio: string) => ratio.replace(':', ' / ');

/** '16:9' → '16:9 · Ngang', derived from the ratio itself. */
export function ratioLabel(ratio: string): string {
  const [w, h] = ratio.split(':').map(Number);
  if (!w || !h) return ratio;
  const orientation = w > h ? 'Ngang' : w < h ? 'Dọc' : 'Vuông';
  return `${ratio} · ${orientation}`;
}

export const durationLabel = (seconds: number) => `${seconds} giây`;

/** 8.5 → '0:08.5': a position in a video, to a tenth of a second. */
export function clockLabel(seconds: number): string {
  const tenths = Math.round(Math.max(0, seconds) * 10);
  const minutes = Math.floor(tenths / 600);
  const rest = ((tenths % 600) / 10).toFixed(1).padStart(4, '0');
  return `${minutes}:${rest}`;
}
