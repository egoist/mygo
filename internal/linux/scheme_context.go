package linux

import "context"

// Each webview owns a parent context on the main thread. Finishing a child
// removes it from the parent, and closing the view cancels all live children.
type schemeRequests struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *schemeRequests) start() (context.Context, context.CancelFunc) {
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}
	return context.WithCancel(s.ctx)
}

func (s *schemeRequests) close() {
	if s.cancel != nil {
		s.cancel()
	}
}
