// The transport of a browser tab that `mygo dev` serves the frontend to
// (devbrowser.go): messages go to Go in POST requests, one at a time so
// they arrive in order, and come back as server-sent events.

/** What the transport needs from the browser, replaced by tests. */
export interface BrowserDeps {
  fetch: (url: string, init: RequestInit) => Promise<{ ok: boolean }>;
  EventSource: new (url: string) => EventSourceLike;
  receive: (messages: unknown[]) => void;
  reload: () => void;
  setTimeout: (fn: () => void, ms: number) => unknown;
}

export interface EventSourceLike {
  onmessage: ((e: { data: string }) => void) | null;
  onerror: (() => void) | null;
  close(): void;
}

/** How often a tab whose app went away checks whether it is back. */
export const RETRY_MS = 500;

/**
 * Connects a tab to the app at endpoint (such as "/__mygo/") as session,
 * and returns the function that posts a message to Go. When the app goes
 * away, as `mygo dev` rebuilds it, the tab reloads once it is back: the
 * new process knows nothing of this page.
 */
export function browserTransport(endpoint: string, session: string, deps: BrowserDeps): (message: string) => void {
  const q = "?s=" + encodeURIComponent(session);
  let gone = false;
  const events = new deps.EventSource(endpoint + "events" + q);
  events.onmessage = (e) => deps.receive(JSON.parse(e.data));
  events.onerror = () => {
    if (gone) return;
    gone = true;
    events.close();
    console.info("[mygo] lost the app; this page reloads once it is back");
    const retry = () =>
      deps
        .fetch(endpoint + "ping", { method: "GET", cache: "no-store" })
        .then((r) => (r.ok ? deps.reload() : deps.setTimeout(retry, RETRY_MS)))
        .catch(() => deps.setTimeout(retry, RETRY_MS));
    deps.setTimeout(retry, RETRY_MS);
  };

  // Messages posted during one task go together.
  let queue: string[] = [];
  let sending = false;
  const pump = () => {
    if (sending || queue.length === 0 || gone) return;
    sending = true;
    const body = JSON.stringify(queue);
    queue = [];
    deps
      .fetch(endpoint + "post" + q, { method: "POST", body, headers: { "Content-Type": "text/plain" } })
      .catch(() => {})
      .finally(() => {
        sending = false;
        pump();
      });
  };
  return (message) => {
    queue.push(message);
    if (queue.length === 1) queueMicrotask(pump);
  };
}
