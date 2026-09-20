import { describe, expect, it } from "vitest";
import { cellWidth, fitLabel, LABEL_GAP, LABEL_MAX, STAR_CELL } from "./label";

// A stand-in for canvas text measurement: ten units per character. The real
// ruler is a canvas context, which no unit test has.
const tens = (text: string) => text.length * 10;

describe("fitLabel", () => {
  it("leaves text that already fits alone", () => {
    expect(fitLabel("ads.example.com", tens, 150)).toBe("ads.example.com");
    expect(fitLabel("ads.example.com", tens, 150)).toBe("ads.example.com");
  });

  it("cuts to the last character that fits with the ellipsis counted", () => {
    expect(fitLabel("analytics.example.com", tens, 100)).toBe("analytics…");
    expect(fitLabel("analytics.example.com", tens, 101)).toBe("analytics…");
    expect(fitLabel("analytics.example.com", tens, 110)).toBe("analytics.…");
  });

  it("keeps one character rather than cutting to nothing", () => {
    expect(fitLabel("ads.example.com", tens, 0)).toBe("a…");
    expect(fitLabel("", tens, 0)).toBe("");
  });
});

describe("cellWidth", () => {
  it("holds the star, the gap, and the wider of the two lines", () => {
    expect(cellWidth(120, 80)).toBe(STAR_CELL + LABEL_GAP + 120);
    expect(cellWidth(80, 120)).toBe(STAR_CELL + LABEL_GAP + 120);
  });

  it("never lets a cell collapse, and never grows past the cap", () => {
    expect(cellWidth(0, 0)).toBe(STAR_CELL + LABEL_GAP + 1);
    expect(cellWidth(9000, 9000)).toBe(STAR_CELL + LABEL_GAP + LABEL_MAX);
  });
});
