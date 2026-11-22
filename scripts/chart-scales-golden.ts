// Reproduce the chart scales' reference without adding an app dependency:
// npm install --prefix /tmp/mygo-chart-reference --ignore-scripts --no-audit --no-fund d3-scale@4.0.2
// node scripts/chart-scales-golden.ts /tmp/mygo-chart-reference
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { gzipSync } from "node:zlib";

const root = process.argv[2];
if (!root) throw new Error("Pass the directory containing node_modules/d3-scale@4.0.2");
const pkg = resolve(root, "node_modules/d3-scale");
const { version } = JSON.parse(await readFile(join(pkg, "package.json"), "utf8"));
if (version !== "4.0.2") throw new Error(`Expected d3-scale 4.0.2, found ${version}`);
const { scaleLog, scaleSymlog, scaleTime } = await import(pathToFileURL(join(pkg, "src/index.js")).href);
const fractions = [-0.5, 0, 0.1, 0.25, 0.5, 0.75, 0.9, 1, 1.5].map(Math.fround);
const cases: object[] = [];
const logDomains = [[1, 1000], [0.001, 1], [0.3, 1.2], [30, 90], [1, 1], [1e-12, 1e12], [1e6, 3.4e6],
  [Number.MIN_VALUE, 1], [1e-320, 1e-300]];
const domains = (ds: number[][]) => ds.flatMap(([lo, hi]) => [[lo!, hi!], [hi!, lo!]]);
for (const kind of ["log", "symlog"]) {
  const ranges = kind === "log" ? domains(logDomains.flatMap(([lo, hi]) => [[lo!, hi!], [-hi!, -lo!]]))
    : domains([[0, 100], [-100, 100], [-100, -1], [-1e6, 1e6], [3.2, 7.9], [0, 0]]);
  for (const domain of ranges) for (const parameter of kind === "log" ? [2, Math.E, 10, 16, 100] : [0.01, 1, 10, 100]) {
    for (const count of [1, 2, 5, 10]) for (const clamp of [false, true]) {
      const scale = kind === "log" ? scaleLog().base(parameter) : scaleSymlog().constant(parameter);
      scale.domain(domain).clamp(clamp);
      const raw = scale.copy().clamp(false);
      // Extrapolating subnormal domains can underflow or overflow. Keep
      // these reference samples inside the representable domain.
      const fs = domain.some((v) => Math.abs(v) < 1e-300 && v !== 0)
        ? fractions.filter((f) => f >= 0 && f <= 1) : fractions;
      const values = fs.map((f) => raw.invert(f));
      cases.push({ kind, domain, parameter, count, clamp,
        values, positions: values.map((v) => scale(v)),
        fractions: fs, inversions: fs.map((f) => scale.invert(f)),
        ticks: scale.ticks(count), nice: scale.copy().nice(count).domain() });
    }
  }
}
const dir = resolve(import.meta.dirname, "../ui/chart/testdata");
await mkdir(dir, { recursive: true });
await writeFile(join(dir, "nonlinear_d3.json.gz"), gzipSync(JSON.stringify({ version, cases }) + "\n", { level: 9 }));
console.log(`${cases.length} cases from d3-scale ${version}`);

// Time scales, as scaleTime in UTC: the days of a step fall on the days of a
// month, as the port's do, which in another zone would be that zone's days.
if (new Date(0).getTimezoneOffset() !== 0) throw new Error("Run this with TZ=UTC");
const times: object[] = [];
const spans: [string, string][] = [
  ["2026-03-05T14:07:00.000Z", "2026-03-05T15:53:00.000Z"], // quarters of an hour
  ["2026-03-05T09:13:07.000Z", "2026-03-05T09:14:51.000Z"], // seconds
  ["2026-01-01T00:00:00.120Z", "2026-01-01T00:00:00.870Z"], // milliseconds
  ["2026-03-01T00:00:00.000Z", "2026-03-29T00:00:00.000Z"], // weeks
  ["2026-01-15T00:00:00.000Z", "2026-04-02T00:00:00.000Z"], // months
  ["2019-06-01T00:00:00.000Z", "2026-03-01T00:00:00.000Z"], // years
  ["2010-06-01T00:00:00.000Z", "2026-06-01T00:00:00.000Z"],
  ["2026-03-05T14:00:00.000Z", "2026-03-05T16:00:00.000Z"], // round already
];
for (const [from, to] of spans) {
  for (const [lo, hi] of [[from, to], [to, from]]) {
    for (const count of [1, 2, 3, 5, 7, 10]) {
      const domain = [Date.parse(lo), Date.parse(hi)];
      const scale = scaleTime().domain(domain);
      const niced = scale.copy().nice(count);
      // d3 leaves a domain that descends, of less than a second, as it is:
      // the step of a reversed pair is negative, and a millisecond of it is
      // what scaleTime.every gets. The port rounds it as it does any other,
      // so the descending cases record only their ticks.
      times.push({ domain, count,
        ticks: scale.ticks(count).map((d) => +d),
        nice: lo < hi ? niced.domain().map((d) => +d) : null,
        niceTicks: lo < hi ? niced.ticks(count).map((d) => +d) : null });
    }
  }
}
await writeFile(join(dir, "time_d3.json.gz"), gzipSync(JSON.stringify({ version, cases: times }) + "\n", { level: 9 }));
console.log(`${times.length} time cases from d3-scale ${version}`);
