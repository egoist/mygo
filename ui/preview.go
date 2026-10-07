//go:build !mygo_noinspector

package ui

import (
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"
)

// Preview coordinates sample and environment selection. Its controller
// methods are safe from any goroutine. Content and Controls are ordinary
// native UI; NewTester drives the same sample with the headless UI tester.
// Each content window or tester owns independent sample state.
type Preview struct {
	mu        sync.Mutex
	presets   []PreviewPreset
	base      PreviewConfig
	configure func(*Context, *PreviewConfig)
	selected  int
	config    PreviewConfig
	// generation changes only for sample selection/reset; environment
	// changes preserve the sample and its element state.
	generation uint64
	watchers   map[*engine]func()
}

// NewPreview creates a playground, initially showing its first preset.
// It panics for an empty list, duplicate/empty names, nil factories or
// invalid configurations. Sample factories wait until the first frame.
func NewPreview(options PreviewOptions) *Preview {
	base, err := normalizePreviewConfig(options.Config)
	if err != nil {
		panic(err)
	}
	if len(options.Presets) == 0 {
		panic("ui: a preview needs at least one preset")
	}
	p := &Preview{base: base, configure: options.Configure, generation: 1,
		presets: append([]PreviewPreset(nil), options.Presets...), watchers: map[*engine]func(){}}
	names := map[string]bool{}
	for i := range p.presets {
		s := &p.presets[i]
		if s.Name == "" || names[s.Name] || s.New == nil {
			panic("ui: preview presets need unique nonempty names and a New function")
		}
		names[s.Name] = true
		s.Config = clonePreviewConfig(s.Config)
		if _, err := normalizePreviewConfig(mergePreviewConfig(base, s.Config)); err != nil {
			panic(fmt.Errorf("ui: preview preset %q: %w", s.Name, err))
		}
	}
	p.config, _ = normalizePreviewConfig(mergePreviewConfig(base, p.presets[0].Config))
	return p
}

// Content returns content for mygo.WindowOptions.Content. Enable the
// window's developer tools to use the existing inspector (F12).
// The viewport fits inside the host window with its aspect ratio intact;
// resizing the host window changes the fit, not the configured viewport.
func (p *Preview) Content() *Content { return &Content{attach: p.attachWindow} }

// NewTester starts an independent sample in the current configuration.
// Controller changes take effect at the tester's next Frame. Image has
// Width×Scale by Height×Scale pixels. Close it when finished.
func (p *Preview) NewTester() *Tester {
	c := p.Config()
	return newTester(nil, c.Width, c.Height, func(rt *engine) {
		p.attachEngine(rt)
		rt.insp.enabled = true
	})
}

// Config returns a copy of the configuration, including its Preferences.
func (p *Preview) Config() PreviewConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return clonePreviewConfig(p.config)
}

// Preset returns the selected preset's name.
func (p *Preview) Preset() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.presets[p.selected].Name
}

// SetConfig replaces the environment, preserving live sample state. Zero
// fields get PreviewConfig's defaults; nil Preferences restores the system
// settings. Invalid configurations leave the preview unchanged.
func (p *Preview) SetConfig(config PreviewConfig) error {
	c, err := normalizePreviewConfig(config)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if reflect.DeepEqual(p.config, c) {
		p.mu.Unlock()
		return nil
	}
	p.config = c
	wake := p.wakes()
	p.mu.Unlock()
	wakePreviews(wake)
	return nil
}

// SelectPreset selects a preset, restores its configuration over the
// playground's defaults and creates fresh state at each host's next frame.
// Selecting the same preset resets it too. Unknown names change nothing.
func (p *Preview) SelectPreset(name string) error {
	p.mu.Lock()
	for i, preset := range p.presets {
		if preset.Name != name {
			continue
		}
		p.selected = i
		p.config, _ = normalizePreviewConfig(mergePreviewConfig(p.base, preset.Config))
		p.generation++
		wake := p.wakes()
		p.mu.Unlock()
		wakePreviews(wake)
		return nil
	}
	p.mu.Unlock()
	return fmt.Errorf("ui: no preview preset %q", name)
}

// Reset creates fresh sample state in the current environment at the next
// frame. It also resets focus, editors, scroll offsets and other element
// state belonging to the sample.
func (p *Preview) Reset() {
	p.mu.Lock()
	p.generation++
	wake := p.wakes()
	p.mu.Unlock()
	wakePreviews(wake)
}

// PreviewEnvironment returns the sample's environment and true in a preview,
// or a zero value and false in an ordinary view. Use Locale here to select
// your sample's strings or formatting; Size, Theme and Preferences already
// report the configured environment.
func PreviewEnvironment(c *Context) (PreviewConfig, bool) {
	if c.rt.insp.preview.config == nil {
		return PreviewConfig{}, false
	}
	return clonePreviewConfig(*c.rt.insp.preview.config), true
}

func clonePreviewConfig(c PreviewConfig) PreviewConfig {
	if c.Preferences != nil {
		v := *c.Preferences
		c.Preferences = &v
	}
	return c
}

func normalizePreviewConfig(c PreviewConfig) (PreviewConfig, error) {
	c = clonePreviewConfig(c)
	if c.Theme == "" {
		c.Theme = PreviewSystem
	}
	if c.Width == 0 {
		c.Width = 640
	}
	if c.Height == 0 {
		c.Height = 480
	}
	if c.Scale == 0 {
		c.Scale = 1
	}
	if c.Theme != PreviewSystem && c.Theme != PreviewLight && c.Theme != PreviewDark {
		return c, fmt.Errorf("ui: invalid preview theme %q", c.Theme)
	}
	if c.Width < 1 || c.Height < 1 || c.Width > 16384 || c.Height > 16384 ||
		math.IsNaN(float64(c.Scale)) || math.IsInf(float64(c.Scale), 0) || c.Scale < 0.25 || c.Scale > 8 ||
		math.Ceil(float64(c.Width)*float64(c.Scale)) > 16384 || math.Ceil(float64(c.Height)*float64(c.Scale)) > 16384 ||
		math.Ceil(float64(c.Width)*float64(c.Scale))*math.Ceil(float64(c.Height)*float64(c.Scale)) > 32_000_000 {
		return c, fmt.Errorf("ui: preview needs a positive viewport, scale 0.25–8, at most 16384 pixels per side and 32 million pixels")
	}
	if c.Preferences != nil {
		s := float64(c.Preferences.TextScale)
		if math.IsNaN(s) || math.IsInf(s, 0) || s < 0 || s > 8 {
			return c, fmt.Errorf("ui: preview text scale must be between 0 and 8 (0 means 1)")
		}
	}
	return c, nil
}

func mergePreviewConfig(base, override PreviewConfig) PreviewConfig {
	if override.Theme != "" {
		base.Theme = override.Theme
	}
	if override.Width != 0 {
		base.Width = override.Width
	}
	if override.Height != 0 {
		base.Height = override.Height
	}
	if override.Scale != 0 {
		base.Scale = override.Scale
	}
	if override.Locale != "" {
		base.Locale = override.Locale
	}
	if override.Preferences != nil {
		base.Preferences = override.Preferences
	}
	return clonePreviewConfig(base)
}

// wakes is called with mu held; native invalidation itself is safe from
// any goroutine and never runs sample code on the caller's thread.
func (p *Preview) wakes() []func() {
	wake := make([]func(), 0, len(p.watchers))
	for _, fn := range p.watchers {
		wake = append(wake, fn)
	}
	return wake
}

func wakePreviews(wake []func()) {
	for _, fn := range wake {
		fn()
	}
}

func (p *Preview) watch(rt *engine) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.watchers[rt]; ok || rt.insp.preview.closed {
		return
	}
	p.watchers[rt] = rt.host.invalidate
	rt.insp.preview.onClose = append(rt.insp.preview.onClose, func() {
		p.mu.Lock()
		delete(p.watchers, rt)
		p.mu.Unlock()
	})
}

type previewSession struct {
	p          *Preview
	rt         *engine
	config     PreviewConfig
	generation uint64
	sample     PreviewSample
}

func (p *Preview) attachEngine(rt *engine) {
	s := &previewSession{p: p, rt: rt}
	rt.insp.preview.beforeFrame = s.prepare
	rt.view = s.build
	rt.insp.preview.onClose = append(rt.insp.preview.onClose, s.dispose)
	p.watch(rt)
}

func (s *previewSession) prepare() {
	p := s.p
	p.mu.Lock()
	c, generation, factory := clonePreviewConfig(p.config), p.generation, p.presets[p.selected].New
	p.mu.Unlock()
	if h, ok := s.rt.host.(*headless); ok {
		h.w, h.h, h.scale = float32(c.Width), float32(c.Height), c.Scale
	}
	if !reflect.DeepEqual(s.config, c) {
		s.config = c
		s.rt.insp.preview.config = &s.config
		s.rt.darkKnown, s.rt.prefsKnown, s.rt.themeOK, s.rt.redraw = false, false, false, false
	}
	if s.generation != generation {
		s.dispose()
		if s.rt.insp.preview.closed {
			return
		}
		if s.generation != 0 {
			s.resetRuntime()
		}
		s.generation = generation
		s.sample = factory()
		if s.rt.insp.preview.closed {
			s.dispose()
			return
		}
		if s.sample.View == nil {
			s.dispose()
			panic("ui: a preview preset returned a nil View")
		}
		s.rt.redraw = false
	}
}

// resetRuntime drops the previous sample's full interaction state,
// including overlays, toasts, exit drawings and pending timers. Keep the
// inspector, accessibility subscription, IME state and host connection;
// none belongs to the sample. The old IME state lets the next frame turn
// input methods off if the new sample has no focused editor.
func (s *previewSession) resetRuntime() {
	rt := s.rt
	if rt.timer != nil {
		rt.timer.Stop()
		rt.timer = nil
	}
	if rt.repaintTimer != nil {
		rt.repaintTimer.Stop()
		rt.repaintTimer = nil
	}
	rt.wakeMu.Lock()
	rt.wakeAt = time.Time{}
	rt.wakeMu.Unlock()
	clear(rt.states)
	rt.texts, rt.selection = nil, textSelection{}
	rt.free = nil
	rt.c = Context{rt: rt}
	rt.hits, rt.focusOrder, rt.focusScopes = nil, nil, nil
	rt.focused, rt.modal, rt.modalLayer = 0, 0, 0
	rt.focusVisible, rt.pressed, rt.drag = false, nil, nil
	rt.hover, rt.chain, rt.downs, rt.clickLater, rt.keys = nil, nil, nil, nil, nil
	rt.regs, rt.nextRegs, rt.delivered = nil, nil, nil
	clear(rt.groups)
	clear(rt.memberOf)
	clear(rt.groupLast)
	clear(rt.openers)
	clear(rt.kept)
	clear(rt.trans)
	clear(rt.byID)
	clear(rt.dupKeys)
	rt.exitsBuilt, rt.redraw, rt.held = false, false, false
	rt.repaintAt, rt.repaintDue = time.Time{}, time.Time{}
	rt.menu, rt.tips, rt.scrollDrag = menuState{}, tooltips{}, scrollDrag{}
	rt.toasts, rt.toastList, rt.announcements, rt.labels, rt.warnings = nil, nil, nil, nil, nil
	rt.toastFrame, rt.toastsPaused, rt.dropOver = 0, false, 0
	rt.paths, rt.svgs = paths{}, svgs{}
	rt.insp.changed = true
}

func (s *previewSession) build(c *Context) {
	// A new key gives new element state; changing only configuration keeps
	// the key, so live sample edits, focus and scrolling survive it.
	Box(c).Key(s.generation).Fill().Children(func() { s.sample.View(c) })
}

func (s *previewSession) dispose() {
	// Invalidate native text-client references before disposing the sample
	// they call into, including clients still holding marked text.
	for _, state := range s.rt.states {
		if state.textAdapter != nil {
			state.textAdapter.release()
			state.textAdapter = nil
		}
	}
	fn := s.sample.Dispose
	s.sample = PreviewSample{}
	if fn != nil {
		fn()
	}
}

// previewHover refreshes hit testing after a virtual viewport moves or
// relays out. A second build lets custom views that read Hovered follow it
// too; native input handlers receive no synthetic pointer events.
func (rt *engine) previewHover() {
	if rt.insp.preview.config == nil || !rt.pointerIn || rt.pressed != nil {
		return
	}
	old, chain := rt.hover, rt.appendHitChain(rt.chain[:0], rt.pointerX, rt.pointerY)
	if rt.setHover(chain) {
		rt.chain = old[:0]
		rt.host.post(rt.changed)
	} else {
		rt.chain = chain[:0]
	}
}
