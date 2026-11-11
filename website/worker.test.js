import { expect, test } from "bun:test"
import worker from "./worker.js"

test("permanently redirects the old domain, preserving the path and query", async () => {
  for (const path of ["/", "/zh/docs/app?from=old&lang=zh", "/assets/app.js", "/docs/%E4%B8%AD%E6%96%87?q=a%2Fb"]) {
    const response = await worker.fetch(new Request(`https://mygo.egoist.dev${path}`), {
      ASSETS: { fetch() { throw new Error("Old-domain requests must not serve assets") } },
    })
    expect(response.status).toBe(308)
    expect(response.headers.get("Location")).toBe(`https://mygo.run${path}`)
  }
})

test("redirects HTTP requests to HTTPS on the new domain", async () => {
  const response = await worker.fetch(new Request("http://mygo.egoist.dev/docs?query=1"), {})
  expect(response.headers.get("Location")).toBe("https://mygo.run/docs?query=1")
})

test("serves canonical, preview and local requests without redirecting", async () => {
  for (const host of ["mygo.run", "preview-mygo.example.workers.dev", "localhost:8787"]) {
    const request = new Request(`https://${host}/zh/docs/app?query=1`)
    const asset = new Response("page", { status: 200 })
    const response = await worker.fetch(request, {
      ASSETS: { fetch(received) { expect(received).toBe(request); return asset } },
    })
    expect(response).toBe(asset)
  }
})
