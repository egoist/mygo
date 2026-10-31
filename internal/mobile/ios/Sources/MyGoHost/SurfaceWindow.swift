// Windows showing native UI. Go's Metal renderer draws into a CAMetalLayer
// it adds to the view's layer; the view gives Go the display's refreshes,
// the fingers, the keyboard (UITextInput, with the marked text of input
// methods) and the questions of VoiceOver.

import UIKit
import QuartzCore

final class SurfaceWindow: HostWindow, UITextInput {
    private var link: CADisplayLink?
    private var fingers: [ObjectIdentifier: Int] = [:]
    private var nextFinger = 1

    // The text being edited, as Go last told it (SetTextInput): its runes,
    // the selection in them, and the input method's text being composed.
    private var text: [Character] = []
    private var selStart = 0, selEnd = 0
    private var marked: String?
    private var caret = CGRect.zero
    private var editing = false
    private var secure = false

    private var elements: [AccessElement]?

    override init(id: Int) {
        super.init(id: id)
        isMultipleTouchEnabled = true
        backgroundColor = .systemBackground
        let l = CADisplayLink(target: self, selector: #selector(tick))
        l.preferredFrameRateRange = CAFrameRateRange(minimum: 30, maximum: 120, preferred: 120)
        l.isPaused = true
        l.add(to: .main, forMode: .common)
        link = l
    }

    required init?(coder: NSCoder) { fatalError() }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        guard let w = window else { return }
        let hz = Double(w.screen.maximumFramesPerSecond)
        Go.send(Event.surfaceChanged(id: id, handle: Int(bitPattern: Unmanaged.passUnretained(layer).toOpaque()),
                                     width: Double(bounds.width), height: Double(bounds.height),
                                     scale: Double(w.screen.scale), refresh: hz))
    }

    override func close() {
        link?.invalidate()
        link = nil
        if isFirstResponder { resignFirstResponder() }
        super.close()
    }

    func requestFrame() { link?.isPaused = false }

    @objc private func tick() {
        // One frame per request: Go asks again while it animates.
        link?.isPaused = true
        Go.send(Event.frame(id: id))
    }

    // Fingers.

    private func send(_ touches: Set<UITouch>, _ phase: String) {
        for t in touches {
            let key = ObjectIdentifier(t)
            var n = fingers[key]
            if n == nil {
                n = nextFinger
                nextFinger += 1
                fingers[key] = n
            }
            let p = t.location(in: self)
            let kind: String
            switch t.type {
            case .pencil: kind = "pen"
            case .indirectPointer: kind = "mouse"
            default: kind = "touch"
            }
            Go.send(Event.touch(id: id, pointer: n!, phase: phase, kind: kind, x: Double(p.x), y: Double(p.y),
                                pressure: Double(t.force), buttons: 1, time: t.timestamp))
            if phase == "up" || phase == "cancel" { fingers[key] = nil }
        }
    }

    override func touchesBegan(_ t: Set<UITouch>, with e: UIEvent?) { send(t, "down") }
    override func touchesMoved(_ t: Set<UITouch>, with e: UIEvent?) { send(t, "move") }
    override func touchesEnded(_ t: Set<UITouch>, with e: UIEvent?) { send(t, "up") }
    override func touchesCancelled(_ t: Set<UITouch>, with e: UIEvent?) { send(t, "cancel") }

    // A hardware keyboard's keys.

    private static let keyNames: [UIKeyboardHIDUsage: String] = [
        .keyboardReturnOrEnter: "Enter", .keyboardEscape: "Escape", .keyboardDeleteOrBackspace: "Backspace",
        .keyboardTab: "Tab", .keyboardSpacebar: "Space", .keyboardDeleteForward: "Delete", .keyboardHome: "Home",
        .keyboardEnd: "End", .keyboardPageUp: "PageUp", .keyboardPageDown: "PageDown", .keyboardLeftArrow: "ArrowLeft",
        .keyboardRightArrow: "ArrowRight", .keyboardUpArrow: "ArrowUp", .keyboardDownArrow: "ArrowDown",
    ]

    private func keys(_ presses: Set<UIPress>, down: Bool) -> Set<UIPress> {
        var rest = Set<UIPress>()
        for p in presses {
            guard let k = p.key else { rest.insert(p); continue }
            let name = SurfaceWindow.keyNames[k.keyCode] ?? k.charactersIgnoringModifiers
            var mods = 0
            if k.modifierFlags.contains(.shift) { mods |= 1 }
            if k.modifierFlags.contains(.control) { mods |= 2 }
            if k.modifierFlags.contains(.alternate) { mods |= 4 }
            if k.modifierFlags.contains(.command) { mods |= 8 }
            let handled = Go.answer(Event.key(id: id, down: down, key: name, text: k.characters, mods: mods, repeated: false)) {
                KeyReply(&$0).handled
            } ?? false
            // Keys typing text while editing reach insertText too.
            if !handled && !(editing && SurfaceWindow.keyNames[k.keyCode] == nil) { rest.insert(p) }
        }
        return rest
    }

    override func pressesBegan(_ presses: Set<UIPress>, with e: UIPressesEvent?) {
        let rest = keys(presses, down: true)
        if !rest.isEmpty { super.pressesBegan(rest, with: e) }
    }

    override func pressesEnded(_ presses: Set<UIPress>, with e: UIPressesEvent?) {
        let rest = keys(presses, down: false)
        if !rest.isEmpty { super.pressesEnded(rest, with: e) }
    }

    // The keyboard.

    override var canBecomeFirstResponder: Bool { true }

    func setTextInput(_ c: SetTextInputCall) {
        let wasEditing = editing
        editing = c.active
        secure = c.secure
        caret = CGRect(x: c.x, y: c.y, width: max(c.width, 1), height: max(c.height, 1))
        let newText = Array(c.text)
        let changed = newText != text || c.selStart != selStart || c.selEnd != selEnd
        if changed { inputDelegate?.textWillChange(self) }
        text = newText
        selStart = max(0, min(c.selStart, text.count))
        selEnd = max(0, min(c.selEnd, text.count))
        if changed { inputDelegate?.textDidChange(self) }
        if editing && !isFirstResponder {
            becomeFirstResponder()
        } else if !editing && wasEditing && isFirstResponder {
            marked = nil
            resignFirstResponder()
        }
    }

    func textInputChanged() {
        inputDelegate?.selectionWillChange(self)
        inputDelegate?.selectionDidChange(self)
    }

    override func resignFirstResponder() -> Bool {
        let r = super.resignFirstResponder()
        if editing { Go.send(Event.textInputEnded(id: id)) }
        return r
    }

    var keyboardType: UIKeyboardType = .default
    var isSecureTextEntry: Bool { secure }
    var autocorrectionType: UITextAutocorrectionType = .default

    var hasText: Bool { !text.isEmpty }

    func insertText(_ s: String) {
        marked = nil
        Go.send(Event.textInsert(id: id, text: s))
        // Until Go says, the text as it will be.
        let lo = min(selStart, selEnd), hi = max(selStart, selEnd)
        if s != "\n" {
            text.replaceSubrange(lo..<hi, with: Array(s))
            selStart = lo + s.count
            selEnd = selStart
        }
    }

    func deleteBackward() {
        if marked != nil {
            setMarkedText(nil, selectedRange: NSRange(location: 0, length: 0))
            return
        }
        Go.send(Event.textDelete(id: id, before: 1, after: 0))
        let lo = min(selStart, selEnd), hi = max(selStart, selEnd)
        if hi > lo {
            text.removeSubrange(lo..<hi)
            selStart = lo
        } else if lo > 0 {
            text.remove(at: lo - 1)
            selStart = lo - 1
        }
        selEnd = selStart
    }

    // UITextInput: the document is the text around the caret, the marked
    // text standing for the selection while an input method composes.

    private var document: [Character] {
        guard let m = marked else { return text }
        let lo = min(selStart, selEnd), hi = max(selStart, selEnd)
        return Array(text[..<lo]) + Array(m) + Array(text[hi...])
    }

    private func pos(_ i: Int) -> Position { Position(max(0, min(i, document.count))) }

    var selectedTextRange: UITextRange? {
        get {
            if let m = marked {
                let at = min(selStart, selEnd) + m.count
                return Range(pos(at), pos(at))
            }
            return Range(pos(selStart), pos(selEnd))
        }
        set {
            guard let r = newValue as? Range else { return }
            Go.send(Event.textSelect(id: id, start: r.lo.i - selStart, end: r.hi.i - selStart))
        }
    }

    var markedTextRange: UITextRange? {
        guard let m = marked else { return nil }
        let lo = min(selStart, selEnd)
        return Range(pos(lo), pos(lo + m.count))
    }

    var markedTextStyle: [NSAttributedString.Key: Any]?

    func setMarkedText(_ s: String?, selectedRange: NSRange) {
        let m = s ?? ""
        marked = m.isEmpty ? nil : m
        let sel = min(selectedRange.location + selectedRange.length, m.count)
        Go.send(Event.textMarked(id: id, text: m, selStart: min(selectedRange.location, m.count), selEnd: sel))
    }

    func unmarkText() {
        guard let m = marked else { return }
        insertText(m)
    }

    var beginningOfDocument: UITextPosition { pos(0) }
    var endOfDocument: UITextPosition { pos(document.count) }

    func text(in range: UITextRange) -> String? {
        guard let r = range as? Range else { return nil }
        let d = document
        let lo = max(0, min(r.lo.i, d.count)), hi = max(lo, min(r.hi.i, d.count))
        return String(d[lo..<hi])
    }

    func replace(_ range: UITextRange, withText s: String) {
        guard let r = range as? Range else { return }
        let lo = min(selStart, selEnd), hi = max(selStart, selEnd)
        let before = max(0, lo - r.lo.i), after = max(0, r.hi.i - hi)
        if before > 0 || after > 0 { Go.send(Event.textDelete(id: id, before: before, after: after)) }
        if !s.isEmpty { insertText(s) }
    }

    func textRange(from: UITextPosition, to: UITextPosition) -> UITextRange? {
        guard let a = from as? Position, let b = to as? Position else { return nil }
        return Range(a, b)
    }

    func position(from p: UITextPosition, offset: Int) -> UITextPosition? {
        guard let p = p as? Position else { return nil }
        let i = p.i + offset
        return i < 0 || i > document.count ? nil : pos(i)
    }

    func position(from p: UITextPosition, in direction: UITextLayoutDirection, offset: Int) -> UITextPosition? {
        switch direction {
        case .left, .up: return position(from: p, offset: -offset)
        default: return position(from: p, offset: offset)
        }
    }

    func compare(_ a: UITextPosition, to b: UITextPosition) -> ComparisonResult {
        let x = (a as? Position)?.i ?? 0, y = (b as? Position)?.i ?? 0
        return x < y ? .orderedAscending : x > y ? .orderedDescending : .orderedSame
    }

    func offset(from: UITextPosition, to: UITextPosition) -> Int {
        ((to as? Position)?.i ?? 0) - ((from as? Position)?.i ?? 0)
    }

    weak var inputDelegate: UITextInputDelegate?
    lazy var tokenizer: UITextInputTokenizer = UITextInputStringTokenizer(textInput: self)

    func position(within range: UITextRange, farthestIn direction: UITextLayoutDirection) -> UITextPosition? {
        guard let r = range as? Range else { return nil }
        return direction == .left || direction == .up ? r.lo : r.hi
    }

    func characterRange(byExtending p: UITextPosition, in direction: UITextLayoutDirection) -> UITextRange? {
        guard let p = p as? Position else { return nil }
        return direction == .left || direction == .up ? Range(pos(0), p) : Range(p, pos(document.count))
    }

    func baseWritingDirection(for p: UITextPosition, in d: UITextStorageDirection) -> NSWritingDirection { .natural }
    func setBaseWritingDirection(_ d: NSWritingDirection, for r: UITextRange) {}
    func firstRect(for range: UITextRange) -> CGRect { caret }
    func caretRect(for p: UITextPosition) -> CGRect { caret }
    func selectionRects(for range: UITextRange) -> [UITextSelectionRect] { [] }
    func closestPosition(to point: CGPoint) -> UITextPosition? { pos(selEnd) }
    func closestPosition(to point: CGPoint, within range: UITextRange) -> UITextPosition? { (range as? Range)?.hi }
    func characterRange(at point: CGPoint) -> UITextRange? { nil }

    // VoiceOver.

    func accessibilityChanged(focus: Int) {
        elements = nil
        let target = focus != 0 ? accessibilityElements?.first(where: { ($0 as? AccessElement)?.node == focus }) : nil
        UIAccessibility.post(notification: .layoutChanged, argument: target)
    }

    override var isAccessibilityElement: Bool {
        get { false }
        set {}
    }

    override var accessibilityElements: [Any]? {
        get {
            if let e = elements { return e }
            guard let nodes = Go.answer(Event.accessibilityTree(id: id), { AccessibilityTreeReply(&$0).nodes }) else { return [] }
            elements = AccessElement.decode(nodes, in: self)
            return elements
        }
        set {}
    }

    func accessAction(_ node: Int, _ action: String) -> Bool {
        Go.answer(Event.accessibilityAction(id: id, node: node, action: action)) { AccessibilityActionReply(&$0).handled } ?? false
    }

    func showMenu(_ c: ShowMenuCall) {}
}

/// A position in the text being edited, in characters.
private final class Position: UITextPosition {
    let i: Int
    init(_ i: Int) { self.i = i }
}

private final class Range: UITextRange {
    let lo: Position, hi: Position
    init(_ a: Position, _ b: Position) {
        lo = a.i <= b.i ? a : b
        hi = a.i <= b.i ? b : a
    }
    override var start: UITextPosition { lo }
    override var end: UITextPosition { hi }
    override var isEmpty: Bool { lo.i == hi.i }
}

/// An element of native UI, as VoiceOver reads it.
final class AccessElement: UIAccessibilityElement {
    var node = 0
    var actions = 0
    var box = CGRect.zero
    weak var surface: SurfaceWindow?

    // platform.AccessRole and its states, as Go numbers them.
    private enum Role: Int {
        case group = 0, text, button, link, checkBox, radio, toggle, slider, progress, textField, image
        case tab = 18, toggleButton = 31, menuItem = 40, heading = 43
    }

    static func decode(_ b: [UInt8], in surface: SurfaceWindow) -> [AccessElement] {
        var out: [AccessElement] = []
        b.withUnsafeBytes { buf in
            var r = WireReader(buf)
            while r.at < buf.count {
                let e = AccessElement(accessibilityContainer: surface)
                e.surface = surface
                e.node = r.int()
                _ = r.int() // the parent: VoiceOver reads the elements in order
                let role = r.int()
                let label = r.string(), value = r.string(), hint = r.string()
                e.box = CGRect(x: r.float(), y: r.float(), width: r.float(), height: r.float())
                let states = r.int()
                e.actions = r.int()
                let level = r.int()
                e.accessibilityLabel = label
                e.accessibilityValue = value.isEmpty ? nil : value
                e.accessibilityHint = hint.isEmpty ? nil : hint
                e.accessibilityFrameInContainerSpace = e.box
                var traits: UIAccessibilityTraits = []
                switch Role(rawValue: role) {
                case .button, .toggleButton, .menuItem, .tab, .checkBox, .radio, .toggle: traits.insert(.button)
                case .link: traits.insert(.link)
                case .slider: traits.insert(.adjustable)
                case .image: traits.insert(.image)
                case .heading: traits.insert(.header)
                case .textField: traits.insert(.searchField)
                case .text: traits.insert(.staticText)
                default: break
                }
                if role == Role.text.rawValue || role == Role.heading.rawValue { _ = level }
                if states & 2 != 0 { traits.insert(.notEnabled) }
                if states & (4 | 16) != 0 { traits.insert(.selected) }
                e.accessibilityTraits = traits
                // Groups without a name of their own hold others, which
                // VoiceOver reads instead.
                if role == Role.group.rawValue && label.isEmpty && value.isEmpty { continue }
                out.append(e)
            }
        }
        return out
    }

    override func accessibilityActivate() -> Bool { surface?.accessAction(node, "press") ?? false }
    override func accessibilityIncrement() { _ = surface?.accessAction(node, "increment") }
    override func accessibilityDecrement() { _ = surface?.accessAction(node, "decrement") }
}
