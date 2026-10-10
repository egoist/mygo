import { expect, test } from "bun:test";
import { browserTransport, type BrowserDeps, type EventSourceLike } from "./browser";

function setup() {
  const posts: { url: string; body: string[]; done: (ok: boolean) => void }[] = [];
  const pings: ((ok: boolean | null) => void)[] = [];
  const received: unknown[][] = [];
  const timers: (() => void)[] = [];
  let reloaded = 0;
  let source!: EventSourceLike & { url: string; closed: boolean };
  const deps: BrowserDeps = {
    fetch: (url, init) =>
      new Promise((resolve, reject) => {
        if (init.method === "POST") {
          posts.push({ url, body: JSON.parse(init.body as string), done: (ok) => resolve({ ok }) });
        } else {
          pings.push((ok) => (ok === null ? reject(new Error("refused")) : resolve({ ok })));
        }
      }),
    EventSource: class {
      onmessage: EventSourceLike["onmessage"] = null;
      onerror: EventSourceLike["onerror"] = null;
      closed = false;
      constructor(readonly url: string) {
        source = this as any;
      }
      close() {
        this.closed = true;
      }
    } as any,
    receive: (m) => received.push(m),
    reload: () => reloaded++,
    setTimeout: (fn) => timers.push(fn),
  };
  const post = browserTransport("/__mygo/", "s 1", deps);
  return { post, posts, pings, received, timers, source: () => source, reloaded: () => reloaded };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

test("posts the messages of a task together, one request at a time", async () => {
  const t = setup();
  expect(t.source().url).toBe("/__mygo/events?s=s%201");
  t.post("a");
  t.post("b");
  await tick();
  expect(t.posts.map((p) => p.body)).toEqual([["a", "b"]]);
  expect(t.posts[0]!.url).toBe("/__mygo/post?s=s%201");
  t.post("c");
  t.post("d");
  await tick();
  // Still waiting for the first request.
  expect(t.posts.length).toBe(1);
  t.posts[0]!.done(true);
  await tick();
  expect(t.posts.map((p) => p.body)).toEqual([["a", "b"], ["c", "d"]]);
});

test("delivers server-sent batches", () => {
  const t = setup();
  t.source().onmessage!({ data: '[{"t":"event","n":"x","p":1}]' });
  expect(t.received).toEqual([[{ t: "event", n: "x", p: 1 }]]);
});

test("reloads once the app is back", async () => {
  const t = setup();
  t.source().onerror!();
  t.source().onerror!(); // only once
  expect(t.source().closed).toBe(true);
  expect(t.timers.length).toBe(1);
  // Nothing goes out while the app is away.
  t.post("lost");
  await tick();
  expect(t.posts.length).toBe(0);

  t.timers.shift()!();
  t.pings.shift()!(null); // refused: try again
  await tick();
  expect(t.timers.length).toBe(1);
  t.timers.shift()!();
  t.pings.shift()!(false); // not ready: try again
  await tick();
  expect(t.reloaded()).toBe(0);
  t.timers.shift()!();
  t.pings.shift()!(true);
  await tick();
  expect(t.reloaded()).toBe(1);
});
