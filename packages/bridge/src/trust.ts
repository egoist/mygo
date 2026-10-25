// Which pages get the bridge: the rule of isTrusted in Go (window.go), which
// the core sends as the `trust` of the bridge configuration, so that the
// page decides for itself at document start, before any of its scripts run.

/** The rule of isTrusted, as data. */
export interface Trust {
  /** Schemes whose pages are trusted, whatever their host. */
  schemes: string[];
  /** Lower case origins, scheme://host[:port], of trusted pages; "*" trusts every page. */
  origins: string[];
  /**
   * Schemes of the app that the web view serves at http://<scheme>.localhost,
   * and at http://<host>.<scheme>.localhost for the host of a URL (WebView2):
   * those pages are trusted, with no port, no credentials and a host of DNS labels.
   */
  hosted: string[];
  /** Pages of http and https on localhost, 127.0.0.1 and ::1 are trusted. */
  loopback: boolean;
}

// One lower case DNS label: letters, digits and hyphens, at most 63, not
// starting or ending with a hyphen (package schemehost).
const label = "[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?";
const dnsName = new RegExp(`^${label}(?:\\.${label})*$`);

// A URL with credentials, http://user@host/: the core maps no such URL to the
// app's scheme (package schemehost), so it does not trust it either.
const hasCredentials = /^[^:/?#]+:\/\/[^/?#]*@/;

/** Reports whether hostname, of a page at http, is one of a scheme of hosted. */
function isHosted(hostname: string, hosted: string[]): boolean {
  return hosted.some((scheme) => {
    const web = `${scheme}.localhost`;
    if (hostname === web) return true;
    const host = hostname.endsWith(`.${web}`) ? hostname.slice(0, -web.length - 1) : "";
    return host.length <= 253 && dnsName.test(host);
  });
}

/** Reports whether the page at loc is trusted. */
export function isTrusted(loc: Pick<Location, "protocol" | "host" | "hostname" | "href">, trust: Trust): boolean {
  const scheme = loc.protocol.replace(/:$/, "").toLowerCase();
  const host = loc.host.toLowerCase();
  const hostname = loc.hostname.toLowerCase();
  if (trust.schemes.includes(scheme)) return true;
  if (trust.origins.includes("*") || trust.origins.includes(`${scheme}://${host}`)) return true;
  // No port, so the host is the hostname alone, and no credentials.
  if (scheme === "http" && host === hostname && !hasCredentials.test(loc.href) && isHosted(hostname, trust.hosted)) return true;
  return (
    trust.loopback &&
    (scheme === "http" || scheme === "https") &&
    ["localhost", "127.0.0.1", "::1"].includes(hostname.replace(/^\[(.*)\]$/, "$1"))
  );
}
