import { afterEach, describe, expect, test } from "bun:test";

// The page's Trusted Types, which compile takes as it loads.
let policy: object | null = null;
class TrustedTypePolicyFactory {
  get defaultPolicy() {
    return policy;
  }
}
(globalThis as { trustedTypes?: object }).trustedTypes = new TrustedTypePolicyFactory();
const { compile } = await import("./compile");

const AsyncFunctionPrototype = Object.getPrototypeOf(async () => {});
const constructor = Object.getOwnPropertyDescriptor(AsyncFunctionPrototype, "constructor")!;
const syntaxError = globalThis.SyntaxError;

const defaultPolicy = Object.getOwnPropertyDescriptor(TrustedTypePolicyFactory.prototype, "defaultPolicy")!;

afterEach(() => {
  Object.defineProperty(AsyncFunctionPrototype, "constructor", constructor);
  Object.defineProperty(TrustedTypePolicyFactory.prototype, "defaultPolicy", defaultPolicy);
  globalThis.SyntaxError = syntaxError;
  policy = null;
});

describe("compile", () => {
  test("compiles a function body, run when called", async () => {
    const f = compile("const a = await 1; return a + 1");
    expect(typeof f).toBe("function");
    expect(await (f as () => Promise<unknown>)()).toBe(2);
  });

  test("answers the SyntaxError of code that does not compile", () => {
    expect(compile("let = = ;")).toStartWith("S");
    expect(compile("let = = ;")).not.toBe("S");
  });

  test("uses what it took before the page could replace it", async () => {
    const seen: string[] = [];
    Object.defineProperty(AsyncFunctionPrototype, "constructor", {
      value: (source: string) => {
        seen.push(source);
        return async () => "forged";
      },
      configurable: true,
    });
    globalThis.SyntaxError = class {} as unknown as SyntaxErrorConstructor;
    expect(await (compile("return 'real'") as () => Promise<unknown>)()).toBe("real");
    expect(compile("let = = ;")).toStartWith("S");
    expect(seen).toEqual([]);
  });

  test("answers C, compiling nothing, where a Trusted Types default policy would be given the code", () => {
    policy = { createScript: (s: string) => s };
    // A page that hides its policy from the getter is still found out.
    Object.defineProperty(TrustedTypePolicyFactory.prototype, "defaultPolicy", { get: () => null, configurable: true });
    expect(compile("return 1")).toBe("C");
  });

  test("answers other failures to compile as errors, not as a refusal", () => {
    const deep = "(".repeat(100000) + "1" + ")".repeat(100000);
    expect(compile("return " + deep)).toStartWith("ERangeError");
  });
});
