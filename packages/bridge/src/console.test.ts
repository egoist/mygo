import { beforeEach, describe, expect, test } from "bun:test";
import { captureConsole, formatArgs, formatValue } from "./console";

describe("formatValue", () => {
  test("as the inspector shows it", () => {
    expect(formatValue("hi")).toBe("hi");
    expect(formatValue(1.5)).toBe("1.5");
    expect(formatValue(true)).toBe("true");
    expect(formatValue(undefined)).toBe("undefined");
    expect(formatValue(null)).toBe("null");
    expect(formatValue(10n)).toBe("10n");
    expect(formatValue({ a: 1 })).toBe('{"a":1}');
    expect(formatValue([1, "two"])).toBe('[1,"two"]');
  });

  test("a function by its name", () => {
    const named = function greet() {};
    expect(formatValue(named)).toBe("ƒ greet()");
    expect(formatValue(() => {})).toBe("ƒ anonymous()");
  });

  test("an error by its stack", () => {
    const err = new Error("boom");
    expect(formatValue(err)).toBe(err.stack ?? "");
  });

  test("a value JSON cannot take", () => {
    expect(formatValue(Symbol("s"))).toBe("Symbol(s)");
    const round: unknown = {};
    (round as { self?: unknown }).self = round;
    expect(formatValue(round)).toBe('{"self":"[Circular]"}');
    const bigintInside = { n: 1n };
    expect(formatValue(bigintInside)).toBe('{"n":"1n"}');
  });
});

describe("formatArgs", () => {
  test("arguments a space apart", () => {
    expect(formatArgs(["a", 1, true])).toBe("a 1 true");
    expect(formatArgs([])).toBe("");
  });

  test("a leading format string substitutes", () => {
    expect(formatArgs(["%s and %d", "a", 2])).toBe("a and 2");
    expect(formatArgs(["%i %f", 2.7, 1.25])).toBe("2 1.25");
    expect(formatArgs(["100%%", "%s"])).toBe("100% %s");
    expect(formatArgs(["%o of %O", { a: 1 }, [2]])).toBe('{"a":1} of [2]');
    expect(formatArgs(["%s stays", undefined])).toBe("undefined stays");
  });

  test("a string without a format reads bare", () => {
    expect(formatArgs(["50% of nothing", 1])).toBe("50% of nothing 1");
  });
});

describe("captureConsole", () => {
  const sent: string[] = [];
  const send = (m: string) => sent.push(m);

  beforeEach(() => {
    sent.length = 0;
  });

  test("sends what is logged, and the console keeps working", () => {
    const con = console as unknown as Record<string, (...a: unknown[]) => void>;
    const originals = { log: con.log!, error: con.error! };
    let logged = 0;
    const spy = (name: "log" | "error") => {
      con[name] = (...args: unknown[]) => {
        logged++;
        originals[name](...args);
      };
    };
    spy("log");
    spy("error");
    try {
      captureConsole(true, send);
      console.log("hello", { a: 1 });
      console.error("no");
      captureConsole(false, send);
      expect(logged).toBe(2); // the page's console still heard both
      expect(sent).toHaveLength(2);
      const log = JSON.parse(sent[0]!);
      const error = JSON.parse(sent[1]!);
      expect(log.t).toBe("console");
      expect(log.lv).toBe("log");
      expect(log.tx).toBe('hello {"a":1}');
      expect(error.lv).toBe("error");
      expect(error.tx).toBe("no");
    } finally {
      con.log = originals.log;
      con.error = originals.error;
    }
  });

  test("stopping unwraps the console", () => {
    const original = console.warn;
    captureConsole(true, send);
    captureConsole(false, send);
    expect(console.warn).toBe(original);
    console.warn("after");
    expect(sent).toEqual([]);
  });

  test("twice wraps once", () => {
    const original = console.info;
    captureConsole(true, send);
    const wrapped = console.info;
    captureConsole(true, send);
    expect(console.info).toBe(wrapped);
    console.info("x");
    captureConsole(false, send);
    expect(console.info).toBe(original);
    expect(sent).toHaveLength(1);
  });

  test("a long text is cut", () => {
    captureConsole(true, send);
    console.log("x".repeat(5000));
    captureConsole(false, send);
    const tx = JSON.parse(sent[0]!).tx as string;
    expect(tx.length).toBe(2001); // 2000 and the ellipsis
    expect(tx.endsWith("…")).toBe(true);
  });

  test("an uncaught error, with where it was thrown", () => {
    captureConsole(true, send);
    const err = new Error("boom");
    const e = new Event("error");
    Object.assign(e, { error: err, filename: "app.js", lineno: 3 });
    dispatchEvent(e);
    const rejection = new Event("unhandledrejection");
    Object.assign(rejection, { reason: "no" });
    dispatchEvent(rejection);
    captureConsole(false, send);
    const error = JSON.parse(sent[0]!);
    expect(error.lv).toBe("error");
    expect(error.tx.startsWith("Uncaught " + (err.stack ?? ""))).toBe(true);
    expect(error.u).toBe("app.js");
    expect(error.n).toBe(3);
    expect(JSON.parse(sent[1]!)).toEqual({
      t: "console",
      lv: "error",
      tx: "Unhandled promise rejection: no",
      u: "",
      n: 0,
    });
  });

  test("a log tells where it came from", () => {
    captureConsole(true, send);
    console.log("here");
    captureConsole(false, send);
    const got = JSON.parse(sent[0]!);
    // The caller's frame, this file, not the wrapper's.
    expect(got.u).toContain("console.test.ts");
    expect(got.n).toBeGreaterThan(0);
  });
});
