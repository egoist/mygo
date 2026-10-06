"""Optional external AT-SPI collection probe; requires python3-pyatspi."""
import sys
import time

import pyatspi

label, row_count, column_count, row, column = sys.argv[1:]
row_count, column_count, row, column = map(int, (row_count, column_count, row, column))


def find(accessible, depth=0):
    if accessible.name == label:
        return accessible
    if depth >= 6:
        return None
    # Inspect only window/container children, never enumerate a collection.
    for i in range(min(accessible.childCount, 30)):
        result = find(accessible[i], depth + 1)
        if result:
            return result
    return None


collection = None
for _ in range(50):
    collection = find(pyatspi.Registry.getDesktop(0))
    if collection:
        break
    time.sleep(0.1)
assert collection, "collection missing from AT-SPI; unset NO_AT_BRIDGE"
table = collection.queryTable()
assert (table.nRows, table.nColumns) == (row_count, column_count)
cell = table.getAccessibleAt(row, column)
info = cell.queryTableCell()
span = info.getRowColumnSpan()
assert (span.row, span.column, span.row_span, span.column_span) == (row, column, 1, 1)
assert info.table == collection
headers = info.columnHeaderCells
assert len(headers) == 1 and headers[0].name == "Size"
assert cell.queryComponent().scrollTo(pyatspi.SCROLL_ANYWHERE)
for _ in range(50):
    if table.getAccessibleAt(row, column).name == f"Cell {row} {column}":
        break
    time.sleep(0.1)
assert table.getAccessibleAt(row, column).name == f"Cell {row} {column}"
selection = collection.querySelection()
assert selection.selectChild(4)  # data row 3, after the header
assert selection.selectChild(row + 1)
for _ in range(50):
    if selection.nSelectedChildren == 2:
        break
    time.sleep(0.1)
assert selection.nSelectedChildren == 2
print("External AT-SPI counts, coordinates, headers, realization and multiselection passed")
