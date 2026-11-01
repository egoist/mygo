// The way between Go and the host. Go runs on a thread of its own and
// makes its calls (HostCalls) on the UI thread, through Host.call; events
// go to Go through MyGoNative.event.

package dev.mygo

import android.os.Looper
import android.view.Surface

/** The Go library's native methods (jni_android.c in internal/mobile). */
object MyGoNative {
    init {
        System.loadLibrary("mygoapp")
    }

    /** Starts Go's main function on a thread of its own. UI thread, once. */
    @JvmStatic external fun start()

    /** Sends an encoded event; for an event Go answers, waits for the answer. */
    @JvmStatic external fun event(msg: ByteArray, onUI: Boolean): ByteArray?

    /** Runs the calls Go made. UI thread. */
    @JvmStatic external fun drain()

    /** The ANativeWindow of a surface, which Go draws into; 0 for none. */
    @JvmStatic external fun nativeWindow(surface: Surface): Long

    @JvmStatic external fun releaseNativeWindow(window: Long)
}

/** What JNI calls of the host: Go's calls, on the UI thread. */
object Host {
    @JvmStatic
    fun call(msg: ByteArray): ByteArray? = dispatchCall(HostImpl, msg)
}

object Go {
    private fun onUI() = Looper.myLooper() == Looper.getMainLooper()

    /** Sends an event Go does not answer. */
    fun send(msg: ByteArray) {
        MyGoNative.event(msg, onUI())
    }

    /** Sends an event and returns Go's answer, null when Go gives none. */
    fun ask(msg: ByteArray): WireReader? = MyGoNative.event(msg, onUI())?.let { WireReader(it) }
}
