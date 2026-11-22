import type {
  BridgeConfig,
  CallOptions,
  Channel,
  Direction,
  Incoming,
  LocaleChange,
  Outgoing,
  Runtime,
  Source,
  ThemePreferences,
} from "./types";

type Pending = {
  method: string;
  resolve: (v: any) => void;
  reject: (e: unknown) => void;
  channels?: ChannelState[];
  sources?: SourceState[];
  /** Told when the call is over, to let go of what it holds. */
  done?: () => void;
};
type Listener = (payload: any) => void;

/** The side of a channel that Go messages reach. */
interface ChannelState {
  /** Set when the channel is passed to a call. */
  id: number;
  readonly closed: boolean;
  /** Delivers a value; ask requests an acknowledgment once it is taken. */
  push(value: unknown, ask: boolean): void;
  /** Go closed the channel. */
  end(): void;
}

/** The side of a source that Go messages reach. */
interface SourceState {
  /** Set when the source is passed to a call. */
  id: number;
  readonly closed: boolean;
  /** The method took the values up to the n-th. */
  ack(n: number): void;
  /** The method closed the source. */
  close(): void;
}

/** Reports an error thrown by page code without stopping the runtime. */
const report = (err: unknown) =>
  setTimeout(() => {
    throw err;
  });

/** The reason an aborted signal gives: its own, or a plain error. */
function abortReason(signal: AbortSignal): unknown {
  if (signal.reason !== undefined) return signal.reason;
  return new Error("mygo: the call was aborted");
}

/** Internal API called by the Go side through `window.__mygo`. */
export interface Internal {
  /** Delivers one or more messages from Go. */
  receive(messages: Incoming | Incoming[]): void;
}

/** Error thrown by `call` when the Go method returns an error. */
export class CallError extends Error {
  constructor(
    readonly method: string,
    message: string,
  ) {
    super(message);
    this.name = "CallError";
  }
}

/**
 * Creates the renderer runtime. `post` delivers a serialized message to the
 * Go side. Kept free of DOM access so it can be unit tested.
 */
export function createRuntime(
  config: BridgeConfig,
  post: (message: string) => void,
): { runtime: Runtime; internal: Internal; restored: () => void } {
  // A per-page token keeps replies addressed to a previous page (before a
  // navigation) from resolving promises of the current one.
  const token = Math.random().toString(36).slice(2) + Date.now().toString(36);
  const pending = new Map<number, Pending>();
  const listeners = new Map<string, Set<Listener>>();
  // The desktop's preferences, which Go tells at document start and on
  // every change; the defaults stand until then.
  let preferences: ThemePreferences = {
    accent: null,
    reduceMotion: false,
    reduceTransparency: false,
    highContrast: false,
    textScale: 1,
  };
  let seq = 0;
  // Channels passed to calls, by id, and the state of every channel;
  // sources likewise, which the page fills instead.
  const channels = new Map<number, ChannelState>();
  const states = new WeakMap<object, ChannelState>();
  const sources = new Map<number, SourceState>();
  const sourceStates = new WeakMap<object, SourceState>();
  let channelSeq = 0;
  // The language the app shows: the configuration tells it at document
  // start, and the mygo:locale event when it changes.
  let locale = config.locale || "en";
  let dir: Direction = config.dir === "rtl" ? "rtl" : "ltr";
  // Whether the window is drawn as active: pages start active, and the
  // mygo:window-active event tells otherwise.
  let windowActive = true;

  const send = (message: Outgoing) => post(JSON.stringify(message));

  const call = <T>(method: string, ...args: unknown[]): Promise<T> => sendCall<T>(method, args);

  // callWith calls a bound Go method, which the page may cancel with the
  // signal of the options: Go is told to cancel the call, and the promise
  // rejects with the signal's reason.
  const callWith = <T>(method: string, args: unknown[], options?: CallOptions): Promise<T> => {
    const signal = options?.signal;
    if (!signal) return sendCall<T>(method, args);
    if (signal.aborted) return Promise.reject(abortReason(signal));
    return sendCall<T>(method, args, signal);
  };

  const sendCall = <T>(method: string, args: unknown[], signal?: AbortSignal): Promise<T> => {
    const id = ++seq;
    return new Promise<T>((resolve, reject) => {
      // Channels among the arguments stream values back; sources stream
      // values to the method.
      let chans: ChannelState[] | undefined;
      let srcs: SourceState[] | undefined;
      const drop = () => {
        chans?.forEach((c) => {
          channels.delete(c.id);
          c.id = 0;
        });
        srcs?.forEach((s) => {
          s.close(); // rejects sends waiting for room
          s.id = 0;
        });
      };
      for (const arg of args) {
        const obj = typeof arg === "object" && arg !== null ? arg : undefined;
        const ch = obj ? states.get(obj) : undefined;
        if (ch) {
          if (ch.id || ch.closed) {
            drop();
            reject(new Error("mygo: a channel can be passed to one call, before it is closed"));
            return;
          }
          ch.id = ++channelSeq;
          channels.set(ch.id, ch);
          (chans ??= []).push(ch);
          continue;
        }
        const src = obj ? sourceStates.get(obj) : undefined;
        if (src) {
          if (src.id || src.closed) {
            drop();
            reject(new Error("mygo: a source can be passed to one call, before it is closed"));
            return;
          }
          src.id = ++channelSeq;
          sources.set(src.id, src);
          (srcs ??= []).push(src);
        }
      }
      // A signal that aborts tells Go to cancel the call, and rejects the
      // promise here: the reply of a canceled call is of no use.
      let onAbort: (() => void) | undefined;
      let done: (() => void) | undefined;
      if (signal) {
        onAbort = () => {
          pending.delete(id);
          drop();
          try {
            send({ t: "cancel", id, k: token });
          } catch {
            // The page is being torn down.
          }
          chans?.forEach((c) => c.end());
          reject(abortReason(signal));
        };
        signal.addEventListener("abort", onAbort, { once: true });
        done = () => signal.removeEventListener("abort", onAbort!);
      }
      pending.set(id, { method, resolve, reject, channels: chans, sources: srcs, done });
      try {
        send({ t: "call", id, k: token, m: method, a: args, cancel: signal ? true : undefined });
      } catch (err) {
        pending.delete(id);
        if (onAbort) signal?.removeEventListener("abort", onAbort);
        drop();
        reject(err);
      }
    });
  };

  const channel = <T>(onmessage?: (value: T) => void): Channel<T> => {
    let handler: ((value: T) => void) | null = null;
    let closed = false;
    // Values not taken yet, and next() calls waiting for one.
    let queue: { value: T; n: number; ask: boolean }[] = [];
    const waiting: ((result: IteratorResult<T>) => void)[] = [];
    let received = 0;

    // Tells Go the page took the values up to the n-th.
    const ack = (n: number) => {
      try {
        send({ t: "chan-ack", c: state.id, k: token, n });
      } catch {
        // The page is being torn down.
      }
    };
    const handle = (value: T) => {
      try {
        handler!(value);
      } catch (err) {
        report(err);
      }
    };
    const finish = () => {
      channels.delete(state.id);
      if (queue.length === 0) waiting.splice(0).forEach((w) => w({ value: undefined, done: true }));
      if (ch.onclose) {
        try {
          ch.onclose();
        } catch (err) {
          report(err);
        }
      }
    };

    const state: ChannelState = {
      id: 0,
      get closed() {
        return closed;
      },
      push(value, ask) {
        if (closed) return;
        const n = ++received;
        if (handler) handle(value as T);
        else if (waiting.length > 0) waiting.shift()!({ value: value as T, done: false });
        else return void queue.push({ value: value as T, n, ask });
        if (ask) ack(n);
      },
      end() {
        if (closed) return;
        closed = true;
        finish(); // values not taken yet can still be iterated
      },
    };

    const ch = {
      get onmessage() {
        return handler;
      },
      set onmessage(fn: ((value: T) => void) | null) {
        handler = fn;
        // Values that arrived earlier come first.
        while (handler && queue.length > 0) {
          const q = queue.shift()!;
          handle(q.value);
          if (q.ask) ack(q.n);
        }
      },
      onclose: null as (() => void) | null,
      get closed() {
        return closed;
      },
      close() {
        if (closed) return;
        closed = true;
        queue = [];
        if (state.id) {
          try {
            send({ t: "chan-close", c: state.id, k: token });
          } catch {
            // The page is being torn down.
          }
        }
        finish();
      },
      [Symbol.asyncIterator](): AsyncIterator<T> {
        return {
          next() {
            const q = queue.shift();
            if (q) {
              if (q.ask) ack(q.n);
              return Promise.resolve({ value: q.value, done: false });
            }
            if (closed) return Promise.resolve({ value: undefined, done: true });
            return new Promise((resolve) => waiting.push(resolve));
          },
          // Leaving a for await loop early stops the stream.
          return() {
            ch.close();
            return Promise.resolve({ value: undefined, done: true });
          },
        };
      },
      // Goes to Go as its id.
      toJSON: () => state.id,
    };
    states.set(ch, state);
    if (onmessage) ch.onmessage = onmessage;
    return ch;
  };

  // How many bytes of values may wait for the method to take them, as
  // Go's sourceWindow. A value counts as its own text, in the UTF-16 units
  // this counts, which is what Go counts in its bytes: a message's envelope
  // is the page's business, and a page sending short values has many of
  // them, so counting it would fill the window before Go had taken a
  // quarter and no acknowledgment would come.
  const sourceWindow = 1 << 20;

  const source = <T>(): Source<T> => {
    let closed = false;
    // Bytes sent and taken by the method, and the cumulative sent bytes
    // of each value, by its sequence number, to read acknowledgments by.
    let sent = 0;
    let taken = 0;
    let seq = 0;
    const asks: { seq: number; sent: number }[] = [];
    // Values waiting for room, in send order.
    const waiters: { n: number; size: number; body: string; resolve: () => void; reject: (e: unknown) => void }[] = [];

    const closedError = () => new Error("mygo: the source is closed");
    // The token as the messages carry it, quoted once.
    const quotedToken = JSON.stringify(token);
    const flush = () => {
      while (waiters.length > 0 && sent - taken < sourceWindow) {
        const w = waiters.shift()!;
        try {
          post(w.body);
          sent += w.size;
          asks.push({ seq: w.n, sent });
          w.resolve();
        } catch (err) {
          w.reject(err);
        }
      }
    };

    const state: SourceState = {
      id: 0,
      get closed() {
        return closed;
      },
      ack(n) {
        while (asks.length > 0 && asks[0]!.seq <= n) {
          taken = asks[0]!.sent;
          asks.shift();
        }
        flush();
      },
      close() {
        if (closed) return;
        closed = true;
        sources.delete(state.id);
        waiters.splice(0).forEach((w) => w.reject(closedError()));
      },
    };

    const src = {
      send(value: T): Promise<void> {
        if (closed) return Promise.reject(closedError());
        if (!state.id) {
          return Promise.reject(new Error("mygo: pass the source to a call before sending"));
        }
        const n = ++seq;
        // The value's own text, which is what Go counts when the method
        // takes it: what the page sends around it is not a value.
        const text = JSON.stringify(value);
        const size = text === undefined ? 0 : text.length;
        const body =
          text === undefined
            ? `{"t":"src-push","c":${state.id},"k":${quotedToken}}`
            : `{"t":"src-push","c":${state.id},"k":${quotedToken},"p":${text}}`;
        if (waiters.length === 0 && sent - taken < sourceWindow) {
          try {
            post(body);
            sent += size;
            asks.push({ seq: n, sent });
            return Promise.resolve();
          } catch (err) {
            return Promise.reject(err);
          }
        }
        return new Promise<void>((resolve, reject) => {
          waiters.push({ n, size, body, resolve, reject });
        });
      },
      close() {
        if (closed) return;
        closed = true;
        if (state.id) {
          try {
            send({ t: "src-end", c: state.id, k: token });
          } catch {
            // The page is being torn down.
          }
        }
        sources.delete(state.id);
        waiters.splice(0).forEach((w) => w.reject(closedError()));
      },
      get closed() {
        return closed;
      },
      // Goes to Go as its id.
      toJSON: () => state.id,
    };
    sourceStates.set(src, state);
    return src;
  };

  const on = (event: string, listener: Listener) => {
    if (typeof listener !== "function") throw new TypeError("listener must be a function");
    let set = listeners.get(event);
    if (!set) listeners.set(event, (set = new Set()));
    // Wrap so the same function can be subscribed twice and removed once.
    const entry: Listener = (p) => listener(p);
    set.add(entry);
    return () => {
      set.delete(entry);
      if (set.size === 0 && listeners.get(event) === set) listeners.delete(event);
    };
  };

  const once = (event: string, listener: Listener) => {
    const off = on(event, (p) => {
      off();
      listener(p);
    });
    return off;
  };

  const internal: Internal = {
    receive(messages) {
      for (const msg of Array.isArray(messages) ? messages : [messages]) {
        if (msg.t === "event") {
          // Before the listeners, which may read it.
          if (msg.n === "mygo:preferences") preferences = Object.freeze(msg.p as ThemePreferences);
          if (msg.n === "mygo:window-active") {
            if (typeof msg.p !== "boolean") continue;
            // Listeners hear changes only.
            if (msg.p === windowActive) continue;
            windowActive = msg.p;
          }
          if (msg.n === "mygo:locale") {
            const p = msg.p as Partial<LocaleChange> | undefined;
            if (typeof p?.locale !== "string") continue;
            // Go tells a page the language again as it loads, for one that
            // started loading as it changed: listeners hear of a language
            // the page did not have only.
            if (p.locale === locale) continue;
            locale = p.locale;
            dir = p.dir === "rtl" ? "rtl" : "ltr";
          }
          const set = listeners.get(msg.n);
          if (!set) continue;
          for (const listener of [...set]) {
            try {
              listener(msg.p);
            } catch (err) {
              // One failing listener must not break the others.
              report(err);
            }
          }
        } else if (msg.t === "chan") {
          if (msg.k !== token) continue;
          const ch = channels.get(msg.c);
          if (!ch) continue;
          if (msg.end) ch.end();
          else ch.push(msg.p, msg.a === 1);
        } else if (msg.t === "src-ack") {
          if (msg.k !== token) continue;
          sources.get(msg.c)?.ack(msg.n);
        } else if (msg.t === "src-close") {
          if (msg.k !== token) continue;
          sources.get(msg.c)?.close();
        } else if (msg.t === "reply") {
          if (msg.k !== token) continue;
          const p = pending.get(msg.id);
          if (!p) continue;
          pending.delete(msg.id);
          p.done?.();
          // Go closed the call's channels before replying, unless the call
          // failed before they were made; its sources too.
          p.channels?.forEach((c) => c.end());
          p.sources?.forEach((s) => s.close());
          if (msg.ok) p.resolve(msg.v);
          else p.reject(new CallError(p.method, msg.e ?? "unknown error"));
        }
      }
    },
  };

  // The page shows again from the web view's cache of pages, as going
  // back to it does: Go canceled the calls it had not answered as the page
  // left, and their replies went to the page that took its place.
  const restored = () => {
    const left = [...pending.values()];
    pending.clear();
    for (const p of left) {
      p.done?.();
      p.channels?.forEach((c) => c.end());
      p.sources?.forEach((s) => s.close());
      p.reject(new CallError(p.method, "the page was left before the call returned"));
    }
  };

  const win = (op: string) => () => call<any>(`mygo:window.${op}`);
  const runtime: Runtime = Object.freeze({
    call,
    callWith,
    channel,
    source,
    on,
    once,
    platform: config.platform,
    windowId: config.windowId,
    version: config.version,
    get preferences() {
      return preferences;
    },
    get locale() {
      return locale;
    },
    get dir() {
      return dir;
    },
    get windowActive() {
      return windowActive;
    },
    window: Object.freeze({
      minimize: win("Minimize"),
      maximize: win("Maximize"),
      unmaximize: win("Unmaximize"),
      toggleMaximize: win("ToggleMaximize"),
      isMaximized: win("IsMaximized"),
      toggleFullScreen: win("ToggleFullScreen"),
      close: win("Close"),
      setTitle: (title: string) => call<void>("mygo:window.SetTitle", title),
    }),
  });

  return { runtime, internal, restored };
}
