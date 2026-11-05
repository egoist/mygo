import { createFileRoute } from "@tanstack/react-router"
import * as React from "react"

import { dataUrl, formatValue, frameworks, scenes, sourceUrl, startMeasures, workflowUrl, type Format, type Measure, type Results, type Series } from "@/lib/benchmarks"
import { site } from "@/lib/site"

const title = "Benchmarks · MyGo"
const description = "MyGo's native UI, GPUI and egui on the same apps: startup, memory, scrolling a long list, a live dashboard and wrapping text, measured on the same machine."
const link = "font-medium underline decoration-gopher/45 underline-offset-[5px] hover:decoration-gopher"

export const Route = createFileRoute("/benchmarks")({
  head: () => ({
    meta: [
      { title },
      { name: "description", content: description },
      { property: "og:title", content: title },
      { property: "og:description", content: description },
    ],
  }),
  component: Benchmarks,
})

type Load = { state: "loading" } | { state: "missing" } | { state: "error"; error: string } | { state: "ready"; data: Results }

function Benchmarks() {
  const [load, setLoad] = React.useState<Load>({ state: "loading" })

  // The results come from the benchmarks branch, newer than the page.
  React.useEffect(() => {
    const controller = new AbortController()
    fetch(dataUrl, { signal: controller.signal })
      .then(async (res) => {
        if (res.status === 404) return setLoad({ state: "missing" })
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        setLoad({ state: "ready", data: (await res.json()) as Results })
      })
      .catch((err: Error) => !controller.signal.aborted && setLoad({ state: "error", error: err.message }))
    return () => controller.abort()
  }, [])

  const data = load.state === "ready" ? load.data : undefined

  return (
    <main className="flex-1">
      <section className="border-b px-4 pt-16 pb-12 sm:px-10 md:pt-20">
        <p className="label">Benchmarks</p>
        <h1 className="mt-5 text-3xl leading-[1.1] font-semibold tracking-[-0.035em] sm:text-4xl">The same apps, in three frameworks.</h1>
        <p className="mt-5 max-w-2xl text-base leading-7 text-muted-foreground">
          The same small apps written with MyGo&apos;s native UI, GPUI and egui, each the usual way of its framework, showing the same text in windows of the same size, and
          measured on the same machine. Every release measures them again.
        </p>
        <p className="mt-6 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
          <a href={sourceUrl} className={link}>
            The apps
          </a>
          <a href={`${sourceUrl}#what-is-measured`} className={link}>
            How they are measured
          </a>
          <a href={workflowUrl} className={link}>
            The workflow
          </a>
          <a href={`${site.repo}/blob/benchmarks/comparison/latest.json`} className={link}>
            The data
          </a>
        </p>
        <Legend />
      </section>

      {load.state === "missing" && <Notice>No results yet: the workflow has not published any.</Notice>}
      {load.state === "error" && <Notice>The results could not load: {load.error}</Notice>}

      <Section title="Starting and resting" text="A number and three buttons, then nothing to do: what an app costs before it does anything.">
        <Bars title="App size" text="A release build, as its framework's tools make the smallest one." format="bytes" values={data && frameworks.map((f) => ({ id: f.id, series: flat(data.frameworks[f.id]?.size) }))} runs={data?.runs} />
        {startMeasures.map((m) => (
          <MeasureBars key={m.id} measure={m} scene="counter" data={data} />
        ))}
      </Section>

      {scenes.map((s) => (
        <Section key={s.id} title={s.title} text={s.text}>
          {s.measures.map((m) => (
            <MeasureBars key={m.id} measure={m} scene={s.id} data={data} />
          ))}
        </Section>
      ))}

      <section className="px-4 py-10 sm:px-10">
        <h2 className="text-xl font-semibold tracking-tight">Reading the results</h2>
        <ul className="mt-4 max-w-2xl list-disc space-y-2 pl-5 text-sm leading-6 text-muted-foreground">
          <li>Shorter bars are better, but for frame rate. Each bar is the median of the runs: hover it for the lowest and the highest.</li>
          <li>The machine is a virtual one, with a small display and a shared GPU: compare the frameworks with each other, not with what a laptop would do.</li>
          <li>CPU time leaves out what the GPU does. Two apps taking the same CPU may not draw at the same cost.</li>
          <li>GPUI&apos;s crate comes without widgets, so its buttons are styled elements, and it has no scroll bars of its own. egui and MyGo draw their own.</li>
        </ul>
        {data && <Setup data={data} />}
      </section>
    </main>
  )
}

function flat(value: number | undefined): Series | undefined {
  return value === undefined ? undefined : { median: value, low: value, high: value }
}

function Legend() {
  return (
    <ul className="mt-8 flex flex-wrap gap-x-6 gap-y-2 text-sm">
      {frameworks.map((f) => (
        <li key={f.id} className="flex items-center gap-2">
          <span className="size-2.5 rounded-[3px]" style={{ background: f.color }} />
          <a href={f.url} className="font-medium hover:underline hover:underline-offset-4">
            {f.label}
          </a>
        </li>
      ))}
    </ul>
  )
}

function Notice({ children }: { children: React.ReactNode }) {
  return <p className="border-b px-4 py-6 text-sm text-muted-foreground sm:px-10">{children}</p>
}

function Section({ title, text, children }: { title: string; text: string; children: React.ReactNode }) {
  return (
    <section className="border-b">
      <header className="px-4 pt-10 pb-6 sm:px-10">
        <h2 className="text-xl font-semibold tracking-tight">{title}</h2>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{text}</p>
      </header>
      <div className="grid gap-x-12 gap-y-10 px-4 pb-10 sm:px-10 md:grid-cols-2">{children}</div>
    </section>
  )
}

function MeasureBars({ measure: m, scene, data }: { measure: Measure; scene: string; data?: Results }) {
  const scaled = (s?: Series): Series | undefined => s && { median: s.median * (m.scale ?? 1), low: s.low * (m.scale ?? 1), high: s.high * (m.scale ?? 1) }
  return (
    <Bars
      title={m.title}
      text={m.text}
      format={m.format}
      higherIsBetter={m.higherIsBetter}
      runs={data?.runs}
      values={data && frameworks.map((f) => ({ id: f.id, series: scaled(data.frameworks[f.id]?.scenes[scene]?.[m.id]) }))}
    />
  )
}

/** One measure: a bar for each framework, from zero, on a scale of its own, with its value beside it. */
function Bars({
  title,
  text,
  format,
  higherIsBetter,
  runs,
  values,
}: {
  title: string
  text: string
  format: Format
  higherIsBetter?: boolean
  runs?: number
  values?: { id: string; series?: Series }[]
}) {
  const top = Math.max(...(values ?? []).map((v) => v.series?.median ?? 0), 0)
  return (
    <figure className="min-w-0">
      <figcaption>
        <h3 className="text-sm font-medium">
          {title}
          {higherIsBetter && <span className="ml-2 font-normal text-muted-foreground">higher is better</span>}
        </h3>
        <p className="mt-1 text-[13px] leading-5 text-muted-foreground">{text}</p>
      </figcaption>
      <ul className="mt-3 space-y-0.5">
        {frameworks.map((f) => {
          const s = values?.find((v) => v.id === f.id)?.series
          return (
            <li key={f.id} tabIndex={s ? 0 : undefined} className="group relative grid grid-cols-[3rem_1fr_5rem] items-center gap-2 rounded-sm py-1 outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <span className="text-[13px] text-muted-foreground">{f.label}</span>
              <span className="h-2.5 min-w-0">
                {s && <span className="block h-full rounded-r-[4px]" style={{ width: top > 0 ? `max(${(s.median / top) * 100}%, 2px)` : "2px", background: f.color }} />}
                {!values && <span className="block h-full w-1/2 animate-pulse rounded-r-[4px] bg-muted" />}
              </span>
              <span className="text-right font-mono text-[13px] tabular-nums">{s ? formatValue(s.median, format) : values ? "–" : ""}</span>
              {s && s.low !== s.high && (
                <span className="pointer-events-none absolute right-0 bottom-full z-10 mb-1 hidden rounded-md border bg-popover px-2 py-1 text-xs whitespace-nowrap text-popover-foreground shadow-sm group-hover:block group-focus-visible:block">
                  {f.label}: {formatValue(s.low, format)} to {formatValue(s.high, format)} over {runs} runs
                </span>
              )}
            </li>
          )
        })}
      </ul>
    </figure>
  )
}

function Setup({ data }: { data: Results }) {
  const { machine: m, versions: v } = data
  const rows: [string, string][] = [
    ["Measured", new Date(data.date).toLocaleDateString("en", { year: "numeric", month: "long", day: "numeric", timeZone: "UTC" })],
    ["Machine", `${m.cpu}, ${m.cpus} CPUs, ${Math.round(m.memory / 2 ** 30)} GB of memory${m.display ? `, ${m.display}` : ""}`],
    ["System", m.os],
    ["Frameworks", `MyGo ${v.mygo}, GPUI ${v.gpui}, egui ${v.egui}`],
    ["Compilers", `Go ${v.go}, Rust ${v.rustc}`],
    ["Runs", `${data.runs} of each scene, ${data.frames} frames each, frameworks taking turns`],
  ]
  return (
    <dl className="mt-8 grid max-w-2xl grid-cols-[6rem_1fr] gap-x-4 gap-y-1.5 text-sm">
      {rows.map(([k, val]) => (
        <React.Fragment key={k}>
          <dt className="text-muted-foreground">{k}</dt>
          <dd className="min-w-0">{val}</dd>
        </React.Fragment>
      ))}
      <dt className="text-muted-foreground">Commit</dt>
      <dd>
        <a href={`${site.repo}/commit/${data.commit}`} className="font-mono text-[13px] underline underline-offset-4">
          {data.commit.slice(0, 7)}
        </a>
      </dd>
    </dl>
  )
}
