// A star is drawn as a point, but what an operator reads is the two lines of
// text beside it. The layered layout places a node by its box, so the box has
// to be as wide as the text, not as wide as the point: laying stars out on a
// fixed 120px box is what let analytics.example.com print straight over
// telemetry.example.io.

/** The room a star takes before its label starts. */
export const STAR_CELL = 56;

/** The breathing space between the star and its first character. */
export const LABEL_GAP = 12;

/** The widest a label may grow before it is cut. */
export const LABEL_MAX = 260;

/** The height of one cell, and so of one layer's row. */
export const ROW_HEIGHT = 52;

export type Measure = (text: string) => number;

export function cellWidth(label: number, detail: number): number {
  return STAR_CELL + LABEL_GAP + Math.max(1, Math.min(LABEL_MAX, Math.max(label, detail)));
}

// fitLabel cuts a string to fit, counting the ellipsis it adds. One character
// survives even at zero width, so a cut label is still visibly a label.
export function fitLabel(text: string, measure: Measure, maxWidth: number): string {
  if (measure(text) <= maxWidth) {
    return text;
  }
  let cut = text;
  while (cut.length > 1 && measure(`${cut}…`) > maxWidth) {
    cut = cut.slice(0, -1);
  }
  return `${cut}…`;
}
