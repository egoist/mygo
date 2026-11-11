export default {
  fetch(request, env) {
    const url = new URL(request.url)
    if (url.hostname === "mygo.egoist.dev") {
      url.protocol = "https:"
      url.hostname = "mygo.run"
      url.port = ""
      return Response.redirect(url.href, 308)
    }
    return env.ASSETS.fetch(request)
  },
}
