# Interactive previews

A preview runs ordinary Go views with fresh sample state. Its controls
switch samples, appearance, viewport size in DIPs, display scale, locale
configuration and accessibility preferences while you interact with the
view. Run the example with no frontend tooling:

```sh
go run ./examples/preview
```

The example opens a preview window and a controls window. Empty,
populated, Japanese and long-content presets exercise a contact form.
Press `F12` in the preview to open the existing [inspector](inspector.md).
Close either window to end the example.

## Create a playground

Each preset factory owns its sample state. Return your ordinary view
method and an optional `Dispose` callback for any resources it started:

```go
p := ui.NewPreview(ui.PreviewOptions{
    Config: ui.PreviewConfig{Width: 520, Height: 440, Locale: "en-US"},
    Presets: []ui.PreviewPreset{
        {Name: "Empty", New: func() ui.PreviewSample {
            model := &Contact{}
            return ui.PreviewSample{View: model.View}
        }},
        {Name: "Populated", New: func() ui.PreviewSample {
            model := &Contact{Name: "Ada Lovelace"}
            return ui.PreviewSample{View: model.View}
        }, Config: ui.PreviewConfig{Theme: ui.PreviewDark, Scale: 2}},
    },
})
mygo.App.WhenReady(func() {
    mygo.NewWindow(mygo.WindowOptions{
        Title: "Contact preview", Width: 1000, Height: 720,
        Content: p.Content(),
        Page: mygo.PageOptions{DevTools: mygo.DevToolsEnabled},
    })
    mygo.NewWindow(mygo.WindowOptions{
        Title: "Preview controls", Width: 460, Height: 760,
        Content: ui.View(p.Controls),
    })
})
// Run mygo.App.Run() on the main goroutine as usual.
```

Factories run at the first frame and again when selecting or resetting
the sample. Every attached preview window or tester owns independent
state. `Dispose` runs once when replacing the sample or closing its host,
on the same thread that builds its view. Stop background work and remove
subscriptions there; factories and disposal must return promptly.

`SelectPreset(name)` restores the playground's base configuration with
the preset's nonzero overrides. An explicit `PreviewSystem` overrides a
base light/dark appearance; empty fields inherit the base. Selecting the
same preset resets it. `Reset()` creates fresh sample and element state
while keeping the current configuration. Changing only configuration
preserves edits, focus and scrolling:

```go
config := p.Config() // a copy, including Preferences
config.Theme, config.Scale = ui.PreviewLight, 1.25
config.Preferences = &ui.Preferences{
    ReduceMotion: true, HighContrast: true, TextScale: 1.5,
}
if err := p.SetConfig(config); err != nil { /* report invalid configuration */ }
```

The controller methods are safe from any goroutine. Changes coalesce at
each host's next frame; they never run sample code on the caller's
goroutine. In headless tests, call `Frame()` after controller changes.
State used by a live sample still follows the ordinary `Window.Update`
threading rules. Configuration starts at 640×480 DIPs, scale 1, and the
system theme and preferences. `SetConfig` replaces the configuration;
its zero fields get these defaults, and nil `Preferences` restores the
desktop settings. Invalid values leave the previous configuration intact.
Viewports must be positive, scale between 0.25 and 8, and the rendered
image at most 16384 pixels per side and 32 million pixels in total.

## Locale and preference hooks

`Context.Size`, `Theme` and `Preferences` report the simulated environment.
`Context.PreviewConfig()` also gives its configuration and a boolean
telling whether the view runs in a preview. Use its `Locale` to select
your own sample strings or formatting. It does not localize the built-in
widgets, mirror layout, or change `mygo.App.Locale()`.

`PreviewOptions.Configure` appends application-specific controls. Edit
the supplied configuration; the controls view validates and applies it.
For example, expose a custom accent without maintaining another
preference model:

```go
Configure: func(c *ui.Context, config *ui.PreviewConfig) {
    if ui.Button(c, "Purple accent").Clicked() {
        prefs := c.Preferences()
        if config.Preferences != nil { prefs = *config.Preferences }
        prefs.Accent = ui.Hex("#9333ea")
        config.Preferences = &prefs
    }
},
```

The engine receives the entire `ui.Preferences` value. Additional OS
settings can be exposed here as they become available in that type.
Overrides affect this preview alone; the controls window and desktop
keep their own settings. For presets, non-nil preferences replace the
whole value, including false/zero fields.

## Rendering and tests

The configured viewport fits inside the native window with its aspect
ratio intact. Resizing that window changes the fit; the Width/Height
controls change the simulated layout. The inspector shares this virtual
viewport and leaves the sample less width, as in ordinary native UI.
Pointer input, scroll distances, IME carets, context menus and accessible
bounds are mapped between the fitted window and the viewport.

The sample uses the existing UI engine and raster renderer at the
selected scale, then the ordinary native renderer presents the image.
This permits simulated DPI on any host display and makes headless pixels
match captures. It previews sRGB colors and CPU effect implementations;
it does not reproduce another OS's font rendering or exercise an
effect's GPU shader. `CapturePage` captures the configured viewport at
the selected scale, including an open inspector, rather than the host
window's surrounding margins.

```go
tt := p.NewTester()
defer tt.Close()
if err := p.SelectPreset("Populated"); err != nil { t.Fatal(err) }
tt.Frame()
if err := tt.Click("Save"); err != nil { t.Fatal(err) }
if !tt.HasText("Saved") { t.Fatal("sample did not save") }
img := tt.Image() // ceil(Width*Scale) × ceil(Height*Scale)
```

`NewTester` uses the existing [Tester](testing.md), including its input,
accessibility tree and inspector. `SetDark` and `SetPreferences` change
the test host's settings, which previews follow while their overrides
are unset. Viewport changes go through `p.SetConfig`; `Tester.SetSize`
does not change a preview's fixed viewport. `Close` stops its timers,
disposes the sample, and removes its controller subscription. The
inspector remains unavailable in `mygo_noinspector` builds.
