#!/usr/bin/env python3
"""Query the accessibility-text example through a separate AT-SPI client.

Needs python3-pyatspi, an accessibility bus, and a desktop (Xvfb works).
Usage: dbus-run-session -- xvfb-run -a python3 scripts/test-text-atspi.py ./accessibility-text
"""

import subprocess
import os
import sys
import time

import pyatspi
from gi.repository import GLib


def wait_for(description, predicate):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        while GLib.MainContext.default().iteration(False):
            pass
        value = predicate()
        if value:
            return value
        time.sleep(0.02)
    raise AssertionError(f"timed out waiting for {description}")


def walk(node):
    yield node
    for i in range(node.childCount):
        yield from walk(node[i])


def main(binary):
    env = os.environ.copy()
    env.pop("NO_AT_BRIDGE", None)  # headless test images may disable the bridge
    process = subprocess.Popen([binary], env=env)
    events = []
    listener = lambda event: events.append((event.type, event.source.name))
    pyatspi.Registry.registerEventListener(
        listener,
        "object:text-changed",
        "object:text-selection-changed",
        "object:text-caret-moved",
    )
    try:
        desktop = pyatspi.Registry.getDesktop(0)

        def find_app():
            for i in range(desktop.childCount):
                app = desktop[i]
                if app.get_process_id() == process.pid:
                    return app
            return None

        app = wait_for("application on AT-SPI", find_app)

        def fields():
            found = {node.name: node for node in walk(app)}
            return found if "Password" in found else None

        nodes = wait_for("accessible text fields", fields)
        editable = nodes["Editable text"].queryText()
        content = editable.getText(0, -1)
        assert content.startswith("A😀e\u0301 שלום\n"), content
        assert editable.characterCount == len(content)
        assert editable.getCharacterAtOffset(1) == ord("😀")
        first, start, end = editable.getTextAtOffset(0, pyatspi.TEXT_BOUNDARY_LINE_START)
        assert first == "A😀e\u0301 שלום\n" and start == 0 and end == 10
        rect = editable.getRangeExtents(0, 2, pyatspi.DESKTOP_COORDS)
        assert rect[2] > 0 and rect[3] > 0, rect
        assert editable.setCaretOffset(0)
        wait_for("caret at document start", lambda: editable.caretOffset == 0)
        assert editable.addSelection(0, 2)
        wait_for("Unicode selection", lambda: editable.getNSelections() == 1)
        assert editable.getSelection(0) == (0, 2)

        readonly = nodes["Read-only text"].queryText()
        assert readonly.addSelection(0, 2)
        wait_for("read-only selection", lambda: readonly.getNSelections() == 1)
        assert readonly.getText(0, 2) == "A😀"
        assert not nodes["Read-only text"].getState().contains(pyatspi.STATE_EDITABLE)
        rich = nodes["Selectable rich text"].queryText()
        assert "שלום" in rich.getText(0, -1)
        assert rich.addSelection(0, 10)

        password = nodes["Password"].queryText()
        assert password.characterCount == 0 and password.getText(0, -1) == ""
        assert not password.setCaretOffset(1) and not password.addSelection(0, 1)

        nodes["Editable text"].queryEditableText().setTextContents("Changed 😀\nsecond")
        wait_for("text mutation", lambda: editable.getText(0, -1) == "Changed 😀\nsecond")
        wait_for(
            "text and selection events",
            lambda: any(kind.startswith("object:text-changed") and name == "Editable text" for kind, name in events)
            and any(kind == "object:text-selection-changed" for kind, _ in events)
            and any(kind == "object:text-caret-moved" for kind, _ in events),
        )
        assert not any(name == "Password" for _, name in events)
        process.terminate()
        process.wait(timeout=5)
        wait_for("defunct text provider", lambda: nodes["Editable text"].getState().contains(pyatspi.STATE_DEFUNCT))
        print("AT-SPI text, Unicode, ranges, selection, bounds, events, read-only, password and cleanup: PASS")
    finally:
        if process.poll() is None:
            process.terminate()
            process.wait(timeout=5)


if __name__ == "__main__":
    main(sys.argv[1])
