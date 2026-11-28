// The website data of pages: the WKWebsiteDataStores of sessions, and
// their cookies (WKHTTPCookieStore), for package mygo's Session and
// Cookies. Go matches the cookies with a URL and sorts them, as on macOS.

import Foundation
import WebKit

/// The stores of sessions, by the numbers Go knows them by; 0 is the
/// default store, which the web views of the default session use.
enum DataStores {
    private static var stores: [Int: WKWebsiteDataStore] = [:]
    private static var next = 0

    /// The store of n, nil for one closed.
    static func store(_ n: Int) -> WKWebsiteDataStore? { n == 0 ? .default() : stores[n] }

    /// A store kept on disk under the session's UUID (iOS 17, the host's
    /// oldest), or in memory for an ephemeral session.
    static func make(_ c: NewDataStoreCall) -> NewDataStoreReply {
        var r = NewDataStoreReply()
        let store: WKWebsiteDataStore
        if c.ephemeral {
            store = .nonPersistent()
        } else if let id = UUID(uuidString: c.id) {
            store = WKWebsiteDataStore(forIdentifier: id)
        } else {
            r.error = "mygo: sessions: the store's ID \(c.id) is not a UUID"
            return r
        }
        next += 1
        stores[next] = store
        r.store = next
        return r
    }

    /// Lets the store go: the web views using it keep it while they live.
    static func close(_ c: CloseDataStoreCall) { stores[c.store] = nil }

    /// The store of a call, or nil once it answered that there is none.
    private static func target(_ n: Int, _ req: Int) -> WKWebsiteDataStore? {
        guard let s = store(n) else {
            done(req, error: "mygo: sessions: the store is closed")
            return nil
        }
        return s
    }

    private static func done(_ req: Int, cookies: [String] = [], error: String = "") {
        Go.send(Event.siteDataDone(req: req, cookies: cookies, error: error, unsupported: false))
    }

    static func listCookies(_ c: ListCookiesCall) {
        guard let ds = target(c.store, c.req) else { return }
        // WKHTTPCookieStore cannot match a URL: Go does.
        ds.httpCookieStore.getAllCookies { all in DataStores.done(c.req, cookies: all.map { DataStores.line($0) }) }
    }

    private static let httpDate: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = TimeZone(identifier: "GMT")
        f.dateFormat = "EEE, dd MMM yyyy HH:mm:ss 'GMT'"
        return f
    }()

    /// The cookie as a line of a Set-Cookie header's form, which Go reads.
    private static func line(_ c: HTTPCookie, value: Bool = true) -> String {
        var s = "\(c.name)=\(value ? c.value : ""); domain=\(c.domain.lowercased()); path=\(c.path)"
        if !c.isSessionOnly, let d = c.expiresDate { s += "; expires=" + httpDate.string(from: d) }
        if c.isSecure { s += "; secure" }
        if c.isHTTPOnly { s += "; httponly" }
        return s + "; samesite=" + sameSite(c)
    }

    /// Foundation names only Lax and Strict, and gives a cookie set with
    /// SameSite=None no policy, which WebKit sends as None.
    private static func sameSite(_ c: HTTPCookie) -> String {
        let p = c.sameSitePolicy?.rawValue.lowercased() ?? ""
        return p.isEmpty ? "none" : p
    }

    static func setCookie(_ c: SetCookieCall) {
        guard let ds = target(c.store, c.req) else { return }
        // Foundation parses the attributes, with a stand-in name and value: it
        // would split a value with ", " in it as two cookies.
        let parsed = URL(string: c.url).map { HTTPCookie.cookies(withResponseHeaderFields: ["Set-Cookie": "n=v; " + c.attributes], for: $0) } ?? []
        guard parsed.count == 1, var props = parsed[0].properties else {
            done(c.req, error: "mygo: cookies: Set could not construct NSHTTPCookie")
            return
        }
        props[.name] = c.name
        props[.value] = c.value
        guard let cookie = HTTPCookie(properties: props) else {
            done(c.req, error: "mygo: cookies: Set could not construct NSHTTPCookie")
            return
        }
        if cookie.name != c.name || cookie.value != c.value || cookie.domain.lowercased() != c.domain || cookie.path != c.path ||
            cookie.isSecure != c.secure || cookie.isHTTPOnly != c.httpOnly || sameSite(cookie) != c.sameSite {
            // Say what differs, without the value.
            done(c.req, error: "mygo: cookies: Set made NSHTTPCookie \(line(cookie, value: false)) of \(c.name)=; \(c.attributes)")
            return
        }
        ds.httpCookieStore.setCookie(cookie) { DataStores.done(c.req) }
    }

    static func deleteCookie(_ c: DeleteCookieCall) {
        guard let ds = target(c.store, c.req) else { return }
        let cookies = ds.httpCookieStore
        cookies.getAllCookies { all in
            let matches = all.filter { $0.name == c.name && $0.domain.lowercased() == c.domain && $0.path == c.path }
            guard matches.count <= 1 else {
                DataStores.done(c.req, error: "mygo: cookies: ambiguous cookie key")
                return
            }
            guard let match = matches.first else {
                DataStores.done(c.req) // none: nothing to delete
                return
            }
            cookies.delete(match) { DataStores.done(c.req) }
        }
    }

    static func clear(_ c: ClearSiteDataCall) {
        guard let ds = target(c.store, c.req) else { return }
        ds.removeData(ofTypes: types(c.types), modifiedSince: .distantPast) { DataStores.done(c.req) }
    }

    /// The WKWebsiteDataTypes of platform.SiteData's bits: cookies, local
    /// and session storage, IndexedDB, caches, service workers; with
    /// SiteDataAll's, all of them, those and the others WebKit has.
    private static func types(_ bits: Int) -> Set<String> {
        let all = 0x1f | 1 << 31
        if bits & all == all { return WKWebsiteDataStore.allWebsiteDataTypes() }
        var t = Set<String>()
        if bits & 1 != 0 { t.insert(WKWebsiteDataTypeCookies) }
        if bits & 2 != 0 { t.formUnion([WKWebsiteDataTypeLocalStorage, WKWebsiteDataTypeSessionStorage]) }
        if bits & 4 != 0 { t.formUnion([WKWebsiteDataTypeIndexedDBDatabases, WKWebsiteDataTypeWebSQLDatabases]) }
        if bits & 8 != 0 {
            t.formUnion([WKWebsiteDataTypeDiskCache, WKWebsiteDataTypeMemoryCache, WKWebsiteDataTypeOfflineWebApplicationCache,
                         WKWebsiteDataTypeFetchCache])
        }
        if bits & 16 != 0 { t.insert(WKWebsiteDataTypeServiceWorkerRegistrations) }
        return t
    }
}

extension Host {
    func newDataStore(_ c: NewDataStoreCall) -> NewDataStoreReply { DataStores.make(c) }
    func closeDataStore(_ c: CloseDataStoreCall) { DataStores.close(c) }
    func listCookies(_ c: ListCookiesCall) { DataStores.listCookies(c) }
    func setCookie(_ c: SetCookieCall) { DataStores.setCookie(c) }
    func deleteCookie(_ c: DeleteCookieCall) { DataStores.deleteCookie(c) }
    func clearSiteData(_ c: ClearSiteDataCall) { DataStores.clear(c) }
}
