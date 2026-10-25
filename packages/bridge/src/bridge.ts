// Entry point of the script MyGo injects at document start into every page.
// The Go side wraps the bundle in a function that defines __MYGO_CONFIG__.
import { find, stopFind } from "./find";
import { createRuntime } from "./runtime";
import { isTrusted } from "./trust";
import type { BridgeConfig, ThemePreferences } from "./types";

declare const __MYGO_CONFIG__: BridgeConfig;

// Elements that keep receiving clicks inside a drag region.
const INTERACTIVE =
  'input,textarea,select,button,a[href],summary,label,[contenteditable]:not([contenteditable="false"]),video[controls],audio[controls]';

// What the __mygo of a page that is not trusted does with what Go delivers.
const ignore = () => {};

(() => {
  const w = window as any;
  if (w.__mygo) return;

  // The native channel is taken, and hidden from the page, wherever the page
  // is from; only the pages Go trusts get the bridge. An untrusted one finds
  // none of it: neither window.mygo, nor the channel to talk to Go with.
  const raw = transport(w);
  if (!raw || !isTrusted(location, __MYGO_CONFIG__.trust)) {
    // Go's scripts reach every page through window.__mygo: replies and
    // events (deliver), the find bar (find). Pinned here, before a script of
    // the page runs, it is not the page's to define, so what Go says for
    // another page is lost in this one, and the page gets no say in what
    // FindInPage reports. (Go does not send a page that is not trusted
    // anything; this is for the document it could not tell about yet.)
    Object.defineProperty(w, "__mygo", { value: Object.freeze({ receive: ignore, deliver: ignore, find, stopFind }) });
    return;
  }
  // Only this closure knows the secret and the channel, so scripts of the
  // page cannot post messages of their own.
  const post = (m: string) => raw.post(__MYGO_CONFIG__.secret + m);

  const { runtime, internal, token } = createRuntime(__MYGO_CONFIG__, post);
  Object.defineProperty(w, "mygo", { value: runtime, enumerable: true });
  Object.defineProperty(w, "__mygo", { value: Object.freeze({ ...internal, find, stopFind }) });

  // A window with a hidden title bar tells its pages the room its window
  // controls take, from document start on: CSS variables on :root.
  runtime.on<TitleBar>("mygo:title-bar", (tb) =>
    setRootVars({
      "--mygo-titlebar-height": `${tb.height}px`,
      "--mygo-titlebar-inset-left": `${tb.left}px`,
      "--mygo-titlebar-inset-right": `${tb.right}px`,
    }),
  );

  // The accent color and the size of text the desktop asks for, which the
  // media queries do not tell a page. From document start on: CSS variables
  // on :root, with the accent left unset where the desktop has none.
  runtime.on<ThemePreferences>("mygo:preferences", (p) =>
    setRootVars({
      "--mygo-accent": p.accent,
      "--mygo-text-scale": String(p.textScale),
    }),
  );

  // dom-ready names the document, so that what Go held for the page is sent
  // for this one (Batch), and for no other the window shows by then.
  const notify = (t: "dom-ready" | "drag" | "dblclick" | "escape") => {
    try {
      post(JSON.stringify(t === "dom-ready" ? { t, k: token } : { t }));
    } catch {
      // The page is being torn down.
    }
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => notify("dom-ready"), { once: true });
  } else {
    notify("dom-ready");
  }

  const nativeRegion = typeof CSS !== "undefined" && CSS.supports("-webkit-app-region", "drag");
  const appRegion = (el: Element): string => {
    const custom = getComputedStyle(el).getPropertyValue("--app-region").trim();
    if (custom) return custom;
    if (!nativeRegion) return "";
    // The native property is not inherited, so walk up the tree.
    for (let node: Element | null = el; node; node = node.parentElement) {
      const value = getComputedStyle(node).getPropertyValue("-webkit-app-region").trim();
      if (value === "drag" || value === "no-drag") return value;
    }
    return "";
  };

  // Files dropped from outside: Go gets their paths next to the page's own
  // drag and drop, which is left as it is. The platform records the paths
  // before the page sees the drop, or WebView2 hands them over with the
  // File objects posted along with the message.
  let inPage = false; // a drag started in this page: none of our business
  w.addEventListener("dragstart", () => (inPage = true), true);
  w.addEventListener("dragend", () => (inPage = false), true);
  const external = (e: DragEvent) =>
    !inPage && !!e.dataTransfer && Array.from(e.dataTransfer.types).includes("Files");
  w.addEventListener(
    "drop",
    (e: DragEvent) => {
      const files = e.dataTransfer?.files;
      if (inPage || !files?.length) return;
      const m = __MYGO_CONFIG__.secret + JSON.stringify({ t: "drop", x: Math.round(e.clientX), y: Math.round(e.clientY) });
      try {
        raw.postWithFiles ? raw.postWithFiles(m, files) : raw.post(m);
      } catch {
        try {
          raw.post(m); // e.g. File objects not backed by a file
        } catch {
          // The page is being torn down.
        }
      }
    },
    true,
  );
  // Where the page does not handle dragged files, accept them rather than
  // letting the engine replace the page with the dropped file.
  const accept = (e: DragEvent) => {
    if (external(e) && !e.defaultPrevented) e.preventDefault();
  };
  w.addEventListener("dragover", accept);
  w.addEventListener("drop", accept);

  // An Escape the page does not handle (preventDefault) closes a flyout,
  // as a menu or popover. Handlers run in any order: look once all did.
  w.addEventListener("keydown", (e: KeyboardEvent) => {
    if (e.key !== "Escape" || e.isComposing) return;
    setTimeout(() => {
      if (!e.defaultPrevented) notify("escape");
    });
  });

  // Frameless windows: `--app-region: drag` (or `-webkit-app-region: drag`
  // where the engine supports it) turns an element into a window handle.
  w.addEventListener(
    "mousedown",
    (e: MouseEvent) => {
      if (e.button !== 0 || !(e.target instanceof Element)) return;
      if (appRegion(e.target) !== "drag" || e.target.closest(INTERACTIVE)) return;
      e.preventDefault();
      notify(e.detail === 2 ? "dblclick" : "drag");
    },
    true,
  );
})();

/** The room the window controls take, in CSS pixels. */
interface TitleBar {
  height: number;
  left: number;
  right: number;
}

/** The MyGo CSS variables of :root, kept from every source that sets one. */
const rootVars: Record<string, string | null> = {};

/** Sets MyGo's CSS variables of :root, keeping the ones already set. A null
 * value removes a variable, so a page's `var(--mygo-accent, AccentColor)`
 * fallback applies. */
function setRootVars(vars: Record<string, string | null>): void {
  Object.assign(rootVars, vars);
  const decls = Object.entries(rootVars)
    .filter(([, v]) => v !== null)
    .map(([k, v]) => `${k}:${v}`)
    .join(";");
  setRootStyle(decls ? `:root{${decls}}` : "");
}

let rootSheet: CSSStyleSheet | null = null;
let rootStyle: HTMLStyleElement | null = null;

/** Sets the rules of MyGo's own style sheet, which a Content Security Policy
 * allows as a constructed sheet. Engines without them get a style element. */
function setRootStyle(css: string): void {
  if ((document as Partial<Document>).adoptedStyleSheets) {
    if (!rootSheet) {
      rootSheet = new CSSStyleSheet();
      document.adoptedStyleSheets = [...document.adoptedStyleSheets, rootSheet];
    }
    rootSheet.replaceSync(css);
    return;
  }
  const style = (rootStyle ??= document.createElement("style"));
  style.textContent = css;
  const attach = () => {
    if (!style.isConnected) (document.head ?? document.documentElement)?.append(style);
  };
  if (document.documentElement) attach();
  else document.addEventListener("DOMContentLoaded", attach, { once: true });
}

interface Transport {
  post(message: string): void;
  /** Posts File objects along with a message (WebView2). */
  postWithFiles?(message: string, files: FileList): void;
}

/**
 * Takes the channel to Go and removes it from the page: the message handler
 * of WebKit, which the page reaches through the accessor of `webkit`'s
 * prototype (`webkit` itself cannot be removed), and `chrome.webview` of
 * WebView2. Scripts the page runs afterwards, and its frames (which the
 * backends' scripts do the same in), cannot see it, or tell this is not
 * Safari or Edge.
 */
function transport(w: any): Transport | null {
  const apply = Reflect.apply;
  const handler = w.webkit?.messageHandlers?.mygo;
  if (handler) {
    const postMessage = handler.postMessage;
    try {
      delete Object.getPrototypeOf(w.webkit).messageHandlers;
    } catch {
      // The page keeps what it could not lose.
    }
    return { post: (m) => apply(postMessage, handler, [m]) };
  }
  const webview = w.chrome?.webview;
  if (!webview) return null;
  const { postMessage, postMessageWithAdditionalObjects } = webview;
  try {
    delete w.chrome.webview;
  } catch {
    // As above.
  }
  return {
    post: (m) => apply(postMessage, webview, [m]),
    postWithFiles: postMessageWithAdditionalObjects
      ? (m, files) => apply(postMessageWithAdditionalObjects, webview, [m, files])
      : undefined,
  };
}
