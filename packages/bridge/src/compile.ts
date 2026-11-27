// What Page.Eval compiles code with (bridge.go): the AsyncFunction
// constructor, the errors' prototypes and the Trusted Types default
// policy's getter, taken at document start, before the page's scripts run,
// so that a page that replaces them can neither read the code nor make one
// answer look like another.

const AsyncFunction = Object.getPrototypeOf(async () => {}).constructor as (source: string) => () => Promise<unknown>;
const getPrototypeOf = Object.getPrototypeOf;
const apply = Reflect.apply;
const syntaxError = SyntaxError.prototype;
const evalError = EvalError.prototype;
const trustedTypes = (globalThis as { trustedTypes?: object }).trustedTypes;
const defaultPolicy = trustedTypes && Object.getOwnPropertyDescriptor(getPrototypeOf(trustedTypes), "defaultPolicy")?.get;

/**
 * compile returns source compiled as the body of an async function, or
 * one of these, when nothing ran:
 *
 * - "S" and the message of its SyntaxError if it does not compile;
 * - "C" if the page would see it: its Trusted Types default policy is
 *   given what is compiled, and a page that refuses to compile strings
 *   (its Content Security Policy) throws an EvalError;
 * - "E" and the error of another failure to compile it, such as code
 *   nested too deep.
 */
export function compile(source: string): (() => Promise<unknown>) | string {
  if (defaultPolicy && apply(defaultPolicy, trustedTypes, [])) return "C";
  try {
    return AsyncFunction(source);
  } catch (e) {
    const prototype = e != null && getPrototypeOf(e);
    if (prototype === syntaxError) return "S" + (e as SyntaxError).message;
    return prototype === evalError ? "C" : "E" + String(e);
  }
}
