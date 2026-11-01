// The navigation of phones: each window in a view controller of its own,
// in a navigation controller (a stack, whose bar shows once a window is
// pushed), the navigation controllers of a bar of tabs in a tab bar
// controller, all children of the root view controller, the one shown
// last in front.

import UIKit

/// The view controller of a window, whose view the window is.
final class WindowController: UIViewController {
    let window: HostWindow
    /// barAtRoot shows the navigation bar at the bottom of a stack, as a
    /// tab's, with the window's title.
    var barAtRoot = false

    init(_ w: HostWindow) {
        window = w
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) { fatalError() }

    override func loadView() { view = window }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        guard let nav = navigationController else { return }
        let atRoot = nav.viewControllers.first === self
        nav.setNavigationBarHidden(atRoot && !barAtRoot, animated: animated)
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        window.reportSize()
    }
}

final class Navigation: NSObject, UINavigationControllerDelegate, UITabBarControllerDelegate {
    static let shared = Navigation()

    private var controllers: [Int: WindowController] = [:]
    /// The windows of each stack, as it last showed them, to tell which
    /// went (Popped).
    private var stacks: [ObjectIdentifier: [Int]] = [:]
    var tabs: [Int: UITabBarController] = [:]
    private var tabIDs: [ObjectIdentifier: Int] = [:]
    private var root: RootViewController { Host.shared.root }

    func controller(_ w: HostWindow) -> WindowController {
        if let c = controllers[w.id] { return c }
        let c = WindowController(w)
        controllers[w.id] = c
        return c
    }

    func forget(_ w: HostWindow) { controllers[w.id] = nil }

    private func stack(_ root: UIViewController) -> UINavigationController {
        let nav = UINavigationController(rootViewController: root)
        nav.delegate = self
        nav.navigationBar.prefersLargeTitles = true
        stacks[ObjectIdentifier(nav)] = [(root as? WindowController)?.window.id ?? 0]
        return nav
    }

    /// add puts a child view controller of the root in front.
    private func add(_ vc: UIViewController) {
        if vc.parent !== root {
            vc.willMove(toParent: nil)
            vc.view.removeFromSuperview()
            vc.removeFromParent()
            root.addChild(vc)
            vc.view.frame = root.view.bounds
            vc.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            root.view.addSubview(vc.view)
            vc.didMove(toParent: root)
        } else {
            root.view.bringSubviewToFront(vc.view)
        }
    }

    private func remove(_ vc: UIViewController) {
        vc.willMove(toParent: nil)
        vc.view.removeFromSuperview()
        vc.removeFromParent()
    }

    /// show brings a window to the front: its tab, its stack, or a stack of
    /// its own.
    func show(_ w: HostWindow) {
        let c = controller(w)
        if let nav = c.navigationController {
            if let tbc = nav.tabBarController {
                tbc.selectedViewController = nav
                add(tbc)
            } else {
                add(nav)
            }
            return
        }
        add(stack(c))
    }

    /// hide takes a window off the screen: its stack, or the windows above
    /// it in its stack.
    func hide(_ w: HostWindow) {
        guard let c = controllers[w.id], let nav = c.navigationController else { return }
        if nav.viewControllers.first === c {
            if nav.tabBarController == nil {
                stacks[ObjectIdentifier(nav)] = nil
                remove(nav)
            }
        } else if let i = nav.viewControllers.firstIndex(of: c) {
            nav.setViewControllers(Array(nav.viewControllers[..<i]), animated: false)
        }
    }

    func push(_ w: HostWindow, over parent: HostWindow, title: String, animated: Bool) {
        let p = controller(parent)
        let nav = p.navigationController ?? {
            let n = stack(p)
            add(n)
            return n
        }()
        let c = controller(w)
        c.title = title
        // Pushed windows take the bar's small title, as Settings' pages.
        c.navigationItem.largeTitleDisplayMode = .never
        // Above the window it is pushed over, as it was pushed from it.
        if let i = nav.viewControllers.firstIndex(of: p), i < nav.viewControllers.count - 1 {
            nav.setViewControllers(Array(nav.viewControllers[...i]), animated: false)
        }
        nav.pushViewController(c, animated: animated)
    }

    func pop(_ w: HostWindow, animated: Bool) {
        guard let c = controllers[w.id], let nav = c.navigationController,
              let i = nav.viewControllers.firstIndex(of: c), i > 0 else { return }
        nav.popToViewController(nav.viewControllers[i - 1], animated: animated)
    }

    func navigationController(_ nav: UINavigationController, didShow vc: UIViewController, animated: Bool) {
        let ids = nav.viewControllers.compactMap { ($0 as? WindowController)?.window.id }
        let key = ObjectIdentifier(nav)
        for id in stacks[key] ?? [] where !ids.contains(id) {
            Go.send(Event.popped(id: id))
        }
        stacks[key] = ids
    }

    // Tabs.

    func createTabs(_ id: Int) {
        let tbc = UITabBarController()
        tbc.delegate = self
        tbc.viewControllers = []
        tabs[id] = tbc
        tabIDs[ObjectIdentifier(tbc)] = id
    }

    func addTab(_ id: Int, _ w: HostWindow, title: String, symbol: String, icon: [UInt8]) {
        guard let tbc = tabs[id] else { return }
        let c = controller(w)
        if let old = c.navigationController {
            // Shown on its own before: the tab takes it.
            remove(old)
            old.setViewControllers([], animated: false)
        }
        c.title = title
        c.barAtRoot = !title.isEmpty
        let nav = stack(c)
        var image = symbol.isEmpty ? nil : UIImage(systemName: symbol)
        if image == nil, !icon.isEmpty {
            image = UIImage(data: Data(icon), scale: 3)?.withRenderingMode(.alwaysTemplate)
        }
        nav.tabBarItem = UITabBarItem(title: title, image: image, tag: tbc.viewControllers?.count ?? 0)
        tbc.viewControllers = (tbc.viewControllers ?? []) + [nav]
    }

    func showTabs(_ id: Int) {
        guard let tbc = tabs[id] else { return }
        add(tbc)
    }

    func closeTabs(_ id: Int) {
        guard let tbc = tabs.removeValue(forKey: id) else { return }
        tabIDs[ObjectIdentifier(tbc)] = nil
        remove(tbc)
    }

    func tabBarController(_ tbc: UITabBarController, didSelect vc: UIViewController) {
        guard let id = tabIDs[ObjectIdentifier(tbc)], let i = tbc.viewControllers?.firstIndex(of: vc) else { return }
        Go.send(Event.tabSelected(id: id, index: i))
    }
}

extension Array {
    subscript(safe i: Int) -> Element? { i >= 0 && i < count ? self[i] : nil }
}
