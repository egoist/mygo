// A window that takes the size of its page's content (FitContent in Go), as
// browsers size the popups of their extensions. The root element is laid out
// at the width of its content (max-content), within the window's limits, so
// that text wraps at the maximum width, and its height is its content's. A
// ResizeObserver on it tells Go the size, and the room of the scrollbars the
// page shows, whenever it changes; Go clamps it, turns it into DIPs, decides
// whether the scrollbars' room counts, and resizes the window.

/** The limits of the window's size, in the CSS pixels of the page; 0 is
 * none. */
export interface FitLimits {
  minWidth: number;
  minHeight: number;
  maxWidth: number;
  maxHeight: number;
}

/** The rule that lays out the root element at its content's size within the
 * limits, leaving the room of a vertical scrollbar out of the maximum width,
 * so that the root and the scrollbar fit in it. It has no specificity, so that
 * a page's own size for html wins. */
export function fitRule(l: FitLimits, scrollbar: number): string {
  const decls = ["width:max-content"];
  if (l.minWidth > 0) decls.push(`min-width:${Math.max(l.minWidth - scrollbar, 0)}px`);
  if (l.maxWidth > 0) decls.push(`max-width:${Math.max(l.maxWidth - scrollbar, 0)}px`);
  if (l.minHeight > 0) decls.push(`min-height:${l.minHeight}px`);
  return `:where(:root){${decls.join(";")}}`;
}

/** The size of the content in CSS pixels: the root element's, and the room
 * of the vertical scrollbar the page shows (sw) and of a horizontal one (sh),
 * which counts only when the content is wider than the root. */
export interface ContentSize {
  w: number;
  h: number;
  sw: number;
  sh: number;
}

/** Returns a function that tells Go of a size only when it changed. */
export function sizeReporter(post: (size: ContentSize) => void) {
  let last: ContentSize | null = null;
  return (size: ContentSize) => {
    if (last && last.w === size.w && last.h === size.h && last.sw === size.sw && last.sh === size.sh) return;
    last = size;
    post(size);
  };
}

/** Fits the window to the document's content: the rule applies from now on;
 * start, once the DOM is ready, measures the content and watches it.
 * setLimits takes other limits, as the page's zoom changed. */
export function fitContent(
  limits: FitLimits,
  setRule: (css: string) => void,
  post: (size: ContentSize) => void,
) {
  const report = sizeReporter(post);
  let scrollbar = 0; // the vertical scrollbar's width the rule leaves out
  let started = false;
  let pending = false;
  const apply = () => setRule(fitRule(limits, scrollbar));

  // fromObserver: changing the root's size in a ResizeObserver's callback
  // would end its loop with an error event, which the page would hear; the
  // rule changes in the next frame then, and the observer tells the size.
  const measure = (fromObserver: boolean) => {
    const root = document.documentElement;
    if (!root) return;
    const viewport = document.scrollingElement ?? root;
    const sw = Math.max(innerWidth - viewport.clientWidth, 0);
    const sh = Math.max(innerHeight - viewport.clientHeight, 0);
    let r = root.getBoundingClientRect();
    // A vertical scrollbar shows for a moment whenever the content grows
    // before the window does, and an engine may keep it while it lays out
    // (WebKit does): its room is left out of the width only where the
    // content is taller than the window may be, and Go decides whether it
    // counts in the window's.
    const room = limits.maxHeight > 0 && r.height > limits.maxHeight ? sw : 0;
    if (room !== scrollbar) {
      scrollbar = room;
      if (!fromObserver) {
        apply();
        r = root.getBoundingClientRect();
      } else if (!pending) {
        pending = true;
        requestAnimationFrame(() => {
          pending = false;
          apply();
        });
      }
    }
    // The content overflows the root only when the root is at its maximum
    // width.
    const body = document.body;
    const wider = !!body && body.scrollWidth > body.clientWidth;
    report({ w: r.width, h: r.height, sw, sh: wider ? sh : 0 });
  };

  apply();
  return {
    start() {
      if (started) return;
      started = true;
      measure(false);
      if (typeof ResizeObserver !== "undefined" && document.documentElement) {
        new ResizeObserver(() => measure(true)).observe(document.documentElement);
      }
      // The window's own size changed what scrollbars show.
      addEventListener("resize", () => measure(false));
    },
    setLimits(l: FitLimits) {
      limits = l;
      apply();
      if (started) measure(false);
    },
  };
}

// A web view in a window of native UI whose element is as tall as its page's
// content (ui's FitContent, WebView.OnContentSizeChanged in Go) measures the
// page at the web view's width, which the native UI sets, without the rule
// above: fit-world.ts runs it in an isolated world of the page, which may be
// untrusted and have no bridge.

/** What a web view's page measures of its document: its viewport, its root
 * element's border box, and its scrolling element's client and scroll sizes,
 * in CSS pixels. */
export interface ViewMetrics {
  innerWidth: number;
  innerHeight: number;
  rootWidth: number;
  rootHeight: number;
  clientWidth: number;
  clientHeight: number;
  scrollWidth: number;
  scrollHeight: number;
}

/** The size of a web view's page's content, in CSS pixels: its width and
 * height, and the room of a horizontal scrollbar (sh), which counts only
 * while the content is wider than the viewport; vw and vh are the viewport
 * it was measured in. */
export interface ViewSize {
  w: number;
  h: number;
  sh: number;
  vw: number;
  vh: number;
}

/** Returns the size of the content of a page laid out in its viewport, or
 * null while it has no width to lay out at (a web view not shown yet).
 *
 * Its height does not follow the viewport's: content taller than the
 * viewport overflows it, and is as tall as it scrolls; content that fits is
 * as tall as its root element, which is as tall as its content (height:
 * auto), so that a web view as tall as it measures the same again, and one
 * whose content shrank measures less. A root as tall as the viewport, as with
 * html { height: 100% }, measures as tall as the viewport: the web view takes
 * that height again, and keeps it. */
export function viewSize(m: ViewMetrics): ViewSize | null {
  if (!(m.innerWidth > 0)) return null;
  const overflowsY = m.scrollHeight > m.clientHeight;
  const overflowsX = m.scrollWidth > m.clientWidth;
  return {
    w: overflowsX ? Math.max(m.rootWidth, m.scrollWidth) : m.rootWidth,
    h: overflowsY ? Math.max(m.rootHeight, m.scrollHeight) : m.rootHeight,
    sh: overflowsX ? Math.max(m.innerHeight - m.clientHeight, 0) : 0,
    vw: m.innerWidth,
    vh: m.innerHeight,
  };
}

/** Returns the size of the content of a page at its viewport's whole width,
 * from its metrics as it shows and, measured with the root's scrollbars
 * hidden, as it lays out without them (null where they could not be hidden).
 *
 * Where scrollbars take room (classic scrollbars: Windows, macOS set to
 * always show them), content laid out while it overflowed is narrower, and
 * taller for it; a web view grown to that height drops the scrollbar, and
 * the engines keep the narrower layout rather than lay out again and bring
 * it back: the page stays narrower than its web view, with an empty band
 * where the scrollbar was. Measured at the whole width, the content is as
 * tall as it is without the scrollbar, which a web view of that height
 * does not show. The room of a horizontal scrollbar is the one the page
 * shows, since the hidden one takes none. Where hiding them didn't give
 * the root the whole width, the content is measured as it shows. */
export function wholeViewSize(shown: ViewMetrics, whole: ViewMetrics | null): ViewSize | null {
  if (!whole || whole.clientWidth < whole.innerWidth || whole.innerWidth !== shown.innerWidth) return viewSize(shown);
  const size = viewSize(whole);
  if (!size) return null;
  const sh = whole.scrollWidth > whole.clientWidth ? Math.max(shown.innerHeight - shown.clientHeight, 0) : 0;
  return { ...size, sh };
}

/** The style sheet that hides the root's scrollbars while the page measures
 * its content at its viewport's whole width (wholeViewSize). */
export const hideRootScrollbars = ":root{scrollbar-width:none!important}:root::-webkit-scrollbar{display:none!important}";

/** What a web view's page posts to Go: its content's size, and r, 1 when it
 * was measured right after the viewport's height alone changed, which Go
 * resized it to (it holds a size that would go back and forth). */
export type ViewReport = ViewSize & { r: 0 | 1 };

/** Returns a function that takes measures of a page as they come and posts
 * the content's size when it changed. */
export function viewReporter(post: (r: ViewReport) => void) {
  let last: ViewSize | null = null;
  let vw = -1;
  let vh = -1;
  return (size: ViewSize | null) => {
    if (!size) return;
    const resized = vh >= 0 && size.vh !== vh && size.vw === vw;
    vw = size.vw;
    vh = size.vh;
    if (last && last.w === size.w && last.h === size.h && last.sh === size.sh) return;
    last = size;
    post({ ...size, r: resized ? 1 : 0 });
  };
}
