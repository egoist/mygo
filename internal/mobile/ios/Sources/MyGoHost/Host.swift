// The calls Go makes of the host (HostCalls), on the main thread.

import UIKit
import WebKit
import UserNotifications
import UniformTypeIdentifiers
import Security

final class Host: NSObject, HostCalls, UNUserNotificationCenterDelegate, UIDocumentPickerDelegate {
    static let shared = Host()

    let root = RootViewController()
    var window: UIWindow?
    var windows: [Int: HostWindow] = [:]
    /// The windows on screen, the one in front last.
    var pickers: [UIDocumentPickerViewController: Int] = [:]

    /// The view controller to present over: the root, or what it presents.
    var presenter: UIViewController {
        var vc: UIViewController = root
        while let p = vc.presentedViewController { vc = p }
        return vc
    }

    func appInfo() -> AppInfoReply {
        var r = AppInfoReply()
        let b = Bundle.main
        r.name = (b.object(forInfoDictionaryKey: "CFBundleDisplayName") ?? b.object(forInfoDictionaryKey: "CFBundleName")) as? String ?? ""
        r.version = b.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
        r.identifier = b.bundleIdentifier ?? ""
        let fm = FileManager.default
        r.filesDir = fm.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?.path ?? ""
        r.cacheDir = fm.urls(for: .cachesDirectory, in: .userDomainMask).first?.path ?? ""
        r.tempDir = NSTemporaryDirectory()
        r.documentsDir = fm.urls(for: .documentDirectory, in: .userDomainMask).first?.path ?? ""
        r.resourcesDir = b.resourcePath ?? ""
        r.languages = Locale.preferredLanguages
        r.regionLocale = Locale.current.identifier
        r.firstWeekday = Calendar.current.firstWeekday - 1
        let j = DateFormatter.dateFormat(fromTemplate: "j", options: 0, locale: .current) ?? ""
        r.hourCycle = j.contains("a") ? 12 : 24
        r.system = UIDevice.current.systemName
        r.systemVersion = UIDevice.current.systemVersion
        r.model = UIDevice.current.model
        return r
    }

    func theme() -> ThemeReply {
        var r = ThemeReply()
        let traits = window?.traitCollection ?? root.traitCollection
        r.dark = traits.userInterfaceStyle == .dark
        r.accent = (window?.tintColor ?? .systemBlue).resolvedColor(with: traits).rgba
        r.reduceMotion = UIAccessibility.isReduceMotionEnabled
        r.reduceTransparency = UIAccessibility.isReduceTransparencyEnabled
        r.increaseContrast = UIAccessibility.isDarkerSystemColorsEnabled
        r.textScale = Double(UIFontMetrics.default.scaledValue(for: 17) / 17)
        return r
    }

    func setAppearance(_ c: SetAppearanceCall) {
        let style: UIUserInterfaceStyle = c.source == "dark" ? .dark : c.source == "light" ? .light : .unspecified
        window?.overrideUserInterfaceStyle = style
    }

    func screen() -> ScreenReply {
        var r = ScreenReply()
        let s = window?.windowScene?.screen ?? UIScreen.main
        r.width = Double(s.bounds.width)
        r.height = Double(s.bounds.height)
        r.scale = Double(s.scale)
        return r
    }

    func log(_ c: LogCall) { NSLog("%@", c.line) }

    // Windows.

    func createWindow(_ c: CreateWindowCall) -> CreateWindowReply {
        var r = CreateWindowReply()
        let w: HostWindow
        if c.surface {
            let s = SurfaceWindow(id: c.id)
            r.layer = Int(bitPattern: Unmanaged.passUnretained(s.layer).toOpaque())
            w = s
        } else {
            w = WebWindow(id: c.id, c)
        }
        if c.hasBackground { w.setBackground(UIColor(rgba: c.background)) }
        windows[c.id] = w
        return r
    }

    func showWindow(_ c: ShowWindowCall) {
        guard let w = windows[c.id] else { return }
        Navigation.shared.show(w)
        w.reportSize()
    }

    func hideWindow(_ c: HideWindowCall) {
        guard let w = windows[c.id] else { return }
        Navigation.shared.hide(w)
    }

    func closeWindow(_ c: CloseWindowCall) {
        guard let w = windows.removeValue(forKey: c.id) else { return }
        Navigation.shared.hide(w)
        Navigation.shared.forget(w)
        w.close()
    }

    func setTitle(_ c: SetTitleCall) { windows[c.id].map { Navigation.shared.controller($0).title = c.title } }
    func createTabs(_ c: CreateTabsCall) { Navigation.shared.createTabs(c.id) }
    func addTab(_ c: AddTabCall) {
        guard let w = windows[c.window] else { return }
        Navigation.shared.addTab(c.id, w, title: c.title, symbol: c.symbol, icon: c.icon)
    }
    func showTabs(_ c: ShowTabsCall) { Navigation.shared.showTabs(c.id) }
    func selectTab(_ c: SelectTabCall) { Navigation.shared.tabs[c.id]?.selectedIndex = c.index }
    func setTabBadge(_ c: SetTabBadgeCall) {
        Navigation.shared.tabs[c.id]?.viewControllers?[safe: c.index]?.tabBarItem.badgeValue = c.badge.isEmpty ? nil : c.badge
    }
    func closeTabs(_ c: CloseTabsCall) { Navigation.shared.closeTabs(c.id) }
    func pushWindow(_ c: PushWindowCall) {
        guard let p = windows[c.parent], let w = windows[c.child] else { return }
        Navigation.shared.push(w, over: p, title: c.title, animated: c.animated)
    }
    func popWindow(_ c: PopWindowCall) {
        guard let w = windows[c.id] else { return }
        Navigation.shared.pop(w, animated: c.animated)
    }

    func setBackground(_ c: SetBackgroundCall) { windows[c.id]?.setBackground(UIColor(rgba: c.color)) }
    func focus(_ c: FocusCall) { windows[c.id]?.focus() }

    // Pages.

    func web(_ id: Int) -> WebWindow? { windows[id] as? WebWindow }

    func loadURL(_ c: LoadURLCall) { web(c.id)?.load(c.url) }
    func loadHTML(_ c: LoadHTMLCall) { web(c.id)?.loadHTML(c.html, base: c.baseURL) }
    func reload(_ c: ReloadCall) {
        if c.ignoreCache { web(c.id)?.webView.reloadFromOrigin() } else { web(c.id)?.webView.reload() }
    }
    func stopLoading(_ c: StopLoadingCall) { web(c.id)?.webView.stopLoading() }
    func goBack(_ c: GoBackCall) { web(c.id)?.webView.goBack() }
    func goForward(_ c: GoForwardCall) { web(c.id)?.webView.goForward() }
    func eval(_ c: EvalCall) { web(c.id)?.webView.evaluateJavaScript(c.js, completionHandler: nil) }

    func callAsync(_ c: CallAsyncCall) {
        guard let w = web(c.id) else {
            Go.send(Event.asyncResult(req: c.req, result: "", error: "mygo: the window is closed"))
            return
        }
        w.webView.callAsyncJavaScript(c.body, arguments: [:], in: nil, in: .page) { result in
            switch result {
            case .success(let v):
                Go.send(Event.asyncResult(req: c.req, result: (v as? String) ?? "", error: ""))
            case .failure(let e):
                Go.send(Event.asyncResult(req: c.req, result: "", error: e.localizedDescription))
            }
        }
    }

    func setZoom(_ c: SetZoomCall) { web(c.id)?.webView.pageZoom = CGFloat(c.factor) }
    func setUserAgent(_ c: SetUserAgentCall) { web(c.id)?.webView.customUserAgent = c.userAgent.isEmpty ? nil : c.userAgent }
    func setUserScripts(_ c: SetUserScriptsCall) { web(c.id)?.setScripts(c.scripts, c.scriptFlags) }

    func capturePage(_ c: CapturePageCall) {
        guard let w = web(c.id) else {
            Go.send(Event.captured(req: c.req, png: [], error: "mygo: the window is closed"))
            return
        }
        w.webView.takeSnapshot(with: nil) { image, error in
            if let png = image?.pngData() {
                Go.send(Event.captured(req: c.req, png: Array(png), error: ""))
            } else {
                Go.send(Event.captured(req: c.req, png: [], error: error?.localizedDescription ?? "mygo: no snapshot"))
            }
        }
    }

    func userAgent(_ c: UserAgentCall) -> UserAgentReply {
        var r = UserAgentReply()
        r.userAgent = web(c.id)?.userAgent ?? ""
        return r
    }

    func schemeRespond(_ c: SchemeRespondCall) { SchemeHandler.respond(c) }
    func schemeData(_ c: SchemeDataCall) { SchemeHandler.data(c) }
    func schemeFinish(_ c: SchemeFinishCall) { SchemeHandler.finish(c.req) }
    func schemeFail(_ c: SchemeFailCall) { SchemeHandler.fail(c.req, c.message) }

    // Dialogs.

    func messageBox(_ c: MessageBoxCall) {
        let title = c.title.isEmpty ? c.message : c.title
        var message = c.title.isEmpty ? c.detail : c.message
        if !c.title.isEmpty, !c.detail.isEmpty { message += "\n\n" + c.detail }
        let a = UIAlertController(title: title, message: message.isEmpty ? nil : message, preferredStyle: .alert)
        for (i, label) in c.buttons.enumerated() {
            let style: UIAlertAction.Style = i == c.cancelID && c.buttons.count > 1 ? .cancel : .default
            let action = UIAlertAction(title: label, style: style) { _ in
                Go.send(Event.messageBoxResult(req: c.req, response: i))
            }
            a.addAction(action)
            if i == c.defaultID { a.preferredAction = action }
        }
        presenter.present(a, animated: true)
    }

    func openFiles(_ c: OpenFilesCall) {
        var types: [UTType] = c.extensions.compactMap { UTType(filenameExtension: $0) }
        if c.directories { types = [.folder] } else if types.isEmpty { types = [.item] }
        let p = UIDocumentPickerViewController(forOpeningContentTypes: types, asCopy: !c.directories)
        p.allowsMultipleSelection = c.multiple
        p.delegate = self
        pickers[p] = c.req
        presenter.present(p, animated: true)
    }

    func documentPicker(_ p: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        guard let req = pickers.removeValue(forKey: p) else { return }
        for u in urls where !u.isFileURL || u.hasDirectoryPath { _ = u.startAccessingSecurityScopedResource() }
        Go.send(Event.filesResult(req: req, paths: urls.map { $0.path }, error: ""))
    }

    func documentPickerWasCancelled(_ p: UIDocumentPickerViewController) {
        guard let req = pickers.removeValue(forKey: p) else { return }
        Go.send(Event.filesResult(req: req, paths: [], error: ""))
    }

    func shareFile(_ c: ShareFileCall) {
        let vc = UIActivityViewController(activityItems: [URL(fileURLWithPath: c.path)], applicationActivities: nil)
        vc.popoverPresentationController?.sourceView = root.view
        vc.popoverPresentationController?.sourceRect = CGRect(x: root.view.bounds.midX, y: root.view.bounds.midY, width: 0, height: 0)
        presenter.present(vc, animated: true)
    }

    // The system.

    func clipboardText() -> ClipboardTextReply {
        var r = ClipboardTextReply()
        let pb = UIPasteboard.general
        if pb.hasStrings {
            r.text = pb.string ?? ""
            r.ok = true
        }
        return r
    }

    func setClipboardText(_ c: SetClipboardTextCall) { UIPasteboard.general.string = c.text }

    func openURL(_ c: OpenURLCall) -> OpenURLReply {
        var r = OpenURLReply()
        guard let u = URL(string: c.url) else { return r }
        UIApplication.shared.open(u)
        r.ok = true
        return r
    }

    func secretQuery(_ service: String, _ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: account]
    }

    func secretGet(_ c: SecretGetCall) -> SecretGetReply {
        var r = SecretGetReply()
        var q = secretQuery(c.service, c.account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &out)
        switch status {
        case errSecSuccess:
            r.value = Array((out as? Data) ?? Data())
            r.found = true
        case errSecItemNotFound:
            break
        default:
            r.error = "mygo: keychain error \(status)"
        }
        return r
    }

    func secretSet(_ c: SecretSetCall) -> SecretSetReply {
        var r = SecretSetReply()
        let q = secretQuery(c.service, c.account)
        let attrs: [String: Any] = [kSecValueData as String: Data(c.value),
                                    kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock]
        var status = SecItemUpdate(q as CFDictionary, attrs as CFDictionary)
        if status == errSecItemNotFound {
            status = SecItemAdd(q.merging(attrs) { $1 } as CFDictionary, nil)
        }
        if status != errSecSuccess { r.error = "mygo: keychain error \(status)" }
        return r
    }

    func secretDelete(_ c: SecretDeleteCall) -> SecretDeleteReply {
        var r = SecretDeleteReply()
        let status = SecItemDelete(secretQuery(c.service, c.account) as CFDictionary)
        if status != errSecSuccess, status != errSecItemNotFound { r.error = "mygo: keychain error \(status)" }
        return r
    }

    func notify(_ c: NotifyCall) {
        let center = UNUserNotificationCenter.current()
        center.requestAuthorization(options: [.alert, .sound, .badge]) { granted, error in
            guard granted else {
                Go.send(Event.notifyDone(req: c.req, error: error?.localizedDescription ?? "mygo: notifications are not allowed"))
                return
            }
            let content = UNMutableNotificationContent()
            content.title = c.title
            content.subtitle = c.subtitle
            content.body = c.body
            if !c.group.isEmpty { content.threadIdentifier = c.group }
            if !c.silent { content.sound = .default }
            let id = c.notificationID.isEmpty ? UUID().uuidString : c.notificationID
            center.add(UNNotificationRequest(identifier: id, content: content, trigger: nil)) { error in
                Go.send(Event.notifyDone(req: c.req, error: error?.localizedDescription ?? ""))
            }
        }
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent n: UNNotification,
                                withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .list, .sound])
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                withCompletionHandler done: @escaping () -> Void) {
        Go.send(Event.notificationClicked(notificationID: response.notification.request.identifier))
        done()
    }

    func removeNotification(_ c: RemoveNotificationCall) {
        let center = UNUserNotificationCenter.current()
        center.removeDeliveredNotifications(withIdentifiers: [c.notificationID])
        center.removePendingNotificationRequests(withIdentifiers: [c.notificationID])
    }

    func removeAllNotifications() {
        UNUserNotificationCenter.current().removeAllDeliveredNotifications()
        UNUserNotificationCenter.current().removeAllPendingNotificationRequests()
    }

    func keepAwake(_ c: KeepAwakeCall) { UIApplication.shared.isIdleTimerDisabled = c.on }

    func haptic(_ c: HapticCall) {
        switch c.kind {
        case "selection": UISelectionFeedbackGenerator().selectionChanged()
        case "light": UIImpactFeedbackGenerator(style: .light).impactOccurred()
        case "heavy": UIImpactFeedbackGenerator(style: .heavy).impactOccurred()
        case "success": UINotificationFeedbackGenerator().notificationOccurred(.success)
        case "warning": UINotificationFeedbackGenerator().notificationOccurred(.warning)
        case "error": UINotificationFeedbackGenerator().notificationOccurred(.error)
        default: UIImpactFeedbackGenerator(style: .medium).impactOccurred()
        }
    }

    // Native UI.

    func surface(_ id: Int) -> SurfaceWindow? { windows[id] as? SurfaceWindow }

    func requestFrame(_ c: RequestFrameCall) { surface(c.id)?.requestFrame() }
    func setTextInput(_ c: SetTextInputCall) { surface(c.id)?.setTextInput(c) }
    func textInputChanged(_ c: TextInputChangedCall) { surface(c.id)?.textInputChanged() }
    func setCursor(_ c: SetCursorCall) {}
    func accessibilityChanged(_ c: AccessibilityChangedCall) { surface(c.id)?.accessibilityChanged(focus: c.focus) }
    func announce(_ c: AnnounceCall) { UIAccessibility.post(notification: .announcement, argument: c.text) }
    func showMenu(_ c: ShowMenuCall) { surface(c.id)?.showMenu(c) }
    // Go lays out and draws text with Core Text on iOS.
    func textMetrics(_ c: TextMetricsCall) -> TextMetricsReply { TextMetricsReply() }
    func measureText(_ c: MeasureTextCall) -> MeasureTextReply { MeasureTextReply() }
    func textAdvances(_ c: TextAdvancesCall) -> TextAdvancesReply { TextAdvancesReply() }
    func drawText(_ c: DrawTextCall) -> DrawTextReply { DrawTextReply() }
    func registerFont(_ c: RegisterFontCall) -> RegisterFontReply { RegisterFontReply() }
}
