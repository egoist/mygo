package dev.mygo

import android.content.Context
import android.view.View

/**
 * A window of the app: a view filling the screen, which shows a page or
 * native UI.
 */
abstract class HostWindow(val context: Context, val id: Long) {
    abstract val view: View
    private var last: DoubleArray? = null

    /** Tells Go the window's size and what covers it, when they changed. */
    fun reportSize() {
        val root = HostImpl.root
        if (root.width == 0) return
        val d = HostImpl.density.toDouble()
        val bars = HostImpl.bars
        val ime = HostImpl.ime
        val size = doubleArrayOf(
            root.width / d, root.height / d, d,
            bars.top / d, bars.left / d, bars.bottom / d, bars.right / d,
            maxOf(0, ime.bottom - bars.bottom) / d,
        )
        if (last?.contentEquals(size) == true) return
        last = size
        Go.send(Event.resized(id, size[0], size[1], size[2], size[3], size[4], size[5], size[6], size[7]))
    }

    /** The insets changed: lays the window out again and tells Go. */
    open fun insetsChanged() = reportSize()

    abstract fun setBackground(argb: Int)
    open fun close() {}
}
