package dev.mygo

import android.content.Context
import android.graphics.Rect
import android.os.Bundle
import android.text.InputType
import android.view.Choreographer
import android.view.InputDevice
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.view.View
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityNodeProvider
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputMethodManager

/**
 * A window showing native UI, which Go draws with OpenGL ES into the
 * surface's ANativeWindow. The view gives Go the display's refreshes
 * (Choreographer), the fingers, a hardware keyboard, the soft keyboard
 * (an InputConnection, with the composing text of input methods) and
 * TalkBack's questions.
 */
class SurfaceWindow(context: Context, id: Long) : HostWindow(context, id) {
    /** The window's number, which views' own id would shadow. */
    private val wid: Long = id
    private val surface = Surface(context)
    override val view: View get() = surface
    private var native = 0L
    private var framePending = false
    private val frame = Choreographer.FrameCallback {
        framePending = false
        Go.send(Event.frame(wid))
    }

    // The text being edited, as Go last told it (SetTextInput).
    private var text = ""
    private var selStart = 0
    private var selEnd = 0
    private var editing = false
    private var secure = false
    private var composing = ""

    private var nodes: List<Node>? = null

    override fun setBackground(argb: Int) = surface.setBackgroundColor(argb)

    fun requestFrame() {
        if (framePending) return
        framePending = true
        Choreographer.getInstance().postFrameCallback(frame)
    }

    override fun close() {
        if (framePending) Choreographer.getInstance().removeFrameCallback(frame)
        releaseNative()
    }

    private fun releaseNative() {
        if (native != 0L) {
            Go.ask(Event.surfaceDestroyed(wid))
            MyGoNative.releaseNativeWindow(native)
            native = 0
        }
    }

    private val density get() = HostImpl.density.toDouble()

    fun setTextInput(c: SetTextInputCall) {
        val was = editing
        editing = c.active
        secure = c.secure
        val changed = c.text != text || c.selStart.toInt() != selStart || c.selEnd.toInt() != selEnd
        text = c.text
        selStart = c.selStart.toInt().coerceIn(0, text.codePointCount(0, text.length))
        selEnd = c.selEnd.toInt().coerceIn(0, text.codePointCount(0, text.length))
        val imm = context.getSystemService(Context.INPUT_METHOD_SERVICE) as InputMethodManager
        if (editing && !was) {
            surface.requestFocus()
            imm.restartInput(surface)
            imm.showSoftInput(surface, 0)
        } else if (!editing && was) {
            composing = ""
            imm.hideSoftInputFromWindow(surface.windowToken, 0)
            imm.restartInput(surface)
        } else if (changed && editing && composing.isEmpty()) {
            val a = utf16(selStart)
            val b = utf16(selEnd)
            imm.updateSelection(surface, a, b, -1, -1)
        }
    }

    /** The UTF-16 offset of rune i of the text. */
    private fun utf16(i: Int): Int = text.offsetByCodePoints(0, i.coerceIn(0, text.codePointCount(0, text.length)))

    fun textInputChanged() {}
    fun setCursor(cursor: String) {}
    fun showMenu(c: ShowMenuCall) {}

    fun accessibilityChanged(focus: Long) {
        nodes = null
        surface.sendAccessibilityEvent(AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED)
    }

    private fun tree(): List<Node> {
        nodes?.let { return it }
        val r = Go.ask(Event.accessibilityTree(wid)) ?: return emptyList()
        val n = Node.decode(AccessibilityTreeReply(r).nodes)
        nodes = n
        return n
    }

    private inner class Surface(context: Context) : SurfaceView(context), SurfaceHolder.Callback {
        init {
            holder.addCallback(this)
            isFocusable = true
            isFocusableInTouchMode = true
        }

        override fun surfaceCreated(h: SurfaceHolder) {
            native = MyGoNative.nativeWindow(h.surface)
            val hz = display?.refreshRate?.toDouble() ?: 60.0
            Go.send(Event.surfaceChanged(wid, native, width / density, height / density, density, hz))
        }

        override fun surfaceChanged(h: SurfaceHolder, format: Int, w: Int, ht: Int) {
            reportSize()
            requestFrame()
        }

        override fun surfaceDestroyed(h: SurfaceHolder) = releaseNative()

        override fun onTouchEvent(e: MotionEvent): Boolean {
            val d = density
            fun send(i: Int, phase: String) {
                val kind = when (e.getToolType(i)) {
                    MotionEvent.TOOL_TYPE_MOUSE -> "mouse"
                    MotionEvent.TOOL_TYPE_STYLUS -> "pen"
                    else -> "touch"
                }
                Go.send(Event.touch(wid, e.getPointerId(i).toLong(), phase, kind, e.getX(i) / d, e.getY(i) / d,
                    e.getPressure(i).toDouble(), e.buttonState.toLong().coerceAtLeast(1), e.eventTime / 1000.0))
            }
            when (e.actionMasked) {
                MotionEvent.ACTION_DOWN, MotionEvent.ACTION_POINTER_DOWN -> send(e.actionIndex, "down")
                MotionEvent.ACTION_MOVE -> for (i in 0 until e.pointerCount) send(i, "move")
                MotionEvent.ACTION_UP, MotionEvent.ACTION_POINTER_UP -> send(e.actionIndex, "up")
                MotionEvent.ACTION_CANCEL -> for (i in 0 until e.pointerCount) send(i, "cancel")
            }
            return true
        }

        override fun onGenericMotionEvent(e: MotionEvent): Boolean {
            if (e.isFromSource(InputDevice.SOURCE_CLASS_POINTER) && e.actionMasked == MotionEvent.ACTION_SCROLL) {
                val d = density
                val line = 48.0
                Go.send(Event.scroll(wid, e.x / d, e.y / d, -e.getAxisValue(MotionEvent.AXIS_HSCROLL) * line,
                    -e.getAxisValue(MotionEvent.AXIS_VSCROLL) * line, false, ""))
                return true
            }
            return super.onGenericMotionEvent(e)
        }

        private fun key(code: Int, e: KeyEvent, down: Boolean): Boolean {
            val name = keyNames[code] ?: e.unicodeChar.takeIf { it > 0 }?.let { String(Character.toChars(it)) } ?: return false
            var mods = 0L
            if (e.isShiftPressed) mods = mods or 1
            if (e.isCtrlPressed) mods = mods or 2
            if (e.isAltPressed) mods = mods or 4
            if (e.isMetaPressed) mods = mods or 8
            val text = if (e.unicodeChar > 0) String(Character.toChars(e.unicodeChar)) else ""
            val r = Go.ask(Event.key(wid, down, name, text, mods, e.repeatCount > 0)) ?: return false
            val handled = KeyReply(r).handled
            // Keys typing text type it, which the content takes as text.
            if (!handled && down && editing && text.isNotEmpty() && keyNames[code] == null) {
                Go.send(Event.textInsert(wid, text))
                return true
            }
            return handled
        }

        override fun onKeyDown(code: Int, e: KeyEvent) = key(code, e, true) || super.onKeyDown(code, e)
        override fun onKeyUp(code: Int, e: KeyEvent) = key(code, e, false) || super.onKeyUp(code, e)

        override fun onCheckIsTextEditor() = editing

        override fun onCreateInputConnection(info: EditorInfo): InputConnection? {
            if (!editing) return null
            info.inputType = if (secure) InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD
            else InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_AUTO_CORRECT
            info.imeOptions = EditorInfo.IME_FLAG_NO_FULLSCREEN or EditorInfo.IME_ACTION_DONE
            info.initialSelStart = utf16(selStart)
            info.initialSelEnd = utf16(selEnd)
            return Connection(this)
        }

        override fun getAccessibilityNodeProvider(): AccessibilityNodeProvider = Provider()
    }

    /** The soft keyboard's edits, to Go. */
    private inner class Connection(v: View) : BaseInputConnection(v, false) {
        override fun commitText(t: CharSequence, newCursorPosition: Int): Boolean {
            composing = ""
            Go.send(Event.textInsert(wid, t.toString()))
            return true
        }

        override fun setComposingText(t: CharSequence, newCursorPosition: Int): Boolean {
            composing = t.toString()
            val n = composing.codePointCount(0, composing.length).toLong()
            Go.send(Event.textMarked(wid, composing, n, n))
            return true
        }

        override fun finishComposingText(): Boolean {
            if (composing.isNotEmpty()) {
                val c = composing
                composing = ""
                Go.send(Event.textInsert(wid, c))
            }
            return true
        }

        override fun deleteSurroundingText(before: Int, after: Int): Boolean {
            Go.send(Event.textDelete(wid, before.toLong(), after.toLong()))
            return true
        }

        override fun sendKeyEvent(e: KeyEvent): Boolean {
            if (e.action == KeyEvent.ACTION_DOWN) when (e.keyCode) {
                KeyEvent.KEYCODE_DEL -> Go.send(Event.textDelete(wid, 1, 0))
                KeyEvent.KEYCODE_FORWARD_DEL -> Go.send(Event.textDelete(wid, 0, 1))
                KeyEvent.KEYCODE_ENTER -> Go.send(Event.textCommand(wid, "newline"))
                else -> if (e.unicodeChar > 0) Go.send(Event.textInsert(wid, String(Character.toChars(e.unicodeChar))))
            }
            return true
        }

        override fun performEditorAction(action: Int): Boolean {
            Go.send(Event.textCommand(wid, "newline"))
            return true
        }

        override fun performContextMenuAction(action: Int): Boolean {
            when (action) {
                android.R.id.selectAll -> Go.send(Event.textCommand(wid, "selectAll"))
                android.R.id.cut -> Go.send(Event.textCommand(wid, "cut"))
                android.R.id.copy -> Go.send(Event.textCommand(wid, "copy"))
                android.R.id.paste -> Go.send(Event.textCommand(wid, "paste"))
            }
            return true
        }

        override fun getTextBeforeCursor(n: Int, flags: Int): CharSequence {
            val a = utf16(minOf(selStart, selEnd))
            return text.substring(maxOf(0, a - n), a)
        }

        override fun getTextAfterCursor(n: Int, flags: Int): CharSequence {
            val b = utf16(maxOf(selStart, selEnd))
            return text.substring(b, minOf(text.length, b + n))
        }

        override fun getSelectedText(flags: Int): CharSequence? {
            val a = utf16(minOf(selStart, selEnd))
            val b = utf16(maxOf(selStart, selEnd))
            return if (a == b) null else text.substring(a, b)
        }

        override fun getCursorCapsMode(reqModes: Int) = 0
    }

    /** TalkBack's view of the elements of native UI, as virtual views. */
    private inner class Provider : AccessibilityNodeProvider() {
        override fun createAccessibilityNodeInfo(virtualId: Int): AccessibilityNodeInfo? {
            val nodes = tree()
            if (virtualId == View.NO_ID) {
                val info = AccessibilityNodeInfo.obtain(surface)
                surface.onInitializeAccessibilityNodeInfo(info)
                for (n in nodes) info.addChild(surface, n.id.toInt())
                return info
            }
            val n = nodes.firstOrNull { it.id.toInt() == virtualId } ?: return null
            val info = AccessibilityNodeInfo.obtain(surface, virtualId)
            info.setParent(surface)
            info.packageName = context.packageName
            info.className = n.className
            info.text = n.label.ifEmpty { null }
            if (n.value.isNotEmpty()) info.stateDescription = n.value
            if (n.hint.isNotEmpty()) info.tooltipText = n.hint
            val d = density
            val box = Rect((n.x * d).toInt(), (n.y * d).toInt(), ((n.x + n.w) * d).toInt(), ((n.y + n.h) * d).toInt())
            val loc = IntArray(2)
            surface.getLocationOnScreen(loc)
            box.offset(loc[0], loc[1])
            info.setBoundsInScreen(box)
            info.isEnabled = n.states and 2 == 0L
            info.isChecked = n.states and 4 != 0L
            info.isCheckable = n.role in longArrayOf(4, 5, 6, 31)
            info.isSelected = n.states and 16 != 0L
            info.isHeading = n.role == 43L
            info.isVisibleToUser = true
            info.isFocusable = true
            if (n.actions and 1 != 0L) {
                info.isClickable = true
                info.addAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_CLICK)
            }
            if (n.actions and 4 != 0L) info.addAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_SCROLL_FORWARD)
            if (n.actions and 8 != 0L) info.addAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_SCROLL_BACKWARD)
            return info
        }

        override fun performAction(virtualId: Int, action: Int, args: Bundle?): Boolean {
            val name = when (action) {
                AccessibilityNodeInfo.ACTION_CLICK -> "press"
                AccessibilityNodeInfo.ACTION_SCROLL_FORWARD -> "increment"
                AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD -> "decrement"
                AccessibilityNodeInfo.ACTION_ACCESSIBILITY_FOCUS -> return true
                else -> return false
            }
            val r = Go.ask(Event.accessibilityAction(wid, virtualId.toLong(), name)) ?: return false
            return AccessibilityActionReply(r).handled
        }
    }

    companion object {
        private val keyNames = mapOf(
            KeyEvent.KEYCODE_ENTER to "Enter", KeyEvent.KEYCODE_ESCAPE to "Escape", KeyEvent.KEYCODE_DEL to "Backspace",
            KeyEvent.KEYCODE_TAB to "Tab", KeyEvent.KEYCODE_SPACE to "Space", KeyEvent.KEYCODE_FORWARD_DEL to "Delete",
            KeyEvent.KEYCODE_MOVE_HOME to "Home", KeyEvent.KEYCODE_MOVE_END to "End", KeyEvent.KEYCODE_PAGE_UP to "PageUp",
            KeyEvent.KEYCODE_PAGE_DOWN to "PageDown", KeyEvent.KEYCODE_DPAD_LEFT to "ArrowLeft",
            KeyEvent.KEYCODE_DPAD_RIGHT to "ArrowRight", KeyEvent.KEYCODE_DPAD_UP to "ArrowUp", KeyEvent.KEYCODE_DPAD_DOWN to "ArrowDown",
        )
    }
}

/** An element of native UI, as Go encodes it (AccessibilityTree). */
class Node(
    val id: Long, val parent: Long, val role: Long, val label: String, val value: String, val hint: String,
    val x: Double, val y: Double, val w: Double, val h: Double, val states: Long, val actions: Long,
) {
    val className: String
        get() = when (role) {
            2L, 31L, 40L, 18L -> "android.widget.Button"
            4L -> "android.widget.CheckBox"
            5L -> "android.widget.RadioButton"
            6L -> "android.widget.Switch"
            7L -> "android.widget.SeekBar"
            8L -> "android.widget.ProgressBar"
            9L -> "android.widget.EditText"
            10L -> "android.widget.ImageView"
            else -> "android.view.View"
        }

    companion object {
        fun decode(b: ByteArray): List<Node> {
            val r = WireReader(b)
            val out = ArrayList<Node>()
            while (true) {
                val n = Node(r.int(), r.int(), r.int(), r.string(), r.string(), r.string(),
                    r.float(), r.float(), r.float(), r.float(), r.int(), r.int())
                r.int() // the level
                if (n.id == 0L) break
                // Groups without a name hold others, which TalkBack reads.
                if (n.role == 0L && n.label.isEmpty() && n.value.isEmpty()) continue
                out.add(n)
            }
            return out
        }
    }
}
