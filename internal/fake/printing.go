package fake

import "github.com/egoist/mygo/internal/platform"

// PrintContent records the fixed pages and returns PrintError, including
// platform.ErrPrintCanceled, without asking for a physical printer.
func (b *Backend) PrintContent(parent platform.Window, job *platform.PrintJob, done func(error)) {
	b.PrintParent, b.PrintJob = parent, job
	done(b.PrintError)
}
