package mygo

// ConsoleLevel is the level of a message a page logged to its console,
// as Page.OnConsoleMessage tells it: ConsoleDebug, ConsoleLog,
// ConsoleInfo, ConsoleWarn or ConsoleError.
type ConsoleLevel string

// The levels of the console, as the page's console methods: debug, log,
// info, warn and error. An error the page failed to catch is
// ConsoleError, whatever it threw.
const (
	ConsoleDebug ConsoleLevel = "debug"
	ConsoleLog   ConsoleLevel = "log"
	ConsoleInfo  ConsoleLevel = "info"
	ConsoleWarn  ConsoleLevel = "warn"
	ConsoleError ConsoleLevel = "error"
)

// ConsoleMessage is one message a page logged to its console, or an error
// it failed to catch, as Page.OnConsoleMessage tells it.
type ConsoleMessage struct {
	// Level is the console method's level, debug, log, info, warn or
	// error; an uncaught error and an unhandled rejection are error.
	Level ConsoleLevel
	// Text is the message as the inspector would show it: the arguments
	// formatted the way the page's console formats them, cut at a length,
	// so that a page logging large values in a loop cannot flood the app.
	// An error's stack follows its message when there is one.
	Text string
	// URL is the URL of the script that logged, and Line the line in it,
	// both empty or 0 when the page's engine did not tell them: uncaught
	// errors carry where they were thrown, console messages usually the
	// line that logged.
	URL  string
	Line int
}

// OnConsoleMessage is called with what the page logs to its console, at
// any level (console.log, console.info, console.warn, console.error,
// console.debug), and with the errors it fails to catch: uncaught errors
// and unhandled promise rejections, as ConsoleError, whose Text has the
// stack when the page's engine has one. It is called on the main thread,
// as other events are, with the messages of the page's main frame, of
// every page the window shows, whatever its origin and whether it is
// trusted.
//
//	w := mygo.NewWindow(mygo.WindowOptions{URL: "/"})
//	w.Page().OnConsoleMessage(func(m *mygo.ConsoleMessage) {
//		log.Printf("[%s] %s (%s:%d)", m.Level, m.Text, m.URL, m.Line)
//	})
//
// Listening is opt-in, so that pages nobody listens to pay nothing: the
// first listener makes the page's world wrap the console and listen for
// errors, which the page's own console is unchanged by, and the last
// listener to go unwraps it again.
func (p *Page) OnConsoleMessage(fn func(m *ConsoleMessage)) func() {
	remove := p.w.onConsoleMessage.add(fn, false)
	p.w.consoleCaptureChanged()
	return func() {
		remove()
		p.w.consoleCaptureChanged()
	}
}
