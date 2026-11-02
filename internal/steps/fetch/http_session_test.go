package fetch_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/pipeline"
	"github.com/sunshow/siphongear/internal/session"
	_ "github.com/sunshow/siphongear/internal/steps"
	"github.com/sunshow/siphongear/internal/store"
	"github.com/sunshow/siphongear/internal/store/models"
)

func openFetchTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "fetch.db") + "?_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newFetchTestManager(t *testing.T, db *gorm.DB) *session.Manager {
	t.Helper()
	cipher, err := crypto.New("fetch-test-master-key-32-bytes!!!")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return session.NewManager(db, cipher)
}

// fakeSite mimics a cookie-login site that hands out a session cookie and a
// device cookie on login, and 401s unauthenticated requests.
type fakeSite struct {
	mu            sync.Mutex
	logins        int
	userAgents    []string
	cookieHeaders []string
	server        *httptest.Server
}

func newFakeSite(t *testing.T) *fakeSite {
	t.Helper()
	site := &fakeSite{}
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.logins++
		site.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1", Path: "/", HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "device", Value: "d1", Path: "/", MaxAge: 30 * 24 * 3600})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		rec := map[string]string{}
		for _, c := range r.Cookies() {
			rec[c.Name] = c.Value
		}
		if rec["sid"] != "s1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":42}`))
	})
	mux.HandleFunc("/echo-cookie", func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.cookieHeaders = append(site.cookieHeaders, r.Header.Get("Cookie"))
		site.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/ua", func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		site.userAgents = append(site.userAgents, r.Header.Get("User-Agent"))
		site.mu.Unlock()
		// A real site sets some cookie; the UA is only persisted alongside
		// actual session data.
		http.SetCookie(w, &http.Cookie{Name: "seen", Value: "1", Path: "/"})
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	site.server = httptest.NewServer(mux)
	t.Cleanup(site.server.Close)
	return site
}

func numeric(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return math.NaN()
}

func (s *fakeSite) loginCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

func sessionContext(manager *session.Manager, key string) *pipeline.Context {
	return &pipeline.Context{
		Context:    context.Background(),
		Logger:     zerolog.Nop(),
		Sessions:   manager,
		SessionKey: key,
		Now:        time.Now(),
	}
}

func runDefinition(t *testing.T, ctx *pipeline.Context, def pipeline.Definition) (*pipeline.Result, error) {
	t.Helper()
	return pipeline.NewEngine().Run(ctx, def, pipeline.NewPayload())
}

// reuseDefinition is the pattern used by the built-in login templates: probe the
// protected endpoint with the saved session, log in only when it is unusable.
func reuseDefinition(site *fakeSite) pipeline.Definition {
	return pipeline.Definition{
		Steps: []pipeline.StepConfig{
			{Kind: "session.load"},
			{Kind: "fetch.http", Name: "probe", When: "vars.session_ok", OnError: "continue",
				Config: map[string]any{
					"method":         "GET",
					"url":            site.server.URL + "/data",
					"timeout":        5,
					"save_status_as": "probe_status",
				}},
			{Kind: "script.js.transform", Name: "check session",
				Config: map[string]any{
					"source":     `return { vars: { session_ok: Number(payload.vars.probe_status || 0) === 200 } };`,
					"timeout_ms": 2000,
				}},
			{Kind: "fetch.http", Name: "login", When: "!vars.session_ok",
				Config: map[string]any{
					"method":  "POST",
					"url":     site.server.URL + "/login",
					"timeout": 5,
				}},
			{Kind: "session.save", Name: "remember session", When: "!vars.session_ok",
				Config: map[string]any{"login": true}},
			{Kind: "fetch.http", Name: "data", When: "!vars.session_ok",
				Config: map[string]any{
					"method":  "GET",
					"url":     site.server.URL + "/data",
					"timeout": 5,
				}},
			{Kind: "script.js.extract", Name: "extract value",
				Config: map[string]any{
					"source":     `var m = JSON.parse(payload.body || "{}"); return { vars: { value: m.value } };`,
					"timeout_ms": 2000,
				}},
		},
		Indicators: []pipeline.IndicatorBind{{Key: "value"}},
	}
}

func TestHTTPSession_SecondRunReusesPersistedCookies(t *testing.T) {
	db := openFetchTestDB(t)
	manager := newFetchTestManager(t, db)
	site := newFakeSite(t)
	def := reuseDefinition(site)

	ctx := sessionContext(manager, "collector:9")
	res, err := runDefinition(t, ctx, def)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if numeric(res.Indicators["value"]) != 42 {
		t.Fatalf("first run value = %v", res.Indicators["value"])
	}
	if got := site.loginCount(); got != 1 {
		t.Fatalf("first run logins = %d, want 1", got)
	}

	// A new Manager instance reads the state back from the DB (process restart).
	restarted := newFetchTestManager(t, db)
	ctx2 := sessionContext(restarted, "collector:9")
	res2, err := runDefinition(t, ctx2, def)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if numeric(res2.Indicators["value"]) != 42 {
		t.Fatalf("second run value = %v", res2.Indicators["value"])
	}
	if got := site.loginCount(); got != 1 {
		t.Fatalf("second run should not log in again, logins = %d", got)
	}

	state, err := restarted.Load(context.Background(), "collector:9")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state == nil || state.LoginCount != 1 {
		t.Fatalf("persisted state = %+v, want exactly one login recorded", state)
	}
	if state.CookieCount(time.Now()) != 2 {
		t.Fatalf("persisted cookies = %+v, want sid + device", state.Cookies)
	}
}

func TestHTTPSession_ExpiredSessionTriggersRelogin(t *testing.T) {
	db := openFetchTestDB(t)
	manager := newFetchTestManager(t, db)
	site := newFakeSite(t)
	def := reuseDefinition(site)
	ctx := sessionContext(manager, "collector:10")

	if _, err := runDefinition(t, ctx, def); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Simulate a server-side invalidation: the saved cookie is no longer valid.
	state, err := manager.Load(context.Background(), "collector:10")
	if err != nil || state == nil {
		t.Fatalf("load state: %v", err)
	}
	for i := range state.Cookies {
		if state.Cookies[i].Name == "sid" {
			state.Cookies[i].Value = "stale"
		}
	}
	state.CollectorID = 10
	if err := manager.Save(context.Background(), "collector:10", state); err != nil {
		t.Fatalf("save stale state: %v", err)
	}

	res, err := runDefinition(t, ctx, def)
	if err != nil {
		t.Fatalf("recovery run: %v", err)
	}
	if numeric(res.Indicators["value"]) != 42 {
		t.Fatalf("recovery value = %v", res.Indicators["value"])
	}
	if got := site.loginCount(); got != 2 {
		t.Fatalf("logins after recovery = %d, want 2", got)
	}
	state, err = manager.Load(context.Background(), "collector:10")
	if err != nil || state == nil {
		t.Fatalf("reload state: %v", err)
	}
	if state.LoginCount != 2 {
		t.Fatalf("login count = %d, want 2", state.LoginCount)
	}
}

func TestHTTPSession_DryRunNeverWritesState(t *testing.T) {
	db := openFetchTestDB(t)
	manager := newFetchTestManager(t, db)
	site := newFakeSite(t)
	def := reuseDefinition(site)

	// The runner wraps the persistent store in an in-memory overlay for dry
	// runs: the login flow must complete inside one dry run, yet nothing may be
	// written to the real session.
	for i := 0; i < 2; i++ {
		ctx := sessionContext(manager, "collector:11")
		ctx.DryRun = true
		ctx.Sessions = session.NewDryRunStore(manager)
		if _, err := runDefinition(t, ctx, def); err != nil {
			t.Fatalf("dry run %d: %v", i, err)
		}
	}
	if got := site.loginCount(); got != 2 {
		t.Fatalf("dry runs should not persist a session, logins = %d", got)
	}
	var count int64
	if err := db.Model(&models.Session{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("dry run wrote %d session row(s)", count)
	}
}

func TestHTTPSession_PersistsAndReusesUserAgent(t *testing.T) {
	db := openFetchTestDB(t)
	manager := newFetchTestManager(t, db)
	site := newFakeSite(t)

	firstDef := pipeline.Definition{Steps: []pipeline.StepConfig{
		{Kind: "fetch.http", Config: map[string]any{
			"method":     "GET",
			"url":        site.server.URL + "/ua",
			"timeout":    5,
			"user_agent": "DeviceUA/1.0",
		}},
	}}
	ctx := sessionContext(manager, "collector:12")
	if _, err := runDefinition(t, ctx, firstDef); err != nil {
		t.Fatalf("first run: %v", err)
	}

	secondDef := pipeline.Definition{Steps: []pipeline.StepConfig{
		{Kind: "fetch.http", Config: map[string]any{
			"method":  "GET",
			"url":     site.server.URL + "/ua",
			"timeout": 5,
		}},
	}}
	if _, err := runDefinition(t, ctx, secondDef); err != nil {
		t.Fatalf("second run: %v", err)
	}

	site.mu.Lock()
	uas := append([]string(nil), site.userAgents...)
	site.mu.Unlock()
	if len(uas) != 2 {
		t.Fatalf("recorded UAs = %v", uas)
	}
	for i, ua := range uas {
		if ua != "DeviceUA/1.0" {
			t.Fatalf("request %d UA = %q, want the persisted device UA", i, ua)
		}
	}
}

func TestHTTPSession_ExplicitCookieHeaderMergesWithSavedJar(t *testing.T) {
	db := openFetchTestDB(t)
	manager := newFetchTestManager(t, db)
	site := newFakeSite(t)
	ctx := sessionContext(manager, "collector:13")

	// Seed the jar directly: host comes from the httptest server URL.
	host := strings.TrimPrefix(site.server.URL, "http://")
	host, _, _ = strings.Cut(host, ":")
	state := session.NewState()
	state.Cookies = []session.Cookie{{Name: "sid", Value: "s1", Domain: host, HostOnly: true, Path: "/"}}
	state.CollectorID = 13
	if err := manager.Save(context.Background(), "collector:13", state); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	def := pipeline.Definition{Steps: []pipeline.StepConfig{
		{Kind: "fetch.http", Config: map[string]any{
			"method":  "GET",
			"url":     site.server.URL + "/echo-cookie",
			"timeout": 5,
			"headers": map[string]any{"Cookie": "manual=1"},
		}},
	}}
	if _, err := runDefinition(t, ctx, def); err != nil {
		t.Fatalf("run: %v", err)
	}

	site.mu.Lock()
	headers := append([]string(nil), site.cookieHeaders...)
	site.mu.Unlock()
	if len(headers) != 1 {
		t.Fatalf("cookie headers = %v", headers)
	}
	header := headers[0]
	if strings.Count(header, "sid=") != 1 || !strings.Contains(header, "sid=s1") {
		t.Fatalf("saved cookie missing or duplicated: %q", header)
	}
	if strings.Count(header, "manual=1") != 1 {
		t.Fatalf("explicit cookie missing or duplicated: %q", header)
	}
}

func TestHTTPSession_DisabledKeepsPlainCookieHeader(t *testing.T) {
	db := openFetchTestDB(t)
	manager := newFetchTestManager(t, db)
	site := newFakeSite(t)
	ctx := sessionContext(manager, "collector:14")

	def := pipeline.Definition{Steps: []pipeline.StepConfig{
		{Kind: "fetch.http", Config: map[string]any{
			"method":      "GET",
			"url":         site.server.URL + "/echo-cookie",
			"timeout":     5,
			"use_session": false,
			"headers":     map[string]any{"Cookie": "manual=1"},
		}},
	}}
	if _, err := runDefinition(t, ctx, def); err != nil {
		t.Fatalf("run: %v", err)
	}
	site.mu.Lock()
	headers := append([]string(nil), site.cookieHeaders...)
	site.mu.Unlock()
	if len(headers) != 1 || headers[0] != "manual=1" {
		t.Fatalf("cookie headers = %v", headers)
	}
	if state, err := manager.Load(context.Background(), "collector:14"); err != nil || state != nil {
		t.Fatalf("session should not be written when disabled, got %+v err=%v", state, err)
	}
}
