// Entry point of the script that measures the page of a web view in a window
// of native UI whose element is as tall as the page's content (ui's
// FitContent, WebView.OnContentSizeChanged in Go). MyGo runs it in an
// isolated world of the page's top frame, in its current document and in
// those it loads: a WKContentWorld on macOS, a script world on Linux, a world
// of Page.addScriptToEvaluateOnNewDocument on Windows. It sees the page's DOM
// and layout, but none of its scripts' objects, and its own prototypes are
// its own, so the page can neither reach it nor change what it measures. It
// tells Go the content's size through a script message handler (WebKit) or a
// binding (WebView2) that only that world has: the page's scripts can't tell
// a size, with the bridge or without it (untrusted pages).
import { type ViewMetrics, viewReporter, viewSize } from "./fit-content";

// The handler's or the binding's name, as the backends name it
// (platform.FitHandler).
const HANDLER = "mygoContentSize";

(() => {
  const w = window as any;
  if (w.__mygoFit) return;
  w.__mygoFit = true;
  // Frames of the page measure nothing (WebView2 runs the script in each).
  if (window.top !== window) return;

  const post = viewReporter((r) => {
    const json = JSON.stringify(r);
    try {
      const h = w.webkit?.messageHandlers?.[HANDLER];
      if (h) h.postMessage(json);
      else w[HANDLER]?.(json);
    } catch {
      // The document is going.
    }
  });

  const metrics = (): ViewMetrics | null => {
    const root = document.documentElement;
    if (!root) return null;
    const viewport = document.scrollingElement ?? root;
    const r = root.getBoundingClientRect();
    return {
      innerWidth,
      innerHeight,
      rootWidth: r.width,
      rootHeight: r.height,
      clientWidth: viewport.clientWidth,
      clientHeight: viewport.clientHeight,
      scrollWidth: viewport.scrollWidth,
      scrollHeight: viewport.scrollHeight,
    };
  };
  const measure = () => {
    const m = metrics();
    if (m) post(viewSize(m));
  };

  // Changes that may change the content's size without resizing the root
  // or the body, as content overflowing a body of height: 100%, measure in
  // the next frame, once each.
  let pending = false;
  const soon = () => {
    if (pending) return;
    pending = true;
    requestAnimationFrame(() => {
      pending = false;
      measure();
    });
  };

  const resized = new ResizeObserver(measure);
  let body: Element | null = null;
  const watch = () => {
    const root = document.documentElement;
    if (!root) return;
    resized.observe(root);
    if (document.body && document.body !== body) {
      if (body) resized.unobserve(body);
      body = document.body;
      resized.observe(body);
    }
  };
  watch();
  new MutationObserver(() => {
    watch();
    soon();
  }).observe(document, { childList: true, subtree: true, attributes: true, characterData: true });
  // Images, fonts and the page's animations change the layout as they come.
  // The loads of elements reach the document, not the window.
  for (const type of ["load", "transitionend", "animationend"]) document.addEventListener(type, soon, true);
  document.fonts?.addEventListener?.("loadingdone", soon);
  addEventListener("resize", measure);
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", measure);
  measure();
})();
