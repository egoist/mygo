import { describe, expect, test } from "bun:test";
import { type ContentSize, fitRule, sizeReporter, type ViewMetrics, type ViewReport, viewReporter, viewSize } from "./fit-content";

const none = { minWidth: 0, minHeight: 0, maxWidth: 0, maxHeight: 0 };

describe("fitRule", () => {
  test("lays out the root at its content's width, without specificity", () => {
    expect(fitRule(none, 0)).toBe(":where(:root){width:max-content}");
  });

  test("bounds the width and the minimum height, not the maximum height", () => {
    expect(fitRule({ minWidth: 100, minHeight: 40, maxWidth: 300, maxHeight: 200 }, 0)).toBe(
      ":where(:root){width:max-content;min-width:100px;max-width:300px;min-height:40px}",
    );
  });

  test("leaves a vertical scrollbar's room out of the width", () => {
    expect(fitRule({ ...none, minWidth: 100, maxWidth: 300 }, 15)).toBe(
      ":where(:root){width:max-content;min-width:85px;max-width:285px}",
    );
    expect(fitRule({ ...none, maxWidth: 10 }, 15)).toBe(":where(:root){width:max-content;max-width:0px}");
  });

  test("takes the fractional CSS pixels of a zoomed page", () => {
    expect(fitRule({ ...none, maxWidth: 133.5 }, 0)).toBe(":where(:root){width:max-content;max-width:133.5px}");
  });
});

describe("sizeReporter", () => {
  test("tells only of changes", () => {
    const told: ContentSize[] = [];
    const report = sizeReporter((s) => told.push(s));
    const size = (w: number, h: number, sw = 0, sh = 0) => ({ w, h, sw, sh });
    report(size(120, 80));
    report(size(120, 80));
    report(size(120.5, 80));
    report(size(120.5, 90));
    report(size(120.5, 90));
    report(size(120.5, 90, 15));
    report(size(120.5, 90, 15, 15));
    report(size(120.5, 90, 15, 15));
    report(size(0, 0));
    expect(told).toEqual([
      size(120, 80),
      size(120.5, 80),
      size(120.5, 90),
      size(120.5, 90, 15),
      size(120.5, 90, 15, 15),
      size(0, 0),
    ]);
  });
});

// A page 300 CSS pixels wide in a viewport of height vh, its root's height
// root, scrolling to scroll, with no scrollbars that take room.
function metrics(vh: number, root: number, scroll: number, more: Partial<ViewMetrics> = {}): ViewMetrics {
  return {
    innerWidth: 300,
    innerHeight: vh,
    rootWidth: 300,
    rootHeight: root,
    clientWidth: 300,
    clientHeight: vh,
    scrollWidth: 300,
    scrollHeight: Math.max(vh, scroll),
    ...more,
  };
}

describe("viewSize", () => {
  test("measures nothing without a width", () => {
    expect(viewSize({ ...metrics(0, 0, 0), innerWidth: 0 })).toBeNull();
  });

  test("is the root's height when the content fits, whatever the viewport", () => {
    expect(viewSize(metrics(1, 1, 1))?.h).toBe(1);
    // As tall as the content, as Go makes it: the same again.
    expect(viewSize(metrics(480.5, 480.5, 480))?.h).toBe(480.5);
    // Shorter than the viewport: the web view shrinks.
    expect(viewSize(metrics(480, 120, 120))?.h).toBe(120);
  });

  test("is as tall as the content scrolls when it overflows", () => {
    // A web view not as tall yet.
    expect(viewSize(metrics(1, 800, 800))).toEqual({ w: 300, h: 800, sh: 0, vw: 300, vh: 1 });
    // A root as tall as the viewport (html, body { height: 100% }), the
    // content overflowing it.
    expect(viewSize(metrics(150, 150, 900))?.h).toBe(900);
    // Then at that height, it keeps it.
    expect(viewSize(metrics(900, 900, 900))?.h).toBe(900);
  });

  test("counts a horizontal scrollbar's room only while the content is wider", () => {
    const wide = { scrollWidth: 500, clientHeight: 185 };
    expect(viewSize(metrics(200, 200, 200, wide))).toEqual({ w: 500, h: 200, sh: 15, vw: 300, vh: 200 });
    expect(viewSize(metrics(200, 200, 200, { clientHeight: 185 }))?.sh).toBe(0);
  });
});

describe("viewReporter", () => {
  test("tells changes of the content's size, and which came with a new height", () => {
    const told: ViewReport[] = [];
    const report = viewReporter((r) => told.push(r));
    report(null);
    report(viewSize(metrics(1, 800, 800))); // first measure
    report(viewSize(metrics(800, 800, 800))); // resized by Go: the same size
    report(viewSize(metrics(800, 600, 600))); // an image went
    report(viewSize(metrics(600, 650, 650))); // resized by Go, and taller
    expect(told.map((r) => [r.h, r.vh, r.r])).toEqual([
      [800, 1, 0],
      [600, 800, 0],
      [650, 600, 1],
    ]);
  });

  test("a new width is not Go's height", () => {
    const told: ViewReport[] = [];
    const report = viewReporter((r) => told.push(r));
    report(viewSize(metrics(100, 100, 100)));
    report(viewSize({ ...metrics(200, 150, 150), innerWidth: 250, rootWidth: 250, clientWidth: 250, scrollWidth: 250 }));
    expect(told.map((r) => r.r)).toEqual([0, 0]);
  });
});
