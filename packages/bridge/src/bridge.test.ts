import { beforeAll, describe, expect, test } from "bun:test";
import { join } from "node:path";
import type { Trust } from "./trust";

// The bridge as the core injects it at document start: the bundle of
// bridge.ts, run as a function body with the page's `window`, `location` and
// `document` and the window's configuration.
let source = "";
beforeAll(async () => {
  const result = await Bun.build({
    entrypoints: [join(import.meta.dir, "bridge.ts")],
    format: "iife",
    target: "browser",
  });
  if (!result.success) throw new Error(result.logs.join("\n"));
  source = await result.outputs[0]!.text();
});

const trust: Trust = { schemes: ["file", "about"], origins: ["https://partner.example"], hosted: [], loopback: false };

/** A WebKit page at href, with the channel to Go, and what it posted. */
function page(href: string, rule: Trust = trust) {
  const posts: string[] = [];
  const u = new URL(href);
  const handler = { postMessage: (m: string) => posts.push(m) };
  // `webkit.messageHandlers` is an accessor of the prototype of `webkit`.
  const proto = { messageHandlers: { mygo: handler } };
  const w: any = { webkit: Object.create(proto), addEventListener() {} };
  w.window = w;
  const location = { protocol: u.protocol, host: u.host, hostname: u.hostname, href: u.href };
  const document = { readyState: "complete", addEventListener() {} };
  const config = { platform: "darwin", windowId: 1, version: "0", secret: "SECRET", locale: "en", dir: "ltr", trust: rule };
  new Function("window", "location", "document", "__MYGO_CONFIG__", source)(w, location, document, config);
  return { w, posts };
}

describe("a page that is not trusted", () => {
  test("has no bridge and no channel, but a window.__mygo of the bridge's that it cannot define", () => {
    const { w, posts } = page("https://evil.example/");
    expect(w.mygo).toBeUndefined();
    expect(w.webkit.messageHandlers).toBeUndefined();
    expect(posts).toEqual([]);

    const d = Object.getOwnPropertyDescriptor(w, "__mygo")!;
    expect(d.configurable).toBe(false);
    expect(d.writable).toBe(false);

    // The page's script comes after the bridge's.
    const leaks: unknown[] = [];
    const mine = { receive: (m: unknown) => leaks.push(m), deliver: (m: unknown) => leaks.push(m), find: () => ({ matches: 99, active: 1 }) };
    expect(() => (w.__mygo = mine)).toThrow(); // silently fails in a page's sloppy script
    expect(() => Object.defineProperty(w, "__mygo", { value: mine })).toThrow();
    expect(() => delete w.__mygo).toThrow();

    w.__mygo.receive([{ t: "reply", id: 1, k: "k", ok: true, v: "SECRET-RESULT" }]);
    w.__mygo.deliver({ m: [{ t: "event", n: "x", p: "SECRET-EVENT" }] });
    expect(leaks).toEqual([]);
    expect(Object.isFrozen(w.__mygo)).toBe(true);
    expect(typeof w.__mygo.find).toBe("function");
    expect(w.__mygo.find).not.toBe(mine.find);
  });

  test("an about: page of a window a page opened is one", () => {
    const popup: Trust = { schemes: ["file"], origins: [], hosted: [], loopback: false };
    const { w } = page("about:blank", popup);
    expect(w.mygo).toBeUndefined();
    expect(Object.getOwnPropertyDescriptor(w, "__mygo")!.configurable).toBe(false);
  });
});

describe("a trusted page", () => {
  test("has the bridge, which tells its document in dom-ready and takes what Go delivers for it", () => {
    const { w, posts } = page("https://partner.example/");
    expect(typeof w.mygo.call).toBe("function");
    expect(w.webkit.messageHandlers).toBeUndefined();
    expect(Object.getOwnPropertyDescriptor(w, "__mygo")!.configurable).toBe(false);

    const ready = posts.map((p) => JSON.parse(p.slice("SECRET".length))).find((m) => m.t === "dom-ready");
    expect(ready?.k).toBeString();

    const seen: unknown[] = [];
    w.mygo.on("tick", (v: unknown) => seen.push(v));
    w.__mygo.deliver({ d: ready.k, m: [{ t: "event", n: "tick", p: 1 }] });
    w.__mygo.deliver({ d: "another-document", m: [{ t: "event", n: "tick", p: 2 }] });
    expect(seen).toEqual([1]);
  });

  test("an about: page the app made has it as well", () => {
    const { w } = page("about:blank");
    expect(typeof w.mygo.call).toBe("function");
  });

  test("a page of a custom scheme served at a host of its own (WebView2) has it as well, one at a port or with credentials has not", () => {
    const web: Trust = { schemes: ["file"], origins: [], hosted: ["app"], loopback: false };
    expect(typeof page("http://abc.app.localhost/", web).w.mygo.call).toBe("function");
    expect(typeof page("http://app.localhost/", web).w.mygo.call).toBe("function");
    expect(page("http://abc.app.localhost:8080/", web).w.mygo).toBeUndefined();
    expect(page("http://user@abc.app.localhost/", web).w.mygo).toBeUndefined();
    expect(page("http://abc.other.localhost/", web).w.mygo).toBeUndefined();
  });
});
