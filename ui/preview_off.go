//go:build mygo_noinspector

package ui

// Content is a user interface for a window, the value of
// mygo.WindowOptions.Content. Create it with View.
type Content struct {
	view func(*Context)
}

// Production builds exclude the playground and its per-window state.
// These hooks inline away, leaving the ordinary rendering path unchanged.
func previewAttach(*Content, *windowHost)            {}
func previewClosed(*engine) bool                     { return false }
func previewPrepareFrame(*engine)                    {}
func previewPrepareSurface(*engine) bool             { return true }
func previewAppearance(_ *engine, dark bool) bool    { return dark }
func previewPreferences(*engine) (Preferences, bool) { return Preferences{}, false }
func previewCloseBegin(*engine) bool                 { return false }
func previewCloseEnd(*engine)                        {}
func (*engine) previewHover()                        {}

// Preview is a developer playground, unavailable in production builds.
type Preview struct{}

const previewDisabled = "ui: previews are unavailable in production builds; use mygo dev or mygo build -debug"

// NewPreview creates a developer playground. Production builds of
// mygo build leave it out by default; this stub fails explicitly if an
// application tries to launch one there.
func NewPreview(PreviewOptions) *Preview       { panic(previewDisabled) }
func (*Preview) Content() *Content             { panic(previewDisabled) }
func (*Preview) NewTester() *Tester            { panic(previewDisabled) }
func (*Preview) Config() PreviewConfig         { panic(previewDisabled) }
func (*Preview) Preset() string                { panic(previewDisabled) }
func (*Preview) SetConfig(PreviewConfig) error { panic(previewDisabled) }
func (*Preview) SelectPreset(string) error     { panic(previewDisabled) }
func (*Preview) Reset()                        { panic(previewDisabled) }
func (*Preview) Controls(*Context)             { panic(previewDisabled) }

// PreviewEnvironment reports no preview environment in production builds.
func PreviewEnvironment(*Context) (PreviewConfig, bool) { return PreviewConfig{}, false }
