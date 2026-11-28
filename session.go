package mygo

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// Session is a website data store of pages: their cookies, local and
// session storage, IndexedDB, caches and service workers, which the pages
// of a session share and the pages of other sessions don't see, as
// Electron's partitions. A page shows in the default session unless its
// PageOptions.Session names another:
//
//	work := mygo.NewSession("work")       // kept on disk, as the default one
//	private := mygo.NewEphemeralSession() // in memory only
//	w := mygo.NewWindow(mygo.WindowOptions{URL: "https://example.com",
//		Page: mygo.PageOptions{Session: work}})
//	list, err := work.Cookies.List(mygo.CookieFilter{})
//	err = work.ClearData(mygo.SiteDataAll)
//
// For example, two accounts of a service side by side, a private window,
// or a preview that must not see the user's sign-ins. A window opened by
// window.open keeps its opener's session. Custom protocols (Protocol)
// serve the pages of every session.
//
// On macOS, a session NewSession makes keeps its data on disk from macOS
// 14 on; before, it keeps it in memory until the app quits, still apart
// from the other sessions' data.
type Session struct {
	// Cookies reads and changes the session's cookies, as mygo.Cookies
	// does those of the default session.
	Cookies CookiesModule

	name      string
	ephemeral bool
	// store is the backend's data store, made when a page of the session
	// or a call first needs it; pages counts the pages using it. Main
	// thread only.
	store       platform.DataStore
	pages       int
	proxy       *platform.SessionProxyConfig
	createStore func(platform.AppController, platform.DataStoreOptions) (platform.DataStore, error)
}

var (
	defaultSession = &Session{Cookies: CookiesModule{calls: &sessionCalls}}
	// sessions are the sessions NewSession made, by name.
	sessions = struct {
		sync.Mutex
		named map[string]*Session
	}{named: map[string]*Session{}}
	// openStore makes the backend's store of a session, set by the
	// constructors of sessions so that apps that make none don't link it.
	// It is guarded by the lock of sessions.
	openStore func(*Session) (platform.DataStore, error)
)

// DefaultSession returns the default session, which pages show in
// unless their PageOptions.Session names another: its cookies are
// mygo.Cookies, and App.ClearBrowsingData clears all of its data.
func DefaultSession() *Session { return defaultSession }

// NewSession returns the persistent session of name, which keeps its
// data on disk from one run of the app to the next, as the default
// session does: every call with the same name returns the same session.
// The name is any text but "", which the app keeps from one run to the
// next to find the session's data again. NewSession may be called before
// App.Run.
func NewSession(name string) *Session {
	if name == "" {
		panic("mygo: NewSession needs a name; DefaultSession returns the default session")
	}
	sessions.Lock()
	defer sessions.Unlock()
	if s := sessions.named[name]; s != nil {
		return s
	}
	return makeSession(name, nil)
}

// makeSession runs with sessions locked.
func makeSession(name string, proxy *platform.SessionProxyConfig) *Session {
	s := &Session{name: name, ephemeral: name == "", proxy: proxy}
	s.Cookies = CookiesModule{s, &sessionCalls}
	if name != "" {
		sessions.named[name] = s
	}
	openStore = openSessionStore
	return s
}

// SessionProxyConfig configures an HTTP proxy for a desktop session.
// macOS configures a Network.framework HTTP CONNECT proxy (used for both HTTP
// and HTTPS) with direct failover disabled; Network.framework still bypasses
// loopback destinations (127.0.0.1/::1). Linux and Windows use HTTP CONNECT
// for HTTPS, standard HTTP proxy forwarding (absolute-URI requests) for plain
// HTTP, and route loopback destinations (127.0.0.1) through the proxy.
// Authenticated Linux proxies require WebKitGTK 4.1 with libsoup 3. Windows
// also disables QUIC and non-proxied WebRTC UDP; macOS and Linux do not
// disable WebRTC UDP via SessionProxyConfig.
type SessionProxyConfig struct {
	// HTTPProxy is the proxy endpoint as an http URL with a numeric IP address
	// and explicit port (for example "http://127.0.0.1:8080" or "http://[::1]:8080").
	// Hostnames are rejected so the proxy destination cannot be redirected by
	// DNS resolution or rebinding and so proxy auth challenges match unambiguously.
	HTTPProxy string
	// Username is the optional HTTP Basic proxy username.
	Username string
	// Password is the optional HTTP Basic proxy password.
	Password string
}

// SessionOptions selects the storage and network settings for a session.
// On Windows a proxied session uses a separate WebView2 environment and
// browser process, increasing memory usage. Its WebView2Proxy data directory
// is isolated from an unproxied session with the same name, and quitting the
// app while an ephemeral proxied session exits may wait up to 1.5 seconds so
// WebView2 can release its temporary directory for removal.
type SessionOptions struct {
	// Name identifies a persistent session kept on disk across runs.
	Name string
	// Ephemeral creates a new private session. Name must be empty.
	Ephemeral bool
	// Proxy routes web traffic through an HTTP proxy. Nil uses system settings.
	// On Windows each proxied session needs a separate browser process and
	// WebView2Proxy/<profile> data directory, isolated from its unproxied namesake.
	Proxy *SessionProxyConfig
}

// NewSessionWithOptions returns a fresh ephemeral session or the named session
// with matching options. NewSession(name) and NewEphemeralSession() are
// convenience entry points for unproxied sessions; NewSession(name) also
// retrieves an existing named session regardless of its options.
// Proxies require macOS 14+, Windows or Linux. Unsupported platforms return
// an error matching errors.ErrUnsupported. On Windows see SessionOptions for
// the storage isolation, browser-process memory cost and ephemeral quit wait.
func NewSessionWithOptions(opts SessionOptions) (*Session, error) {
	if opts.Name == "" && !opts.Ephemeral {
		return nil, fmt.Errorf("mygo: SessionOptions needs Name or Ephemeral; DefaultSession returns the default session")
	}
	if opts.Name != "" && opts.Ephemeral {
		return nil, fmt.Errorf("mygo: an ephemeral session cannot have a name")
	}
	var proxy *platform.SessionProxyConfig
	if opts.Proxy != nil {
		config := platform.SessionProxyConfig{
			HTTPProxy: opts.Proxy.HTTPProxy,
			Username:  opts.Proxy.Username,
			Password:  opts.Proxy.Password,
		}
		u, err := url.Parse(config.HTTPProxy)
		if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(config.HTTPProxy, "#") {
			return nil, fmt.Errorf("mygo: invalid HTTP proxy endpoint")
		}
		target, err := netip.ParseAddrPort(u.Host)
		if err != nil || target.Port() == 0 || target.Addr().Zone() != "" || strings.ContainsRune(config.Username, 0) || strings.ContainsRune(config.Password, 0) {
			return nil, fmt.Errorf("mygo: invalid proxy endpoint or credentials")
		}
		if !supportsSessionProxy() {
			return nil, platform.ErrUnsupported
		}
		config.HTTPProxy = "http://" + target.String()
		proxy = &config
	}
	sessions.Lock()
	defer sessions.Unlock()
	if s := sessions.named[opts.Name]; s != nil {
		if s.proxy == proxy || (s.proxy != nil && proxy != nil && *s.proxy == *proxy) {
			return s, nil
		}
		return nil, fmt.Errorf("mygo: session already exists with different options")
	}
	s := makeSession(opts.Name, proxy)
	if proxy != nil {
		s.createStore = openProxyStore
	}
	return s, nil
}

// NewEphemeralSession returns a new session that keeps its data in
// memory and writes nothing to disk, as a browser's private window: its
// data goes when the last page using it closes, and the next page that
// uses the session starts with none. Cookies set before its first page
// opens go to that page. NewEphemeralSession may be called before App.Run.
func NewEphemeralSession() *Session {
	sessions.Lock()
	defer sessions.Unlock()
	return makeSession("", nil)
}

// Name returns the name of a session NewSession made, "" for the default
// session and ephemeral ones.
func (s *Session) Name() string { return s.name }

// IsEphemeral reports whether the session keeps its data in memory only
// (NewEphemeralSession).
func (s *Session) IsEphemeral() bool { return s.ephemeral }

// isDefault reports whether s is the default session, as nil is.
func (s *Session) isDefault() bool { return s == nil || s.name == "" && !s.ephemeral }

// dataStore returns the backend's store of s, nil for the default
// session, making it if needed. Main thread only.
func (s *Session) dataStore() (platform.DataStore, error) {
	if s.isDefault() {
		return nil, nil
	}
	if s.store == nil {
		sessions.Lock()
		open := openStore
		sessions.Unlock()
		store, err := open(s)
		if err != nil {
			return nil, err
		}
		s.store = store
	}
	return s.store, nil
}

// attach counts a page that uses s, and returns the store it uses. Main
// thread only.
func (s *Session) attach() (platform.DataStore, error) {
	store, err := s.dataStore()
	if err == nil && store != nil {
		s.pages++
	}
	return store, err
}

// detach counts a page that used s gone: with the last page of an
// ephemeral session its store goes, and its data. Main thread only.
func (s *Session) detach() {
	if s.isDefault() || s.pages == 0 {
		return
	}
	s.pages--
	if s.pages == 0 && s.ephemeral && s.store != nil {
		s.store.Close()
		s.store = nil
	}
}

// openSessionStore makes the store of a session other than the default
// one: a persistent session's is identified by a UUID made of its name,
// and kept in directories of its own on Linux.
func openSessionStore(s *Session) (platform.DataStore, error) {
	opts := platform.DataStoreOptions{Ephemeral: s.ephemeral, Proxy: s.proxy}
	if !s.ephemeral {
		opts.ID = sessionID(s.name)
		if runtime.GOOS == "linux" {
			data, err := App.Path(PathUserData)
			if err != nil {
				return nil, fmt.Errorf("mygo: session %q: %w", s.name, err)
			}
			cache, err := App.Path(PathCache)
			if err != nil {
				return nil, fmt.Errorf("mygo: session %q: %w", s.name, err)
			}
			opts.DataDir = filepath.Join(data, "Sessions", opts.ID)
			opts.CacheDir = filepath.Join(cache, "Sessions", opts.ID)
		}
	}
	create := s.createStore
	if create == nil {
		create = platform.AppController.NewDataStore
	}
	store, err := create(backend().App(), opts)
	if err != nil {
		return nil, fmt.Errorf("mygo: session %q: %w", s.name, err)
	}
	return store, nil
}

// sessionID returns the UUID of a session's name, the same at every run:
// the first 16 bytes of its SHA-256, as a version 8 UUID (RFC 9562).
func sessionID(name string) string {
	sum := sha256.Sum256([]byte("mygo session " + name))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x80
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// SiteData are kinds of data pages store, which Session.ClearData clears.
type SiteData uint

const (
	SiteDataCookies = SiteData(platform.SiteDataCookies)
	// SiteDataLocalStorage is localStorage and sessionStorage.
	SiteDataLocalStorage = SiteData(platform.SiteDataLocalStorage)
	// SiteDataIndexedDB is IndexedDB, and WebSQL where the engine still
	// has it.
	SiteDataIndexedDB = SiteData(platform.SiteDataIndexedDB)
	// SiteDataCaches is the HTTP cache, and Cache Storage (window.caches).
	SiteDataCaches         = SiteData(platform.SiteDataCaches)
	SiteDataServiceWorkers = SiteData(platform.SiteDataServiceWorkers)
	// SiteDataAll is all the data the engine keeps for sites: the kinds
	// above and the others it has, such as HSTS and media keys.
	SiteDataAll = SiteData(platform.SiteDataAll)
)

// ClearData deletes the data of types the session's pages stored, as
// SiteDataCookies|SiteDataLocalStorage, or SiteDataAll when the user
// signs out. Open pages keep what they hold in memory until they reload.
// It is safe from any goroutine once the app is running.
func (s *Session) ClearData(types SiteData) error {
	if s.Cookies.calls == nil {
		return errNoSession
	}
	return s.Cookies.calls.clearData(s, types)
}

func clearSessionData(s *Session, types SiteData) error {
	needsApp("Session.ClearData")
	if types == 0 {
		return nil
	}
	return nativeCall(func(done func(nativeResult)) {
		store, err := s.dataStore()
		if err != nil {
			done(nativeResult{err: err})
			return
		}
		backend().App().ClearBrowsingData(store, platform.SiteData(types), func(err error) { done(nativeResult{err: err}) })
	}).err
}
