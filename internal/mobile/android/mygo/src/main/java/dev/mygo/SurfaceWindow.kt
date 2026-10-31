package dev.mygo

import android.content.Context
import android.view.SurfaceView
import android.view.View

/** A window showing native UI, which Go draws with OpenGL ES. */
class SurfaceWindow(context: Context, id: Long) : HostWindow(context, id) {
    private val surface = SurfaceView(context)
    override val view: View get() = surface

    override fun setBackground(argb: Int) = surface.setBackgroundColor(argb)
    fun requestFrame() {}
    fun setTextInput(c: SetTextInputCall) {}
    fun textInputChanged() {}
    fun setCursor(cursor: String) {}
    fun accessibilityChanged(focus: Long) {}
    fun showMenu(c: ShowMenuCall) {}
}
