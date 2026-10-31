// Windows showing native UI, which Go draws with Metal into the layer of
// the window's view.

import UIKit
import QuartzCore

final class SurfaceWindow: HostWindow {
    let metalLayer = CAMetalLayer()

    override init(id: Int) {
        super.init(id: id)
        metalLayer.isOpaque = true
        layer.addSublayer(metalLayer)
    }

    required init?(coder: NSCoder) { fatalError() }

    func requestFrame() {}
    func setTextInput(_ c: SetTextInputCall) {}
    func textInputChanged() {}
    func accessibilityChanged(focus: Int) {}
    func showMenu(_ c: ShowMenuCall) {}
}
