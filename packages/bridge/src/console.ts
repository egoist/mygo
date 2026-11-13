// What the page logs and fails to catch (Page.OnConsoleMessage in Go):
// the console methods are wrapped — the page's own console keeps working,
// its messages still show in the inspector — and uncaught errors and
// unhandled rejections listened for, the same way on WKWebView,
// WebKitGTK and WebView2, none of which tells Go of all three.

/** The longest text a message sends, cut after it: a page logging large
 * values in a loop cannot flood Go. */
const maxText = 2000;

/** The console methods, by the level Go hears them as. */
const methods = { debug: "debug", log: "log", info: "info", warn: "warn", error: "error" } as const;

/** Takes the console back, after captureConsole(true) gave it. */
let unwrap: (() => void) | null = null;

/**
 * Starts or stops sending what the page logs and fails to catch to send,
 * as JSON messages the Go side hears. Starting twice does nothing, as
 * does stopping what never started; the page's console behaves the same
 * either way, its messages still going to the inspector.
 */
export function captureConsole(on: boolean, send: (message: string) => void): void {
  if (on) {
    if (unwrap) return;
    const con = console as unknown as Record<string, (...args: unknown[]) => void>;
    const saved: Record<string, (...args: unknown[]) => void> = {};
    for (const name of Object.keys(methods)) {
      const original = con[name]!;
      saved[name] = original;
      con[name] = function mygoLogged(this: unknown, ...args: unknown[]) {
        // The stack is taken here, where the first frame of it is this
        // wrapper's own, whatever the bundler calls it.
        const at = site(new Error().stack ?? "");
        send(message(methods[name as keyof typeof methods], formatArgs(args), at.u, at.n));
        return original.apply(this, args);
      };
    }
    // An error the page did not catch, and a promise it did not settle
    // with a handler of the rejection. The message some engines give has
    // the Uncaught of their own, which ours replaces.
    const error = (e: ErrorEvent) => {
      const err = e.error;
      const bare = (e.message || "").replace(/^Uncaught /, "");
      const text = err instanceof Error ? err.stack || String(err) : bare || String(err);
      send(message("error", "Uncaught " + text, e.filename, e.lineno));
    };
    const rejection = (e: PromiseRejectionEvent) => {
      const reason = e.reason;
      const text = reason instanceof Error ? reason.stack || String(reason) : formatValue(reason);
      send(message("error", "Unhandled promise rejection: " + text));
    };
    addEventListener("error", error);
    addEventListener("unhandledrejection", rejection);
    unwrap = () => {
      for (const name of Object.keys(saved)) con[name] = saved[name]!;
      removeEventListener("error", error);
      removeEventListener("unhandledrejection", rejection);
      unwrap = null;
    };
    return;
  }
  unwrap?.();
}

/** The message of a console record: its level, its text, and the script
 * it came from, empty when the record names none. */
function message(level: string, text: string, u?: string, n?: number): string {
  if (text.length > maxText) text = text.slice(0, maxText) + "…";
  return JSON.stringify({ t: "console", lv: level, tx: text, u: u ?? "", n: n ?? 0 });
}

/** Where a log came from, from the stack its wrapper took: the first
 * frame of the stack outside the wrapper, when the page's engine names
 * one. The wrapper's frame comes first, whatever the bundler calls it. */
function site(stack: string): { u: string; n: number } {
  let own = true; // the first frame that names a place is the wrapper's
  for (const line of stack.split("\n")) {
    if (line.includes("[native code]")) continue;
    // A frame's place: "at url:line:col", "fn@url:line:col" or a bare
    // "url:line:col" at the line's end, however the engine writes stacks.
    const at = line.match(/(\S+?):(\d+)(?::\d+)?\)?\s*$/);
    if (!at) continue; // the Error header, or a frame without a place
    if (own) {
      own = false;
      continue;
    }
    return { u: at[1]!.replace(/^\(/, ""), n: Number(at[2]) || 0 };
  }
  return { u: "", n: 0 };
}

/**
 * Formats what the page logs as the inspector shows it, the arguments a
 * space apart: a leading format string substitutes, strings show bare,
 * and other values as JSON, or as their kind when JSON cannot take them.
 */
export function formatArgs(values: unknown[]): string {
  const fmt = values[0];
  const parts: string[] = [];
  let i = 0;
  if (typeof fmt === "string" && fmt.includes("%")) {
    i = 1;
    parts.push(
      fmt.replace(/%[sdifoO%]/g, (spec) => {
        if (spec === "%%") return "%";
        if (i >= values.length) return spec; // no argument left to take
        const v = values[i++];
        switch (spec[1]) {
          case "s":
            return String(v);
          case "d":
          case "i":
            return String(Math.trunc(Number(v)));
          case "f":
            return String(Number(v));
          default: // %o and %O
            return formatValue(v);
        }
      }),
    );
  }
  for (; i < values.length; i++) parts.push(formatValue(values[i]));
  return parts.join(" ");
}

/** Formats one value as the inspector shows it. */
export function formatValue(v: unknown): string {
  switch (typeof v) {
    case "string":
      return v;
    case "number":
    case "boolean":
    case "undefined":
    case "symbol":
      return String(v);
    case "bigint":
      return String(v) + "n";
    case "function":
      return `ƒ ${((v as { name?: string }).name || "anonymous")}()`;
  }
  if (v === null) return "null";
  if (v instanceof Error) return v.stack || String(v);
  if (typeof Element === "function" && v instanceof Element) return "<" + v.localName + ">";
  const seen = new Set<object>();
  try {
    return (
      JSON.stringify(v, (_k, x: unknown) => {
        if (typeof x === "bigint") return String(x) + "n";
        if (typeof x === "object" && x !== null) {
          if (seen.has(x)) return "[Circular]";
          seen.add(x);
        }
        return x;
      }) ?? "undefined"
    );
  } catch {
    return Object.prototype.toString.call(v);
  }
}
