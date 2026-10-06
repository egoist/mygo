// Query the accessibility-text example from a separate macOS AX client.
// Usage: swift scripts/test-text-ax.swift /absolute/path/to/accessibility-text
// The invoking terminal needs Accessibility permission. This test does not
// request permission or change the user's screen-reader settings.
import Foundation
import ApplicationServices

enum TestError: Error { case failure(String) }

func check(_ ok: Bool, _ message: String) throws {
    if !ok { throw TestError.failure(message) }
}

func attribute(_ node: AXUIElement, _ name: String) -> CFTypeRef? {
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(node, name as CFString, &value) == .success else { return nil }
    return value
}

func parameter(_ node: AXUIElement, _ name: String, _ argument: CFTypeRef) -> CFTypeRef? {
    var value: CFTypeRef?
    guard AXUIElementCopyParameterizedAttributeValue(node, name as CFString, argument, &value) == .success else { return nil }
    return value
}

func rangeValue(_ location: Int, _ length: Int) -> AXValue {
    var range = CFRange(location: location, length: length)
    return AXValueCreate(.cfRange, &range)!
}

func range(_ value: CFTypeRef?) -> CFRange {
    var result = CFRange()
    if let value = value, CFGetTypeID(value) == AXValueGetTypeID() {
        AXValueGetValue(value as! AXValue, .cfRange, &result)
    }
    return result
}

func find(_ node: AXUIElement, _ label: String) -> AXUIElement? {
    if (attribute(node, kAXDescriptionAttribute) as? String) == label ||
       (attribute(node, kAXTitleAttribute) as? String) == label { return node }
    for child in (attribute(node, kAXChildrenAttribute) as? [AXUIElement]) ?? [] {
        if let found = find(child, label) { return found }
    }
    return nil
}

func wait(_ description: String, _ predicate: () -> Bool) throws {
    let deadline = Date().addingTimeInterval(10)
    while Date() < deadline {
        if predicate() { return }
        CFRunLoopRunInMode(.defaultMode, 0.02, false)
    }
    throw TestError.failure("timed out waiting for \(description)")
}

final class Events { var names: [String] = [] }
func onNotification(_ observer: AXObserver, _ element: AXUIElement, _ name: CFString, _ context: UnsafeMutableRawPointer?) {
    if let context = context { Unmanaged<Events>.fromOpaque(context).takeUnretainedValue().names.append(name as String) }
}

func main() throws {
    guard AXIsProcessTrusted() else {
        print("SKIP: invoking terminal has no macOS Accessibility permission")
        return
    }
    let app = Process()
    app.executableURL = URL(fileURLWithPath: CommandLine.arguments[1])
    try app.run()
    defer { if app.isRunning { app.terminate(); app.waitUntilExit() } }
    let root = AXUIElementCreateApplication(app.processIdentifier)
    try wait("text window") { find(root, "Password") != nil }
    let editor = find(root, "Editable text")!
    let readonly = find(root, "Read-only text")!
    let rich = find(root, "Selectable rich text")!
    let password = find(root, "Password")!
    let count = (attribute(editor, kAXNumberOfCharactersAttribute) as? NSNumber)!.intValue
    let text = parameter(editor, kAXStringForRangeParameterizedAttribute, rangeValue(0, count)) as? String
    try check(text?.hasPrefix("A😀e\u{0301} שלום\n") == true, "UTF-16 document range")
    try check((text! as NSString).length == count, "character count")
    let character = range(parameter(editor, kAXRangeForIndexParameterizedAttribute, NSNumber(value: 2)))
    try check(character.location == 1 && character.length == 2, "surrogate boundary")
    let firstLine = parameter(editor, kAXRangeForLineParameterizedAttribute, NSNumber(value: 0))!
    try check((parameter(editor, kAXStringForRangeParameterizedAttribute, firstLine) as? String) == "A😀e\u{0301} שלום\n", "first visual line")
    let boundsValue = parameter(editor, kAXBoundsForRangeParameterizedAttribute, rangeValue(0, 3))!
    var bounds = CGRect()
    AXValueGetValue(boundsValue as! AXValue, .cgRect, &bounds)
    try check(bounds.width > 0 && bounds.height > 0, "range bounds")
    var point = CGPoint(x: bounds.minX + 0.5, y: bounds.midY)
    let pointRange = range(parameter(editor, kAXRangeForPositionParameterizedAttribute, AXValueCreate(.cgPoint, &point)!))
    try check(pointRange.location == 0 && pointRange.length == 0, "range from screen point")
    try check(range(attribute(editor, kAXVisibleCharacterRangeAttribute)).length > 0, "visible range")
    let events = Events()
    var observer: AXObserver?
    try check(AXObserverCreate(app.processIdentifier, onNotification, &observer) == .success, "AXObserver")
    let obs = observer!
    let context = Unmanaged.passUnretained(events).toOpaque()
    for name in [kAXValueChangedNotification, kAXSelectedTextChangedNotification] {
        try check(AXObserverAddNotification(obs, editor, name as CFString, context) == .success, "notification \(name)")
    }
    CFRunLoopAddSource(CFRunLoopGetCurrent(), AXObserverGetRunLoopSource(obs), .defaultMode)
    defer { CFRunLoopRemoveSource(CFRunLoopGetCurrent(), AXObserverGetRunLoopSource(obs), .defaultMode) }
    for node in [editor, readonly] {
        try check(AXUIElementSetAttributeValue(node, kAXSelectedTextRangeAttribute as CFString, rangeValue(0, 3)) == .success, "selection setter")
        try wait("Unicode selection") { (attribute(node, kAXSelectedTextAttribute) as? String) == "A😀" }
    }
    var settable = DarwinBoolean(false)
    try check(AXUIElementIsAttributeSettable(readonly, kAXValueAttribute as CFString, &settable) == .success && !settable.boolValue, "read-only value")
    let richCount = (attribute(rich, kAXNumberOfCharactersAttribute) as? NSNumber)!.intValue
    try check((parameter(rich, kAXStringForRangeParameterizedAttribute, rangeValue(0, richCount)) as? String)?.contains("שלום") == true, "rich text")
    try check(parameter(password, kAXStringForRangeParameterizedAttribute, rangeValue(0, 100)) == nil, "password ranges")
    try check(AXUIElementSetAttributeValue(editor, kAXValueAttribute as CFString, "Changed 😀\nsecond" as CFString) == .success, "text setter")
    try wait("text changed") { (attribute(editor, kAXValueAttribute) as? String) == "Changed 😀\nsecond" }
    try wait("AX text and selection notifications") { events.names.contains(kAXValueChangedNotification) && events.names.contains(kAXSelectedTextChangedNotification) }
    app.terminate()
    app.waitUntilExit()
    try check(attribute(editor, kAXValueAttribute) == nil, "defunct provider")
    print("macOS AX text, UTF-16, ranges, selection, bounds, notifications, read-only, password and cleanup: PASS")
}

do { try main() } catch { fputs("\(error)\n", stderr); exit(1) }
