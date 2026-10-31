// The navigation of phones on Android: each window in a page of a stack,
// whose pages above the first have a toolbar with a back arrow and the
// window's title, and bars of tabs (Material's bottom navigation), a
// stack in each tab, all in the root view, the one shown last in front.

package dev.mygo

import android.content.Context
import android.graphics.BitmapFactory
import android.graphics.drawable.BitmapDrawable
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.LinearLayout
import androidx.appcompat.view.ContextThemeWrapper
import com.google.android.material.bottomnavigation.BottomNavigationView
import com.google.android.material.appbar.MaterialToolbar
import com.google.android.material.color.DynamicColors

object Navigation {
    /** A window's page in a stack: its toolbar, when it has one, over it. */
    class Page(val window: HostWindow, val toolbar: MaterialToolbar?, val view: LinearLayout)

    /** A stack of pages: the first is the window it shows, the others pushed over it. */
    class Stack(context: Context) : FrameLayout(context) {
        val pages = ArrayList<Page>()
        var tabs: Tabs? = null
        var barAtRoot = ""
    }

    /** A bar of tabs, a stack in each. */
    class Tabs(context: Context, val id: Long) : LinearLayout(context) {
        val content = FrameLayout(context)
        val stacks = ArrayList<Stack>()
        val bar = BottomNavigationView(material(context))
        var selected = 0

        init {
            orientation = VERTICAL
            addView(content, LayoutParams(LayoutParams.MATCH_PARENT, 0, 1f))
            addView(bar, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
            bar.labelVisibilityMode = BottomNavigationView.LABEL_VISIBILITY_LABELED
            bar.setOnItemSelectedListener { item ->
                val i = item.itemId
                if (i != selected) {
                    select(i)
                    Go.send(Event.tabSelected(id, i.toLong()))
                }
                true
            }
        }

        fun select(i: Int) {
            if (i !in stacks.indices) return
            selected = i
            for ((k, s) in stacks.withIndex()) s.visibility = if (k == i) View.VISIBLE else View.GONE
            if (bar.selectedItemId != i) bar.selectedItemId = i
            for (p in stacks[i].pages) p.window.reportSize()
        }
    }

    private val stacks = HashMap<Long, Stack>() // by window
    val tabs = HashMap<Long, Tabs>()
    private val root get() = HostImpl.root

    /** A context with Material 3's theme, and the wallpaper's colors where the system has them. */
    fun material(context: Context): Context =
        DynamicColors.wrapContextIfAvailable(ContextThemeWrapper(context, com.google.android.material.R.style.Theme_Material3_DayNight_NoActionBar))

    private fun page(w: HostWindow, title: String?, back: Boolean): Page {
        (w.view.parent as? ViewGroup)?.removeView(w.view)
        val v = LinearLayout(w.context).apply { orientation = LinearLayout.VERTICAL }
        var toolbar: MaterialToolbar? = null
        if (title != null) {
            val t = MaterialToolbar(material(w.context))
            t.title = title
            if (back) {
                t.setNavigationIcon(androidx.appcompat.R.drawable.abc_ic_ab_back_material)
                t.setNavigationContentDescription(androidx.appcompat.R.string.abc_action_bar_up_description)
                t.setNavigationOnClickListener { pop(w, true) }
            }
            t.setBackgroundColor(HostImpl.themeColor(android.R.attr.colorBackground))
            v.addView(t, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT))
            toolbar = t
        }
        v.addView(w.view, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f))
        w.view.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> w.reportSize() }
        return Page(w, toolbar, v)
    }

    private fun front(v: View) {
        (v.parent as? ViewGroup)?.let { if (it !== root) it.removeView(v) }
        if (v.parent == null) {
            root.addView(v, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
        } else {
            v.bringToFront()
        }
        insetsChanged()
    }

    /** show brings a window to the front: its tab, its stack, or a stack of its own. */
    fun show(w: HostWindow) {
        val s = stacks[w.id]
        if (s != null) {
            val t = s.tabs
            if (t != null) {
                t.select(t.stacks.indexOf(s))
                front(t)
            } else {
                front(s)
            }
            return
        }
        val stack = Stack(w.context)
        val p = page(w, null, false)
        stack.pages.add(p)
        stack.addView(p.view, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
        stacks[w.id] = stack
        front(stack)
    }

    /** hide takes a window off the screen: its stack, or the pages above it in its stack. */
    fun hide(w: HostWindow) {
        val s = stacks[w.id] ?: return
        val i = s.pages.indexOfFirst { it.window === w }
        if (i == 0) {
            if (s.tabs == null) {
                root.removeView(s)
                for (p in s.pages) stacks.remove(p.window.id)
            }
        } else if (i > 0) {
            truncate(s, i, false)
        }
    }

    fun forget(w: HostWindow) {
        val s = stacks.remove(w.id) ?: return
        s.pages.removeAll { it.window === w }
    }

    fun push(w: HostWindow, parent: HostWindow, title: String, animated: Boolean) {
        val s = stacks[parent.id] ?: run {
            show(parent)
            stacks[parent.id]!!
        }
        val at = s.pages.indexOfFirst { it.window === parent }
        if (at >= 0 && at < s.pages.size - 1) truncate(s, at + 1, false)
        val below = s.pages.lastOrNull()
        val p = page(w, title, true)
        s.pages.add(p)
        stacks[w.id] = s
        s.addView(p.view, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
        insetsChanged()
        if (animated && s.width > 0) {
            p.view.translationX = s.width.toFloat()
            p.view.animate().translationX(0f).setDuration(250).withEndAction { below?.view?.visibility = View.INVISIBLE }.start()
            below?.view?.animate()?.translationX(-s.width * 0.25f)?.setDuration(250)?.start()
        } else {
            below?.view?.visibility = View.INVISIBLE
        }
    }

    fun pop(w: HostWindow, animated: Boolean) {
        val s = stacks[w.id] ?: return
        val i = s.pages.indexOfFirst { it.window === w }
        if (i > 0) truncate(s, i, animated)
    }

    /** truncate takes the pages from i up off the stack, which tells Go they went. */
    private fun truncate(s: Stack, i: Int, animated: Boolean) {
        val gone = s.pages.subList(i, s.pages.size).toList()
        s.pages.subList(i, s.pages.size).clear()
        val below = s.pages.lastOrNull()
        below?.view?.visibility = View.VISIBLE
        below?.view?.translationX = 0f
        for ((k, p) in gone.withIndex()) {
            stacks.remove(p.window.id)
            val last = k == gone.size - 1
            if (animated && last && s.width > 0) {
                below?.view?.translationX = -s.width * 0.25f
                below?.view?.animate()?.translationX(0f)?.setDuration(220)?.start()
                p.view.animate().translationX(s.width.toFloat()).setDuration(220).withEndAction { s.removeView(p.view) }.start()
            } else {
                s.removeView(p.view)
            }
            Go.send(Event.popped(p.window.id))
        }
    }

    /** back pops the window in front off its stack, and reports whether there was one to pop. */
    fun back(): Boolean {
        for (k in root.childCount - 1 downTo 0) {
            val v = root.getChildAt(k)
            val s = when (v) {
                is Tabs -> v.stacks.getOrNull(v.selected)
                is Stack -> v
                else -> null
            } ?: continue
            if (s.pages.size > 1) {
                pop(s.pages.last().window, true)
                return true
            }
            return false
        }
        return false
    }

    // Tabs.

    fun createTabs(id: Long, context: Context) {
        tabs[id] = Tabs(context, id)
    }

    fun addTab(id: Long, w: HostWindow, title: String, icon: ByteArray) {
        val t = tabs[id] ?: return
        stacks[w.id]?.let { old ->
            if (old.tabs == null) root.removeView(old)
            stacks.remove(w.id)
        }
        val stack = Stack(w.context)
        stack.tabs = t
        val p = page(w, title.ifEmpty { null }, false)
        stack.pages.add(p)
        stack.addView(p.view, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
        stacks[w.id] = stack
        val i = t.stacks.size
        t.stacks.add(stack)
        t.content.addView(stack, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
        val item = t.bar.menu.add(0, i, i, title)
        if (icon.isNotEmpty()) {
            BitmapFactory.decodeByteArray(icon, 0, icon.size)?.let { item.icon = BitmapDrawable(w.context.resources, it) }
        }
    }

    fun showTabs(id: Long) {
        val t = tabs[id] ?: return
        t.select(0)
        front(t)
    }

    fun setBadge(id: Long, i: Int, badge: String) {
        val t = tabs[id] ?: return
        if (badge.isEmpty()) {
            t.bar.removeBadge(i)
            return
        }
        val b = t.bar.getOrCreateBadge(i)
        badge.toIntOrNull()?.let { b.number = it } ?: run { b.text = badge }
    }

    fun closeTabs(id: Long) {
        val t = tabs.remove(id) ?: return
        root.removeView(t)
    }

    fun setTitle(w: HostWindow, title: String) {
        stacks[w.id]?.pages?.firstOrNull { it.window === w }?.toolbar?.title = title
    }

    /** The toolbars at the top of the screen take the status bar's room. */
    fun insetsChanged() {
        val top = HostImpl.bars.top
        for (s in stacks.values.toSet()) for (p in s.pages) p.toolbar?.setPadding(0, top, 0, 0)
        for (w in HostImpl.windows.values) w.insetsChanged()
    }
}
