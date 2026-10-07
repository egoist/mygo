package platform

// AccessCollectionProbe is the result of the backends' native accessibility
// test hooks. Values are read through AX, ATK or UIA provider interfaces.
type AccessCollectionProbe struct {
	Rows, Columns                    int
	Row, Column, RowSpan, ColumnSpan int
	Label, Header                    string
	Selected                         int
	Virtualized                      bool
}
