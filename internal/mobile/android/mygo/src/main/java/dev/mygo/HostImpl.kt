// The calls Go makes of the host (HostCalls), on the UI thread.

package dev.mygo

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.UiModeManager
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.MutableContextWrapper
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.provider.OpenableColumns
import android.provider.Settings
import android.system.Os
import android.text.format.DateFormat
import android.util.Log
import android.view.HapticFeedbackConstants
import android.view.View
import android.view.WindowManager
import android.webkit.MimeTypeMap
import android.widget.FrameLayout
import androidx.core.content.FileProvider
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import java.io.File
import java.util.Calendar
import java.util.Locale

object HostImpl : HostCalls {
    /** The Activity showing the app, null while none does. */
    var activity: Activity? = null
        private set
    lateinit var app: Context
        private set

    /** The context of the views, which follows the Activity. */
    private lateinit var context: MutableContextWrapper
    lateinit var root: FrameLayout
        private set
    val windows = HashMap<Long, HostWindow>()
    private var started = false

    // What covers the screen's edges, in pixels: the system's bars and
    // the cutout, and the keyboard.
    var bars = androidx.core.graphics.Insets.NONE
        private set
    var ime = androidx.core.graphics.Insets.NONE
        private set

    private const val PICK_FILES = 0x6d79
    private const val ASK_NOTIFICATIONS = 0x6d7a
    private var pickReq = 0L
    private var pendingNotes = ArrayList<NotifyCall>()

    fun attach(a: Activity) {
        activity = a
        if (!::app.isInitialized) {
            app = a.applicationContext
            context = MutableContextWrapper(a)
            root = FrameLayout(context)
            ViewCompat.setOnApplyWindowInsetsListener(root) { _, insets ->
                bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
                ime = insets.getInsets(WindowInsetsCompat.Type.ime())
                Navigation.insetsChanged()
                insets
            }
            root.addOnLayoutChangeListener { _, l, t, r, b, ol, ot, or, ob ->
                if (r - l != or - ol || b - t != ob - ot) for (w in windows.values) w.reportSize()
            }
        } else {
            context.baseContext = a
            (root.parent as? android.view.ViewGroup)?.removeView(root)
        }
        root.setBackgroundColor(themeColor(android.R.attr.colorBackground))
        a.setContentView(root)
        ViewCompat.requestApplyInsets(root)
    }

    fun detach(a: Activity) {
        if (activity === a) activity = null
    }

    /** Starts Go once per process, with the environment a launch passes (mygo dev). */
    fun startGo(a: Activity, intent: Intent?) {
        if (started) return
        started = true
        val files = a.filesDir.absolutePath
        val cache = a.cacheDir.absolutePath
        // Android sets none of these, which os.UserHomeDir, UserConfigDir,
        // UserCacheDir and TempDir read.
        Os.setenv("HOME", files, true)
        Os.setenv("XDG_CONFIG_HOME", files, true)
        Os.setenv("XDG_CACHE_HOME", cache, true)
        Os.setenv("TMPDIR", cache, true)
        Os.setenv("MYGO_RESOURCES", Resources.extract(a), true)
        intent?.getStringExtra("mygo.env")?.split('\n')?.forEach { line ->
            val i = line.indexOf('=')
            if (i > 0) Os.setenv(line.substring(0, i), line.substring(i + 1), true)
        }
        MyGoNative.start()
    }

    fun themeColor(attr: Int): Int {
        val a = (activity ?: context).obtainStyledAttributes(intArrayOf(attr))
        val c = a.getColor(0, Color.WHITE)
        a.recycle()
        return c
    }

    val density get() = app.resources.displayMetrics.density

    /** The window in front: the visible window highest on screen. */
    fun front(): HostWindow? = windows.values.filter { it.view.isShown }.maxByOrNull { w ->
        var depth = 0
        var v: View? = w.view
        while (v != null && v !== root) {
            val parent = v.parent as? android.view.ViewGroup ?: break
            depth = depth * 64 + parent.indexOfChild(v)
            v = parent
        }
        depth
    }

    /** Android's back: the window in front's page goes back, or the app takes it. */
    fun back(): Boolean {
        if (Navigation.back()) return true
        val w = front() ?: return false
        val r = Go.ask(Event.backPressed(w.id)) ?: return false
        return BackPressedReply(r).handled
    }

    override fun appInfo(): AppInfoReply {
        val r = AppInfoReply()
        val pm = app.packageManager
        val info = pm.getPackageInfo(app.packageName, 0)
        r.name = app.applicationInfo.loadLabel(pm).toString()
        r.version = info.versionName ?: ""
        r.identifier = app.packageName
        r.filesDir = app.filesDir.absolutePath
        r.cacheDir = app.cacheDir.absolutePath
        r.tempDir = app.cacheDir.absolutePath
        r.documentsDir = (app.getExternalFilesDir(null) ?: app.filesDir).absolutePath
        r.resourcesDir = Resources.dir(app).absolutePath
        val locales = app.resources.configuration.locales
        r.languages = (0 until locales.size()).map { locales[it].toLanguageTag() }
        val format = Locale.getDefault(Locale.Category.FORMAT)
        r.regionLocale = format.toLanguageTag()
        r.firstWeekday = (Calendar.getInstance(format).firstDayOfWeek - 1).toLong()
        r.hourCycle = if (DateFormat.is24HourFormat(app)) 24L else 12L
        r.system = "Android"
        r.systemVersion = Build.VERSION.RELEASE
        r.model = Build.MODEL
        return r
    }

    override fun theme(): ThemeReply {
        val r = ThemeReply()
        val c = (activity ?: app).resources.configuration
        r.dark = c.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES
        val accent = if (Build.VERSION.SDK_INT >= 31) app.getColor(android.R.color.system_accent1_600) else themeColor(android.R.attr.colorAccent)
        r.accent = argbToRGBA(accent)
        r.reduceMotion = Settings.Global.getFloat(app.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
        if (Build.VERSION.SDK_INT >= 34) {
            r.increaseContrast = (app.getSystemService(Context.UI_MODE_SERVICE) as UiModeManager).contrast > 0.5f
        }
        r.textScale = c.fontScale.toDouble()
        return r
    }

    override fun setAppearance(c: SetAppearanceCall) {
        if (Build.VERSION.SDK_INT < 31) return
        val ui = app.getSystemService(Context.UI_MODE_SERVICE) as UiModeManager
        ui.setApplicationNightMode(
            when (c.source) {
                "dark" -> UiModeManager.MODE_NIGHT_YES
                "light" -> UiModeManager.MODE_NIGHT_NO
                else -> UiModeManager.MODE_NIGHT_AUTO
            }
        )
    }

    override fun screen(): ScreenReply {
        val r = ScreenReply()
        val m = app.resources.displayMetrics
        r.width = m.widthPixels / m.density.toDouble()
        r.height = m.heightPixels / m.density.toDouble()
        r.scale = m.density.toDouble()
        return r
    }

    override fun log(c: LogCall) {
        Log.println(if (c.error) Log.ERROR else Log.INFO, "mygo", c.line)
    }

    // Windows.

    override fun createWindow(c: CreateWindowCall): CreateWindowReply {
        val r = CreateWindowReply()
        try {
            val w = if (c.surface) SurfaceWindow(context, c.id) else WebWindow(context, c)
            if (c.hasBackground) w.setBackground(rgbaToARGB(c.background))
            windows[c.id] = w
        } catch (e: Throwable) {
            r.error = "mygo: ${e.message ?: e.toString()}"
        }
        return r
    }

    override fun showWindow(c: ShowWindowCall) {
        val w = windows[c.id] ?: return
        Navigation.show(w)
        w.insetsChanged()
    }

    override fun hideWindow(c: HideWindowCall) {
        val w = windows[c.id] ?: return
        Navigation.hide(w)
    }

    override fun closeWindow(c: CloseWindowCall) {
        val w = windows.remove(c.id) ?: return
        Navigation.hide(w)
        Navigation.forget(w)
        w.close()
    }

    override fun setTitle(c: SetTitleCall) { windows[c.id]?.let { Navigation.setTitle(it, c.title) } }
    override fun createTabs(c: CreateTabsCall) = Navigation.createTabs(c.id, context)
    override fun addTab(c: AddTabCall) { windows[c.window]?.let { Navigation.addTab(c.id, it, c.title, c.icon) } }
    override fun showTabs(c: ShowTabsCall) = Navigation.showTabs(c.id)
    override fun selectTab(c: SelectTabCall) { Navigation.tabs[c.id]?.select(c.index.toInt()) }
    override fun setTabBadge(c: SetTabBadgeCall) = Navigation.setBadge(c.id, c.index.toInt(), c.badge)
    override fun closeTabs(c: CloseTabsCall) = Navigation.closeTabs(c.id)
    override fun pushWindow(c: PushWindowCall) {
        val p = windows[c.parent] ?: return
        val w = windows[c.child] ?: return
        Navigation.push(w, p, c.title, c.animated)
    }
    override fun popWindow(c: PopWindowCall) { windows[c.id]?.let { Navigation.pop(it, c.animated) } }

    override fun setBackground(c: SetBackgroundCall) {
        val w = windows[c.id] ?: return
        val argb = rgbaToARGB(c.color)
        w.fill = argb
        w.setBackground(argb)
        Navigation.backgroundChanged(w)
    }

    override fun focus(c: FocusCall) {
        windows[c.id]?.view?.requestFocus()
    }

    // Pages.

    private fun web(id: Long) = windows[id] as? WebWindow

    override fun loadURL(c: LoadURLCall) { web(c.id)?.load(c.url) }
    override fun loadHTML(c: LoadHTMLCall) { web(c.id)?.loadHTML(c.html, c.baseURL) }
    override fun reload(c: ReloadCall) {
        val w = web(c.id) ?: return
        if (c.ignoreCache) w.web.clearCache(false)
        w.web.reload()
    }
    override fun stopLoading(c: StopLoadingCall) { web(c.id)?.web?.stopLoading() }
    override fun goBack(c: GoBackCall) { web(c.id)?.web?.goBack() }
    override fun goForward(c: GoForwardCall) { web(c.id)?.web?.goForward() }
    override fun eval(c: EvalCall) { web(c.id)?.web?.evaluateJavascript(c.js, null) }
    override fun callAsync(c: CallAsyncCall) {
        val w = web(c.id)
        if (w == null) {
            Go.send(Event.asyncResult(c.req, "", "mygo: the window is closed"))
            return
        }
        w.callAsync(c.req, c.body)
    }
    override fun setZoom(c: SetZoomCall) { web(c.id)?.web?.settings?.textZoom = (c.factor * 100).toInt() }
    override fun setUserAgent(c: SetUserAgentCall) { web(c.id)?.setUserAgent(c.userAgent) }
    override fun setUserScripts(c: SetUserScriptsCall) { web(c.id)?.setScripts(c.scripts, c.scriptFlags) }
    override fun capturePage(c: CapturePageCall) {
        val w = web(c.id)
        if (w == null) {
            Go.send(Event.captured(c.req, ByteArray(0), "mygo: the window is closed"))
            return
        }
        w.capture(c.req)
    }
    override fun userAgent(c: UserAgentCall): UserAgentReply {
        val r = UserAgentReply()
        r.userAgent = web(c.id)?.web?.settings?.userAgentString ?: ""
        return r
    }

    override fun schemeRespond(c: SchemeRespondCall) = Schemes.respond(c)
    override fun schemeData(c: SchemeDataCall) = Schemes.data(c.req, c.data)
    override fun schemeFinish(c: SchemeFinishCall) = Schemes.finish(c.req, null)
    override fun schemeFail(c: SchemeFailCall) = Schemes.finish(c.req, c.message)

    // Dialogs.

    override fun messageBox(c: MessageBoxCall) {
        val a = activity ?: run {
            Go.send(Event.messageBoxResult(c.req, c.cancelID))
            return
        }
        val title = if (c.title.isEmpty()) c.message else c.title
        var message = if (c.title.isEmpty()) c.detail else c.message
        if (c.title.isNotEmpty() && c.detail.isNotEmpty()) message += "\n\n" + c.detail
        var answered = false
        fun answer(i: Long) {
            if (!answered) {
                answered = true
                Go.send(Event.messageBoxResult(c.req, i))
            }
        }
        val b = AlertDialog.Builder(a).setTitle(title)
        if (message.isNotEmpty()) b.setMessage(message)
        // Android's dialogs take three buttons: the default one positive,
        // the cancel one negative, another neutral; more make a list.
        val buttons = c.buttons
        if (buttons.size <= 3) {
            val order = buttons.indices.sortedBy { if (it.toLong() == c.defaultID) 0 else if (it.toLong() == c.cancelID) 2 else 1 }
            order.forEachIndexed { slot, i ->
                val l = { _: android.content.DialogInterface, _: Int -> answer(i.toLong()) }
                when {
                    slot == 0 -> b.setPositiveButton(buttons[i], l)
                    i.toLong() == c.cancelID || slot == 2 -> b.setNegativeButton(buttons[i], l)
                    else -> b.setNeutralButton(buttons[i], l)
                }
            }
        } else {
            b.setItems(buttons.toTypedArray()) { _, i -> answer(i.toLong()) }
        }
        b.setOnCancelListener { answer(c.cancelID) }
        b.show()
    }

    override fun openFiles(c: OpenFilesCall) {
        val a = activity ?: run {
            Go.send(Event.filesResult(c.req, emptyList(), ""))
            return
        }
        pickReq = c.req
        val intent = if (c.directories) {
            Intent(Intent.ACTION_OPEN_DOCUMENT_TREE)
        } else {
            Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                addCategory(Intent.CATEGORY_OPENABLE)
                val types = c.extensions.mapNotNull { MimeTypeMap.getSingleton().getMimeTypeFromExtension(it.lowercase()) }.distinct()
                type = if (types.size == 1) types[0] else "*/*"
                if (types.size > 1) putExtra(Intent.EXTRA_MIME_TYPES, types.toTypedArray())
                putExtra(Intent.EXTRA_ALLOW_MULTIPLE, c.multiple)
            }
        }
        @Suppress("DEPRECATION")
        a.startActivityForResult(intent, PICK_FILES)
    }

    fun activityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (requestCode == PICK_FILES) {
            val req = pickReq
            if (resultCode != Activity.RESULT_OK || data == null) {
                Go.send(Event.filesResult(req, emptyList(), ""))
                return
            }
            val uris = ArrayList<Uri>()
            data.clipData?.let { clip -> for (i in 0 until clip.itemCount) uris.add(clip.getItemAt(i).uri) }
            if (uris.isEmpty()) data.data?.let { uris.add(it) }
            // Copies the app can read by path: phones give apps no paths
            // of other apps' documents.
            Thread {
                try {
                    val dir = File(app.cacheDir, "picked/${System.nanoTime()}").apply { mkdirs() }
                    val paths = uris.map { copyToFile(it, dir).absolutePath }
                    Go.send(Event.filesResult(req, paths, ""))
                } catch (e: Exception) {
                    Go.send(Event.filesResult(req, emptyList(), e.toString()))
                }
            }.start()
        } else {
            WebWindow.activityResult(requestCode, resultCode, data)
        }
    }

    fun copyToFile(uri: Uri, dir: File): File {
        var name = "file"
        app.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
            if (it.moveToFirst()) name = it.getString(0) ?: name
        }
        val f = File(dir, name.replace('/', '_'))
        app.contentResolver.openInputStream(uri)!!.use { input -> f.outputStream().use { input.copyTo(it) } }
        return f
    }

    override fun shareFile(c: ShareFileCall) {
        val a = activity ?: return
        val f = File(c.path)
        val uri = FileProvider.getUriForFile(app, app.packageName + ".mygo.files", f)
        val mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(f.extension.lowercase()) ?: "application/octet-stream"
        val send = Intent(Intent.ACTION_SEND).setType(mime).putExtra(Intent.EXTRA_STREAM, uri).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        a.startActivity(Intent.createChooser(send, null))
    }

    // The system.

    private val clipboard get() = app.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager

    override fun clipboardText(): ClipboardTextReply {
        val r = ClipboardTextReply()
        val clip = clipboard.primaryClip
        if (clip != null && clip.itemCount > 0) {
            r.text = clip.getItemAt(0).coerceToText(app).toString()
            r.ok = true
        }
        return r
    }

    override fun setClipboardText(c: SetClipboardTextCall) {
        clipboard.setPrimaryClip(ClipData.newPlainText(null, c.text))
    }

    override fun openURL(c: OpenURLCall): OpenURLReply {
        val r = OpenURLReply()
        try {
            val i = Intent(Intent.ACTION_VIEW, Uri.parse(c.url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            (activity ?: app).startActivity(i)
            r.ok = true
        } catch (e: Exception) {
            r.ok = false
        }
        return r
    }

    override fun secretGet(c: SecretGetCall): SecretGetReply = Secrets.get(app, c)
    override fun secretSet(c: SecretSetCall): SecretSetReply = Secrets.set(app, c)
    override fun secretDelete(c: SecretDeleteCall): SecretDeleteReply = Secrets.delete(app, c)

    override fun notify(c: NotifyCall) {
        val a = activity
        if (Build.VERSION.SDK_INT >= 33 && app.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            if (a == null) {
                Go.send(Event.notifyDone(c.req, "mygo: notifications are not allowed"))
                return
            }
            pendingNotes.add(c)
            if (pendingNotes.size == 1) a.requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), ASK_NOTIFICATIONS)
            return
        }
        post(c)
    }

    fun permissionsResult(requestCode: Int, results: IntArray) {
        if (requestCode == ASK_NOTIFICATIONS) {
            val granted = results.isNotEmpty() && results[0] == PackageManager.PERMISSION_GRANTED
            val notes = pendingNotes
            pendingNotes = ArrayList()
            for (c in notes) if (granted) post(c) else Go.send(Event.notifyDone(c.req, "mygo: notifications are not allowed"))
        } else {
            WebWindow.permissionsResult(requestCode, results)
        }
    }

    private fun post(c: NotifyCall) {
        val nm = app.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channel = if (c.silent) "mygo.silent" else "mygo"
        if (nm.getNotificationChannel(channel) == null) {
            val ch = NotificationChannel(channel, appInfo().name, if (c.silent) NotificationManager.IMPORTANCE_LOW else NotificationManager.IMPORTANCE_DEFAULT)
            nm.createNotificationChannel(ch)
        }
        val id = c.notificationID.ifEmpty { System.nanoTime().toString() }
        val open = Intent(app, MyGoActivity::class.java).putExtra("mygo.notification", id).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP)
        val pi = PendingIntent.getActivity(app, id.hashCode(), open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val b = android.app.Notification.Builder(app, channel)
            .setSmallIcon(app.applicationInfo.icon)
            .setContentTitle(c.title)
            .setContentText(c.body)
            .setContentIntent(pi)
            .setAutoCancel(true)
        if (c.subtitle.isNotEmpty()) b.setSubText(c.subtitle)
        if (c.group.isNotEmpty()) b.setGroup(c.group)
        nm.notify(id, 0, b.build())
        Go.send(Event.notifyDone(c.req, ""))
    }

    override fun removeNotification(c: RemoveNotificationCall) {
        (app.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager).cancel(c.notificationID, 0)
    }

    override fun removeAllNotifications() {
        (app.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager).cancelAll()
    }

    override fun keepAwake(c: KeepAwakeCall) {
        val w = activity?.window ?: return
        if (c.on) w.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON) else w.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
    }

    override fun haptic(c: HapticCall) {
        val v: View = front()?.view ?: root
        val k = when (c.kind) {
            "selection" -> HapticFeedbackConstants.CLOCK_TICK
            "light" -> HapticFeedbackConstants.KEYBOARD_TAP
            "heavy" -> HapticFeedbackConstants.LONG_PRESS
            "success" -> if (Build.VERSION.SDK_INT >= 30) HapticFeedbackConstants.CONFIRM else HapticFeedbackConstants.VIRTUAL_KEY
            "warning", "error" -> if (Build.VERSION.SDK_INT >= 30) HapticFeedbackConstants.REJECT else HapticFeedbackConstants.LONG_PRESS
            else -> HapticFeedbackConstants.VIRTUAL_KEY
        }
        v.performHapticFeedback(k)
    }

    // Native UI.

    private fun surface(id: Long) = windows[id] as? SurfaceWindow

    override fun requestFrame(c: RequestFrameCall) { surface(c.id)?.requestFrame() }
    override fun setTextInput(c: SetTextInputCall) { surface(c.id)?.setTextInput(c) }
    override fun textInputChanged(c: TextInputChangedCall) { surface(c.id)?.textInputChanged() }
    override fun setCursor(c: SetCursorCall) { surface(c.id)?.setCursor(c.cursor) }
    override fun accessibilityChanged(c: AccessibilityChangedCall) { surface(c.id)?.accessibilityChanged(c.focus) }
    override fun announce(c: AnnounceCall) { (front()?.view ?: root).announceForAccessibility(c.text) }
    override fun showMenu(c: ShowMenuCall) { surface(c.id)?.showMenu(c) }
    override fun textMetrics(c: TextMetricsCall): TextMetricsReply = Text.metrics(c)
    override fun measureText(c: MeasureTextCall): MeasureTextReply = Text.measure(c)
    override fun textAdvances(c: TextAdvancesCall): TextAdvancesReply = Text.advances(c)
    override fun drawText(c: DrawTextCall): DrawTextReply = Text.draw(c)
    override fun registerFont(c: RegisterFontCall): RegisterFontReply = Text.register(c)
}

/** 0xRRGGBBAA, as Go writes colors, to Android's 0xAARRGGBB. */
fun rgbaToARGB(v: Long): Int = (((v and 0xff) shl 24) or ((v ushr 8) and 0xffffff)).toInt()

fun argbToRGBA(c: Int): Long = ((c.toLong() and 0xffffff) shl 8) or ((c.toLong() ushr 24) and 0xff)
