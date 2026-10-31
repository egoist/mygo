// The iOS host of MyGo: the app's delegates, which start Go, and the
// bridge between Go and UIKit. Go runs on a thread of its own and makes
// its calls (HostCalls) through mygo_drain on the main thread; events go
// to Go through mygo_event.

import UIKit
import os

/// The way to Go.
enum Go {
    /// The descriptor of the process's standard error before Go took it,
    /// where the app's output still goes, as Xcode and simctl show it.
    static var stderr: Int32 = -1
    static let log = OSLog(subsystem: Bundle.main.bundleIdentifier ?? "mygo", category: "app")

    /// Sends an event Go does not answer.
    static func send(_ msg: [UInt8]) { _ = ask(msg) }

    /// Sends an event and returns Go's answer, nil when Go answers none
    /// (the event is not one it answers, or Go is not running yet).
    @discardableResult
    static func ask(_ msg: [UInt8]) -> [UInt8]? {
        var msg = msg
        var reply: UnsafeMutablePointer<UInt8>? = nil
        var n = 0
        msg.withUnsafeMutableBufferPointer { p in
            mygo_event(p.baseAddress, p.count, Thread.isMainThread ? 1 : 0, &reply, &n)
        }
        guard let reply else { return nil }
        defer { mygo_free(reply) }
        return Array(UnsafeBufferPointer(start: reply, count: n))
    }

    /// Decodes Go's answer to an event, or nil when there is none.
    static func answer<T>(_ msg: [UInt8], _ decode: (inout WireReader) -> T) -> T? {
        guard let reply = ask(msg) else { return nil }
        return reply.withUnsafeBytes { buf in
            var r = WireReader(buf)
            return decode(&r)
        }
    }

    static func start() {
        stderr = dup(2)
        var host = mygo_host(
            call: { msg, len, reply, replyLen in
                let out = dispatchCall(Host.shared, UnsafeRawBufferPointer(start: msg, count: len))
                guard let out, let reply, let replyLen else {
                    reply?.pointee = nil
                    replyLen?.pointee = 0
                    return
                }
                let p = malloc(max(out.count, 1))!.assumingMemoryBound(to: UInt8.self)
                out.withUnsafeBufferPointer { b in
                    if let base = b.baseAddress { p.update(from: base, count: out.count) }
                }
                reply.pointee = p
                replyLen.pointee = out.count
            },
            post: {
                DispatchQueue.main.async { mygo_drain() }
            },
            log: { isError, line, len in
                guard let line else { return }
                let s = String(decoding: UnsafeBufferPointer(start: line, count: len), as: UTF8.self)
                os_log("%{public}@", log: Go.log, type: isError != 0 ? .error : .default, s)
                if Go.stderr >= 0 {
                    var b = Array(s.utf8)
                    b.append(10)
                    _ = b.withUnsafeBytes { write(Go.stderr, $0.baseAddress, $0.count) }
                }
            })
        mygo_start(&host)
    }
}

@main
final class AppDelegate: UIResponder, UIApplicationDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = Host.shared
        Go.start()
        return true
    }

    func application(_ application: UIApplication, configurationForConnecting session: UISceneSession, options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let c = UISceneConfiguration(name: nil, sessionRole: session.role)
        c.delegateClass = SceneDelegate.self
        return c
    }

    func applicationDidReceiveMemoryWarning(_ application: UIApplication) { Go.send(Event.memoryWarning()) }
    func applicationWillTerminate(_ application: UIApplication) { Go.ask(Event.terminating()) }
}

final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options: UIScene.ConnectionOptions) {
        guard let scene = scene as? UIWindowScene else { return }
        let w = UIWindow(windowScene: scene)
        w.rootViewController = Host.shared.root
        w.makeKeyAndVisible()
        window = w
        Host.shared.window = w
        let urls = options.urlContexts.map { $0.url.absoluteString } + options.userActivities.compactMap { $0.webpageURL?.absoluteString }
        if !urls.isEmpty { Go.send(Event.openURLs(urls: urls)) }
    }

    func scene(_ scene: UIScene, openURLContexts contexts: Set<UIOpenURLContext>) {
        Go.send(Event.openURLs(urls: contexts.map { $0.url.absoluteString }))
    }

    func scene(_ scene: UIScene, continue activity: NSUserActivity) {
        if let url = activity.webpageURL { Go.send(Event.openURLs(urls: [url.absoluteString])) }
    }

    func sceneDidBecomeActive(_ scene: UIScene) { Go.send(Event.active(active: true)) }
    func sceneWillResignActive(_ scene: UIScene) { Go.send(Event.active(active: false)) }
    func sceneDidEnterBackground(_ scene: UIScene) { Go.send(Event.background(background: true)) }
    func sceneWillEnterForeground(_ scene: UIScene) { Go.send(Event.background(background: false)) }
}

/// The view controller of the app's window, whose view holds the app's
/// windows, the one in front last.
final class RootViewController: UIViewController {
    var keyboard: CGFloat = 0

    override func loadView() {
        view = UIView()
        view.backgroundColor = .systemBackground
        let nc = NotificationCenter.default
        nc.addObserver(self, selector: #selector(keyboardChanged(_:)), name: UIResponder.keyboardWillChangeFrameNotification, object: nil)
        nc.addObserver(self, selector: #selector(keyboardChanged(_:)), name: UIResponder.keyboardWillHideNotification, object: nil)
        nc.addObserver(self, selector: #selector(settingsChanged), name: UIAccessibility.reduceMotionStatusDidChangeNotification, object: nil)
        nc.addObserver(self, selector: #selector(settingsChanged), name: UIContentSizeCategory.didChangeNotification, object: nil)
        nc.addObserver(self, selector: #selector(localeChanged), name: NSLocale.currentLocaleDidChangeNotification, object: nil)
        registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (_: RootViewController, _) in
            Go.send(Event.themeChanged())
        }
    }

    @objc func settingsChanged() { Go.send(Event.themeChanged()) }
    @objc func localeChanged() { Go.send(Event.localeChanged()) }

    @objc func keyboardChanged(_ n: Notification) {
        var height: CGFloat = 0
        if n.name != UIResponder.keyboardWillHideNotification,
           let frame = (n.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? NSValue)?.cgRectValue {
            let local = view.convert(frame, from: nil)
            height = max(0, view.bounds.maxY - local.minY)
        }
        if height != keyboard {
            keyboard = height
            for w in Host.shared.windows.values { w.reportSize() }
        }
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        for w in Host.shared.windows.values {
            w.frame = view.bounds
            w.reportSize()
        }
    }

    override func viewSafeAreaInsetsDidChange() {
        super.viewSafeAreaInsetsDidChange()
        for w in Host.shared.windows.values { w.reportSize() }
    }

    override var childForStatusBarStyle: UIViewController? { nil }
}

/// A window of the app: a view filling the screen, which shows a page or
/// native UI.
class HostWindow: UIView {
    let id: Int
    private var lastSize: [Double] = []

    init(id: Int) {
        self.id = id
        super.init(frame: Host.shared.root.view.bounds)
        autoresizingMask = [.flexibleWidth, .flexibleHeight]
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Tells Go the window's size and what covers it, when they changed.
    func reportSize() {
        let s = safeAreaInsets
        let scale = window?.screen.scale ?? traitCollection.displayScale
        let size = [Double(bounds.width), Double(bounds.height), Double(scale), Double(s.top), Double(s.left),
                    Double(s.bottom), Double(s.right), Double(Host.shared.root.keyboard)]
        if size == lastSize || bounds.width == 0 { return }
        lastSize = size
        Go.send(Event.resized(id: id, width: size[0], height: size[1], scale: size[2], safeTop: size[3],
                              safeLeft: size[4], safeBottom: size[5], safeRight: size[6], keyboard: size[7]))
    }

    override func safeAreaInsetsDidChange() {
        super.safeAreaInsetsDidChange()
        reportSize()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        reportSize()
    }

    func close() { removeFromSuperview() }
    func focus() {}
    func setBackground(_ color: UIColor) { backgroundColor = color }
}

extension UIColor {
    /// The color of 0xRRGGBBAA.
    convenience init(rgba v: Int) {
        self.init(red: CGFloat((v >> 24) & 0xff) / 255, green: CGFloat((v >> 16) & 0xff) / 255,
                  blue: CGFloat((v >> 8) & 0xff) / 255, alpha: CGFloat(v & 0xff) / 255)
    }

    var rgba: Int {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        getRed(&r, green: &g, blue: &b, alpha: &a)
        func c(_ v: CGFloat) -> Int { Int((min(max(v, 0), 1) * 255).rounded()) }
        return c(r) << 24 | c(g) << 16 | c(b) << 8 | c(a)
    }
}
