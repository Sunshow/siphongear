package templates_test

import (
	"context"
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
	"github.com/sunshow/siphongear/internal/store"
	"github.com/sunshow/siphongear/internal/templates"
)

func openTemplateSessionDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "templates.db") + "?_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newTemplateSessionManager(t *testing.T, db *gorm.DB) *session.Manager {
	t.Helper()
	cipher, err := crypto.New("template-test-master-key-32-bytes!")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return session.NewManager(db, cipher)
}

func runPipelineWithSessions(t *testing.T, def pipeline.Definition, creds staticResolver, sessions pipeline.SessionStore, key string) (map[string]any, error) {
	t.Helper()
	ctx := &pipeline.Context{
		Context:     context.Background(),
		Logger:      zerolog.Nop(),
		Credentials: creds,
		Sessions:    sessions,
		SessionKey:  key,
		Now:         time.Now(),
	}
	res, err := pipeline.NewEngine().Run(ctx, def, pipeline.NewPayload())
	if err != nil {
		return nil, err
	}
	return res.Indicators, nil
}

func numericIndicator(t *testing.T, indicators map[string]any, key string) float64 {
	t.Helper()
	switch n := indicators[key].(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	t.Fatalf("indicator %q = %v (%T), want number", key, indicators[key], indicators[key])
	return 0
}

// newAPICookieLoginServer fakes the NewAPI cookie-session login flow.
func newAPICookieLoginServer(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	logins := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		logins++
		mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "sess-1", Path: "/", HttpOnly: true})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":69}}`))
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != "sess-1" || r.Header.Get("New-Api-User") != "69" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"message":"Unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success": true, "data": {"id": 69, "quota": 44637095, "used_quota": 230362905}}`))
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000,"display_in_currency":true}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return logins
	}
}

func TestNewAPIBalanceTemplate_ReusesCookieSession(t *testing.T) {
	db := openTemplateSessionDB(t)
	manager := newTemplateSessionManager(t, db)
	srv, logins := newAPICookieLoginServer(t)
	def := buildDefinition(t, "newapi-balance", srv.URL, 1)
	creds := staticResolver{1: {"username": "user", "password": "pass"}}

	first, err := runPipelineWithSessions(t, def, creds, manager, "collector:1")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := numericIndicator(t, first, "balance"); got != 89.27419 {
		t.Fatalf("first run balance = %v", got)
	}
	if got := logins(); got != 1 {
		t.Fatalf("first run logins = %d, want 1", got)
	}

	// A fresh Manager instance proves the session was persisted, not cached.
	restarted := newTemplateSessionManager(t, db)
	second, err := runPipelineWithSessions(t, def, creds, restarted, "collector:1")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := numericIndicator(t, second, "balance"); got != 89.27419 {
		t.Fatalf("second run balance = %v", got)
	}
	if got := logins(); got != 1 {
		t.Fatalf("second run must reuse the session, logins = %d", got)
	}

	state, err := restarted.Load(context.Background(), "collector:1")
	if err != nil || state == nil {
		t.Fatalf("load session: %v", err)
	}
	if state.Vars["user_id"] != "69" {
		t.Fatalf("persisted user_id = %v", state.Vars["user_id"])
	}
	if state.LoginCount != 1 || state.CookieCount(time.Now()) == 0 {
		t.Fatalf("persisted session metadata = %+v", state)
	}

	// The cookie may still be valid, but require_vars=["user_id"] must force a
	// re-login when the derived var is missing (the probe needs New-Api-User).
	delete(state.Vars, "user_id")
	state.CollectorID = 1
	if err := restarted.Save(context.Background(), "collector:1", state); err != nil {
		t.Fatalf("save session without user_id: %v", err)
	}
	third, err := runPipelineWithSessions(t, def, creds, restarted, "collector:1")
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if got := numericIndicator(t, third, "balance"); got != 89.27419 {
		t.Fatalf("third run balance = %v", got)
	}
	if got := logins(); got != 2 {
		t.Fatalf("missing required var must trigger a re-login, logins = %d", got)
	}
}

// gpt2imageDashboardHTML mimics the parts of the redesigned dashboard the
// extraction regex anchors on.
const gpt2imageDashboardHTML = `<html><body>
<div>Credits Balance</div>
<div data-slot="card-content" class="p-6"><div class="font-serif text-3xl">1,234</div></div>
<div>Images Generated</div>
<div data-slot="card-content" class="p-6"><div class="font-serif text-3xl">5</div></div>
</body></html>`

func newGPT2ImageServer(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	logins := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/sign-in/email", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		logins++
		mu.Unlock()
		// Real better-auth sets a __Secure- cookie over https; httptest is http,
		// so issue it without the Secure flag (the jar matches by name/domain).
		http.SetCookie(w, &http.Cookie{Name: "__Secure-better-auth.session_token", Value: "tok-1", Path: "/", HttpOnly: true})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/en/dashboard", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("__Secure-better-auth.session_token"); err != nil {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><h1>Sign in</h1></body></html>`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(gpt2imageDashboardHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return logins
	}
}

func TestGPT2ImageTemplate_ReusesCookieSession(t *testing.T) {
	db := openTemplateSessionDB(t)
	manager := newTemplateSessionManager(t, db)
	srv, logins := newGPT2ImageServer(t)
	def := buildDefinition(t, "gpt2image-balance", srv.URL, 2)
	creds := staticResolver{2: {"email": "user@example.com", "password": "pass"}}

	first, err := runPipelineWithSessions(t, def, creds, manager, "collector:2")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := numericIndicator(t, first, "balance"); got != 1234 {
		t.Fatalf("first run balance = %v", got)
	}
	if got := numericIndicator(t, first, "images_generated"); got != 5 {
		t.Fatalf("first run images = %v", got)
	}
	if got := logins(); got != 1 {
		t.Fatalf("first run logins = %d, want 1", got)
	}

	second, err := runPipelineWithSessions(t, def, creds, manager, "collector:2")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := numericIndicator(t, second, "balance"); got != 1234 {
		t.Fatalf("second run balance = %v", got)
	}
	if got := logins(); got != 1 {
		t.Fatalf("second run must reuse the session, logins = %d", got)
	}
}

func newSub2APIServer(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	logins := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		logins++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"access_token":"tok-1"}}`))
	})
	mux.HandleFunc("/api/v1/user/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"balance":7.5}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return logins
	}
}

func TestSub2APITemplate_ReusesTokenSession(t *testing.T) {
	db := openTemplateSessionDB(t)
	manager := newTemplateSessionManager(t, db)
	srv, logins := newSub2APIServer(t)
	def := buildDefinition(t, "sub2api-balance", srv.URL, 3)
	creds := staticResolver{3: {"email": "user@example.com", "password": "pass"}}

	if _, err := runPipelineWithSessions(t, def, creds, manager, "collector:3"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := runPipelineWithSessions(t, def, creds, manager, "collector:3")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := numericIndicator(t, second, "balance"); got != 7.5 {
		t.Fatalf("second run balance = %v", got)
	}
	if got := logins(); got != 1 {
		t.Fatalf("second run must reuse the persisted token, logins = %d", got)
	}
}

// TestLoginTemplatesUseSessionReuse guards the pipeline shape so a future edit
// cannot silently regress the four password-login templates to logging in on
// every scheduled run.
func TestLoginTemplatesUseSessionReuse(t *testing.T) {
	for _, name := range []string{"sub2api-balance", "newapi-balance", "udealproxy-balance", "gpt2image-balance"} {
		tpl, ok := templates.Get(name)
		if !ok {
			t.Fatalf("template %q missing", name)
		}
		if err := tpl.Pipeline.Validate(); err != nil {
			t.Fatalf("%s: validate: %v", name, err)
		}
		var hasLoad, hasConditionalLogin, hasSessionSave bool
		for _, step := range tpl.Pipeline.Steps {
			if step.Kind == "session.load" {
				hasLoad = true
			}
			if step.Kind == "fetch.http" && strings.Contains(step.When, "session_ok") && step.OnError == "continue" {
				// Probe step: skipped when there is no session, errors tolerated.
				if step.When == "vars.session_ok" {
					hasConditionalLogin = true
				}
			}
			if step.Kind == "session.save" {
				if login, _ := step.Config["login"].(bool); login {
					hasSessionSave = true
				}
			}
			if step.Kind == "fetch.http" {
				if headers, ok := step.Config["headers"].(map[string]any); ok {
					for key := range headers {
						if strings.EqualFold(key, "Cookie") {
							t.Fatalf("%s: fetch.http %q still sets a manual Cookie header; the session jar must own it", name, step.Name)
						}
					}
				}
			}
		}
		if !hasLoad || !hasConditionalLogin || !hasSessionSave {
			t.Fatalf("%s: missing session reuse wiring (load=%v probe=%v save=%v)", name, hasLoad, hasConditionalLogin, hasSessionSave)
		}
	}
}
