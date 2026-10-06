package ui

// PreviewTheme selects a preview's appearance without changing the desktop
// or the application's other windows.
type PreviewTheme string

const (
	PreviewSystem PreviewTheme = "system"
	PreviewLight  PreviewTheme = "light"
	PreviewDark   PreviewTheme = "dark"
)

// PreviewConfig is the environment in which a sample view runs. A zero
// configuration uses the system appearance and preferences, a 640×480 DIP
// viewport and scale 1. It does not change any system setting.
type PreviewConfig struct {
	Theme         PreviewTheme
	Width, Height int
	// Scale is device pixels per DIP, independent of the host display.
	Scale float32
	// Locale is a configuration hook for the sample's own strings and
	// formatting. It does not localize widgets or change the app's locale.
	Locale string
	// Preferences replaces the desktop's preferences for this preview;
	// nil follows the desktop. The entire Preferences value is passed to
	// the engine, including settings added to it in future releases.
	Preferences *Preferences
}

// PreviewSample owns one live sample: its view and an optional cleanup
// function for resources such as subscriptions or background work.
type PreviewSample struct {
	View    func(*Context)
	Dispose func()
}

// PreviewPreset creates fresh sample state whenever it is selected or
// reset. New and Dispose run on the main thread for a window, or on the
// thread driving a Tester. They must return promptly. Config's nonzero
// fields override PreviewOptions.Config when selecting the preset.
type PreviewPreset struct {
	Name   string
	New    func() PreviewSample
	Config PreviewConfig
}

// PreviewOptions describes an interactive playground of sample views.
type PreviewOptions struct {
	Presets []PreviewPreset
	Config  PreviewConfig
	// Configure adds controls after the built-in controls. Edit config in
	// this callback, for example to expose additional Preferences fields
	// or provide a locale picker. It runs in the controls window's view.
	Configure func(c *Context, config *PreviewConfig)
}
