package dev.mygo

import android.webkit.CookieManager
import androidx.webkit.CookieManagerCompat
import androidx.webkit.WebViewFeature

/**
 * The website data of pages: CookieManager's cookies, for package mygo's
 * Cookies. The process's web views share one store, the default session's,
 * so Go makes no other (NewDataStore) and asks for store 0 alone. Go builds
 * the Set-Cookie lines, and reads the ones listed.
 */
object SiteData {
    private fun done(req: Long, cookies: List<String> = emptyList(), error: String = "", unsupported: Boolean = false) {
        Go.send(Event.siteDataDone(req, cookies, error, unsupported))
    }

    /** Whether the call is of the default store, which is all there is; else it answers so. */
    private fun defaultStore(req: Long, store: Long): Boolean {
        if (store == 0L) return true
        done(req, error = ONE_STORE, unsupported = true)
        return false
    }

    private const val ONE_STORE = "mygo: sessions: Android's web view keeps one store for the app, the default session's"

    fun newDataStore(c: NewDataStoreCall) = NewDataStoreReply(error = ONE_STORE)

    /** The cookies a request to the URL sends, with their attributes, which CookieManager.getCookie leaves out. */
    fun list(c: ListCookiesCall) {
        if (!defaultStore(c.req, c.store)) return
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.GET_COOKIE_INFO)) {
            done(c.req, error = "mygo: cookies: this Android System WebView lists no cookies with their attributes; a newer one does", unsupported = true)
            return
        }
        try {
            done(c.req, CookieManagerCompat.getCookieInfo(CookieManager.getInstance(), c.url))
        } catch (e: Exception) {
            done(c.req, error = "mygo: cookies: $e")
        }
    }

    fun set(c: SetCookieCall) {
        if (!defaultStore(c.req, c.store)) return
        val cm = CookieManager.getInstance()
        cm.setCookie(c.url, c.name + "=" + c.value + "; " + c.attributes) { ok ->
            cm.flush()
            done(c.req, error = if (ok == true) "" else "mygo: cookies: the web view refused the cookie")
        }
    }

    /**
     * CookieManager deletes no cookie by itself: one of the same name,
     * domain and path that expired (Attributes) takes its place and goes.
     * Deleting a cookie that does not exist is no error.
     */
    fun delete(c: DeleteCookieCall) {
        if (!defaultStore(c.req, c.store)) return
        val cm = CookieManager.getInstance()
        cm.setCookie(c.url, c.name + "=; " + c.attributes) { _ ->
            cm.flush()
            done(c.req)
        }
    }

    /** Clears cookies (SiteDataCookies, 1): the web view has no call that clears the other kinds of a store. */
    fun clear(c: ClearSiteDataCall) {
        if (!defaultStore(c.req, c.store)) return
        if (c.types != 1L) {
            done(c.req, error = "mygo: Android's web view clears only cookies (SiteDataCookies)", unsupported = true)
            return
        }
        val cm = CookieManager.getInstance()
        cm.removeAllCookies { _ ->
            cm.flush()
            done(c.req)
        }
    }
}
