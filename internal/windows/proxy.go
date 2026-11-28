//go:build windows && (amd64 || arm64) && !mygo_nowebview

package windows

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/sessionproxy"
)

type proxyDrain struct {
	b           *Backend
	pending     map[string]func()
	quitPending bool
}

var (
	proxyDrains map[*Backend]*proxyDrain
	proxyStates map[*dataStore]*proxyState
)

func drainOf(b *Backend) *proxyDrain {
	if proxyDrains == nil {
		proxyDrains = map[*Backend]*proxyDrain{}
	}
	d := proxyDrains[b]
	if d == nil {
		d = &proxyDrain{b: b, pending: map[string]func(){}}
		proxyDrains[b] = d
		proxyQuitHook = drainProxyBackend
	}
	return d
}

func drainProxyBackend(b *Backend, expired bool) bool {
	d := proxyDrains[b]
	if d == nil {
		return false
	}
	return d.drain(expired)
}

// Keep the STA pumping for BrowserProcessExited. The fallback must run
// before the application's three-second stop grace period.
func (d *proxyDrain) drain(expired bool) bool {
	b := d.b
	if !expired && !d.quitPending {
		for _, s := range slices.Clone(b.stores) {
			if s.private && s.proxied {
				s.Close()
				for r := range s.cookies.requests {
					r.finish(nil, errSessionClosed)
				}
				s.cookies.requests = nil
				b.closeCookieView(&s.cookies)
				b.closeCookieViewOf(&s.cookies)
			}
		}
		b.dropStores()
	}
	if len(d.pending) == 0 {
		delete(proxyDrains, b)
		return false
	}
	if !expired {
		if d.quitPending {
			return true
		}
		d.quitPending = true
		if id, _, _ := procSetTimer.Call(b.appHwnd, timerProxyQuit, 1500, 0); id != 0 {
			return true
		}
	}
	for _, cleanup := range d.pending {
		cleanup()
	}
	return true
}

func (d *proxyDrain) remove(folder string) {
	delete(d.pending, folder)
	if err := os.RemoveAll(folder); err != nil {
		log.Print(err)
	}
	if d.quitPending && len(d.pending) == 0 {
		delete(proxyDrains, d.b)
		procKillTimer.Call(d.b.appHwnd, timerProxyQuit)
		procPostQuitMessage.Call(0)
	}
}

type proxyState struct {
	store             *dataStore
	config            *platform.SessionProxyConfig
	envPtr            uintptr
	envStarting       bool
	browserExited     bool
	startedController bool
	cleanup           func()
	waiters           []func(uintptr)
	drain             *proxyDrain
}

// NewProxyDataStore opens a session with explicit proxy settings.
func NewProxyDataStore(app platform.AppController, opts platform.DataStoreOptions) (platform.DataStore, error) {
	a, ok := app.(appController)
	if !ok {
		return nil, platform.ErrUnsupported
	}
	if err := proxyOverrides(); err != nil {
		return nil, err
	}
	store, err := a.NewDataStore(opts)
	if err == nil {
		s := store.(*dataStore)
		p := &proxyState{store: s, config: opts.Proxy}
		if opts.Ephemeral {
			p.drain = drainOf(a.b)
		}
		s.proxied = true
		if proxyStates == nil {
			proxyStates = map[*dataStore]*proxyState{}
		}
		proxyStates[s] = p
		proxyEnvHook = proxyStoreEnv
		proxyCloseHook = proxyStoreClose
		proxyControllerHook = proxyStoreCreateController
		proxySetupAuthHook = proxyStoreSetupAuth
	}
	return store, err
}

func proxyStoreEnv(s *dataStore) uintptr {
	if p := proxyStates[s]; p != nil {
		return p.envPtr
	}
	return 0
}

func proxyStoreClose(s *dataStore) {
	p := proxyStates[s]
	if p == nil {
		return
	}
	p.close()
	if p.cleanup == nil && !p.envStarting {
		delete(proxyStates, s)
	}
}

func proxyStoreCreateController(s *dataStore, hwnd uintptr, composition bool, handler uintptr) uintptr {
	if p := proxyStates[s]; p != nil {
		return p.createController(hwnd, composition, handler)
	}
	return eFail
}

func proxyStoreSetupAuth(s *dataStore, w *window, ready func()) {
	if p := proxyStates[s]; p != nil {
		p.setupAuth(w, ready)
		return
	}
	ready()
}

func (p *proxyState) close() {
	if p.envPtr != 0 {
		release(p.envPtr)
		p.envPtr = 0
	}
	if (!p.startedController || p.browserExited) && p.cleanup != nil {
		p.cleanup()
	}
}

func (p *proxyState) createController(hwnd uintptr, composition bool, handler uintptr) uintptr {
	s := p.store
	b := s.b
	if s.closed {
		return eFail
	}
	if p.envPtr != 0 {
		return p.requestController(p.envPtr, hwnd, composition, handler)
	}
	addRef(handler)
	p.waiters = append(p.waiters, func(env uintptr) {
		defer release(handler)
		hr := uintptr(eFail)
		if env != 0 && !s.closed {
			hr = p.requestController(env, hwnd, composition, handler)
		}
		if failed(hr) {
			comCall(handler, 3, hr, 0)
		}
	})
	if p.envStarting {
		return sOK
	}
	p.envStarting = true
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	root := filepath.Join(dir, sanitize(b.name), "WebView2Proxy")
	userData := filepath.Join(root, s.profile)
	disposable := ""
	done := func(env uintptr, err error) {
		p.envStarting = false
		if s.closed && env != 0 {
			release(env)
			env = 0
		}
		if disposable != "" {
			if env == 0 {
				p.drain.remove(disposable)
			} else if err = watchProxyExit(env, p, disposable); err != nil {
				release(env)
				env = 0
				p.drain.remove(disposable)
			}
		}
		if err != nil {
			log.Print(err)
		}
		p.envPtr = env
		waiters := p.waiters
		p.waiters = nil
		for _, fn := range waiters {
			fn(env)
		}
		if s.closed && p.cleanup == nil {
			delete(proxyStates, s)
		}
	}
	args := "--proxy-server=" + p.config.HTTPProxy + " --proxy-bypass-list=<-loopback> --disable-quic --force-webrtc-ip-handling-policy=disable_non_proxied_udp"
	if extra := browserArguments(b.envUnthrottled, b.envAutoplay); extra != "" {
		args += " " + extra
	}
	language := ""
	if len(b.languages) > 0 {
		language = b.languages[0]
	}
	if s.private {
		if err := os.MkdirAll(root, 0700); err != nil {
			done(0, err)
			return sOK
		}
		folder, err := os.MkdirTemp(root, "mygo-private-")
		if err != nil {
			done(0, err)
			return sOK
		}
		disposable, userData = folder, folder
		p.drain.pending[folder] = func() { p.drain.remove(folder) }
	}
	loader := loaderProc()
	if err := proxyOverrides(); err != nil {
		done(0, err)
		return sOK
	}
	if err := createEnvironmentWith(loader, userData, language, args, done); err != nil {
		done(0, err)
	}
	return sOK
}

func (p *proxyState) requestController(env, hwnd uintptr, composition bool, handler uintptr) uintptr {
	hr := createProfileController(env, p.store, hwnd, composition, handler)
	if !failed(hr) {
		p.startedController = true
		p.browserExited = false
	}
	return hr
}

func createProfileController(env uintptr, s *dataStore, hwnd uintptr, composition bool, handler uintptr) uintptr {
	env10 := queryInterface(env, &iidICoreWebView2Environment10)
	if env10 == 0 {
		return eNoInterface
	}
	defer release(env10)
	var options uintptr
	if hr := comCall(env10, env10CreateControllerOptions, uintptr(unsafe.Pointer(&options))); failed(hr) || options == 0 {
		return hr
	}
	defer release(options)
	if hr := comCall(options, ctlOptsPutProfileName, uintptr(unsafe.Pointer(u16(s.profile)))); failed(hr) {
		return hr
	}
	if s.private {
		if hr := comCall(options, ctlOptsPutIsInPrivateModeEnabled, 1); failed(hr) {
			return hr
		}
	}
	if composition {
		return comCall(env10, env10CreateCompositionControllerWithOptions, hwnd, options, handler)
	}
	return comCall(env10, env10CreateControllerWithOptions, hwnd, options, handler)
}

func proxyOverrides() error {
	var count int32
	argv, err := syscall.CommandLineToArgv(syscall.GetCommandLine(), &count)
	if err != nil {
		return err
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(argv)))
	args := append([]string(nil), os.Args...)
	for _, p := range unsafe.Slice((**uint16)(unsafe.Pointer(argv)), count) {
		args = append(args, wstr(uintptr(unsafe.Pointer(p))))
	}
	return sessionproxy.CheckOverrides(os.Getenv, args, func() error {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		names := []string{filepath.Base(exe)}
		var id uintptr
		hr, _, _ := shell32.NewProc("GetCurrentProcessExplicitAppUserModelID").Call(uintptr(unsafe.Pointer(&id)))
		if !failed(hr) {
			names = append(names, takeWstr(id))
		} else if uint32(hr) != 0x80004005 && uint32(hr) != 0x80070490 {
			return hresultError("reading explicit app ID", hr)
		}
		var n uint32
		appID := kernel32.NewProc("GetCurrentApplicationUserModelId")
		rc, _, _ := appID.Call(uintptr(unsafe.Pointer(&n)), 0)
		switch rc {
		case 122:
			buf := make([]uint16, n)
			rc, _, _ = appID.Call(uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&buf[0])))
			if rc != 0 {
				return syscall.Errno(rc)
			}
			names = append(names, syscall.UTF16ToString(buf))
		case 15703: // APPMODEL_ERROR_NO_APPLICATION
		default:
			return fmt.Errorf("reading packaged app ID: %d", rc)
		}
		return sessionproxy.CheckPolicy(names, func(machine bool, setting, name string) (bool, error) {
			root := uintptr(hkeyCurrentUser)
			if machine {
				root = hkeyLocalMachine
			}
			path := `SOFTWARE\Policies\Microsoft\Edge\WebView2\` + setting
			for _, view := range []uintptr{0x100, 0x200} {
				var key uintptr
				rc, _, _ := procRegOpenKeyExW.Call(root, uintptr(unsafe.Pointer(u16(path))), 0, keyQueryValue|view, uintptr(unsafe.Pointer(&key)))
				if rc == 2 || rc == 3 {
					continue
				}
				if rc != 0 {
					return false, syscall.Errno(rc)
				}
				var size uint32
				rc, _, _ = procRegQueryValueExW.Call(key, uintptr(unsafe.Pointer(u16(name))), 0, 0, 0, uintptr(unsafe.Pointer(&size)))
				procRegCloseKey.Call(key)
				if rc == 0 {
					return true, nil
				}
				if rc != 2 {
					return false, syscall.Errno(rc)
				}
			}
			return false, nil
		})
	})
}

func browserProcessID(env uintptr) int {
	env8 := queryInterface(env, &iidICoreWebView2Environment8)
	if env8 == 0 {
		return 0
	}
	defer release(env8)
	var infos uintptr
	if failed(comCall(env8, env8GetProcessInfos, uintptr(unsafe.Pointer(&infos)))) || infos == 0 {
		return 0
	}
	defer release(infos)
	var n uint32
	if failed(comCall(infos, procInfosGetCount, uintptr(unsafe.Pointer(&n)))) {
		return 0
	}
	for i := range n {
		var info uintptr
		if failed(comCall(infos, procInfosGetValueAtIndex, uintptr(i), uintptr(unsafe.Pointer(&info)))) || info == 0 {
			continue
		}
		var kind, pid int32
		ok := !failed(comCall(info, procInfoGetKind, uintptr(unsafe.Pointer(&kind)))) && kind == 0 &&
			!failed(comCall(info, procInfoGetProcessId, uintptr(unsafe.Pointer(&pid))))
		release(info)
		if ok {
			return int(pid)
		}
	}
	return 0
}

func watchProxyExit(env uintptr, p *proxyState, folder string) error {
	iid := guid("319e423d-e0d7-4b8d-9254-ae9475de9b17")
	env5 := queryInterface(env, &iid)
	if env5 == 0 {
		return platform.ErrUnsupported
	}
	var token int64
	cleanup := func() {
		comCall(env5, 13, uintptr(token))
		release(env5)
		p.cleanup = nil
		if p.store.closed && !p.envStarting {
			delete(proxyStates, p.store)
		}
		p.drain.remove(folder)
	}

	hr := withHandler(func(_, args uintptr) {
		// BrowserProcessExited may arrive after another process has started for this environment.
		var exited uint32
		if failed(comCall(args, 4, uintptr(unsafe.Pointer(&exited)))) {
			return
		}
		if current := browserProcessID(env5); current != 0 && current != int(exited) {
			return
		}
		if p.store.closed {
			cleanup()
		} else {
			p.browserExited = true
		}
	}, func(h uintptr) uintptr { return comCall(env5, 12, h, uintptr(unsafe.Pointer(&token))) })
	if failed(hr) {
		release(env5)
		return hresultError("watching proxy browser exit", hr)
	}
	p.cleanup = cleanup
	p.drain.pending[folder] = cleanup
	return nil
}

// WebView2.h 1.0.4191.47: ICoreWebView2_11 and
// ICoreWebView2DevToolsProtocolEventReceivedEventArgs2, respectively.
const (
	iidProxySessionViewStr       = "0be78e56-c193-4051-b943-23b460c08bdb"
	iidProxyEventArgs2Str        = "2dc4959d-1494-4393-95ba-bea4cb9ebd1b"
	callProtocolMethodForSession = 99
	eventArgsGetSessionID        = 4
)

func (w *window) proxyEvent(name string, handle func(string, []byte)) error {
	var receiver uintptr
	hr := comCall(w.webview, wvGetDevToolsProtocolEventReceiver, uintptr(unsafe.Pointer(u16(name))), uintptr(unsafe.Pointer(&receiver)))
	if failed(hr) || receiver == 0 {
		return hresultError(name, hr)
	}
	defer release(receiver)
	args2IID := guid(iidProxyEventArgs2Str)
	var tok int64
	hr = withHandler(func(_, args uintptr) {
		var p uintptr
		if !failed(comCall(args, cdpEventArgsGetParameterObjectAsJSON, uintptr(unsafe.Pointer(&p)))) {
			session := ""
			if args2 := queryInterface(args, &args2IID); args2 != 0 {
				var id uintptr
				if !failed(comCall(args2, eventArgsGetSessionID, uintptr(unsafe.Pointer(&id)))) {
					session = takeWstr(id)
				}
				release(args2)
			}
			handle(session, []byte(takeWstr(p)))
		}
	}, func(h uintptr) uintptr {
		return comCall(receiver, cdpReceiverAddEventReceived, h, uintptr(unsafe.Pointer(&tok)))
	})
	if failed(hr) {
		return hresultError(name, hr)
	}
	return nil
}

func (w *window) devtoolsForSession(session, method, params string, done func(string, error)) {
	id := 0
	if done != nil {
		w.nextCall++
		id = w.nextCall
		w.calls[id] = done
	}
	viewIID := guid(iidProxySessionViewStr)
	hr := withHandler(func(hr, result uintptr) {
		cb := w.calls[id]
		delete(w.calls, id)
		if cb == nil {
			return
		}
		if failed(hr) {
			cb(wstr(result), hresultError(method, hr))
			return
		}
		cb(wstr(result), nil)
	}, func(h uintptr) uintptr {
		view := queryInterface(w.webview, &viewIID)
		if view == 0 {
			return eFail
		}
		defer release(view)
		return comCall(view, callProtocolMethodForSession, uintptr(unsafe.Pointer(u16(session))), uintptr(unsafe.Pointer(u16(method))), uintptr(unsafe.Pointer(u16(params))), h)
	})
	if failed(hr) {
		if cb := w.calls[id]; cb != nil {
			delete(w.calls, id)
			cb("", hresultError(method, hr))
		}
	}
}

func (p *proxyState) setupAuth(w *window, ready func()) {
	viewIID := guid(iidProxySessionViewStr)
	v11 := queryInterface(w.webview, &viewIID)
	if v11 == 0 {
		w.webViewFailed(platform.ErrUnsupported)
		return
	}
	release(v11)
	sessionproxy.Attach(p.config.HTTPProxy, p.config.Username, p.config.Password, w.proxyEvent, func(session, method, params string, done func(error)) {
		var cb func(string, error)
		if done != nil {
			cb = func(_ string, err error) { done(err) }
		}
		if session == "" {
			w.devtools(method, params, cb)
			return
		}
		w.devtoolsForSession(session, method, params, cb)
	}, ready, w.webViewFailed)
}
