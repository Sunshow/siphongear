package templates_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/rs/zerolog"

	"github.com/sunshow/siphongear/internal/pipeline"
	_ "github.com/sunshow/siphongear/internal/steps"
	"github.com/sunshow/siphongear/internal/templates"
)

// staticResolver serves the plaintext credential payloads used by a test.
type staticResolver map[uint]map[string]any

func (r staticResolver) Resolve(_ context.Context, id uint) (map[string]any, error) {
	return r[id], nil
}

// buildDefinition renders {{BASE_URL}} the way the web wizard does and binds
// the credential ID, then round-trips through JSON like a persisted collector.
func buildDefinition(t *testing.T, name, baseURL string, credentialID uint) pipeline.Definition {
	t.Helper()
	tpl, ok := templates.Get(name)
	if !ok {
		t.Fatalf("template %q not registered", name)
	}
	raw, err := sonic.Marshal(tpl.Pipeline)
	if err != nil {
		t.Fatalf("marshal pipeline: %v", err)
	}
	rendered := strings.ReplaceAll(string(raw), "{{BASE_URL}}", baseURL)

	var def pipeline.Definition
	if err := sonic.UnmarshalString(rendered, &def); err != nil {
		t.Fatalf("unmarshal pipeline: %v", err)
	}
	for i := range def.Steps {
		if def.Steps[i].Kind == "input.credential" {
			def.Steps[i].Config["credential_id"] = float64(credentialID)
		}
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("validate pipeline: %v", err)
	}
	return def
}

func runPipeline(t *testing.T, def pipeline.Definition, creds staticResolver) (map[string]any, error) {
	t.Helper()
	ctx := &pipeline.Context{
		Context:     context.Background(),
		Logger:      zerolog.Nop(),
		Credentials: creds,
		Now:         time.Now(),
	}
	res, err := pipeline.NewEngine().Run(ctx, def, pipeline.NewPayload())
	if err != nil {
		return nil, err
	}
	return res.Indicators, nil
}

// newAPIServer fakes the NewAPI dashboard endpoints used by the balance
// templates. It reports the Authorization header it saw so tests can assert
// the token was normalized before being sent.
func newAPIServer(t *testing.T, userSelfBody string, wantAuth string) (*httptest.Server, *string) {
	t.Helper()
	var seenAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		if seenAuth != wantAuth {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"code":"AUTH_UNAUTHORIZED","message":"Unauthorized, invalid access token","success":false}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, userSelfBody)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{"quota_per_unit":500000,"display_in_currency":true}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &seenAuth
}

const newAPIUserSelf = `{"success":true,"data":{"id":69,"quota":44637095,"used_quota":230362905}}`

func TestNewAPIAccesstokenTemplate_AccessTokenField(t *testing.T) {
	srv, seenAuth := newAPIServer(t, newAPIUserSelf, "Bearer nap_testtoken")
	def := buildDefinition(t, "newapi-balance-accesstoken", srv.URL, 1)

	indicators, err := runPipeline(t, def, staticResolver{1: {
		"access_token": "nap_testtoken",
		"user_id":      "69",
	}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := indicators["balance"]; got != 89.27419 {
		t.Fatalf("balance = %v, want 89.27419", got)
	}
	if got := indicators["used"]; got != 460.72581 {
		t.Fatalf("used = %v, want 460.72581", got)
	}
	if *seenAuth != "Bearer nap_testtoken" {
		t.Fatalf("Authorization = %q", *seenAuth)
	}
}

func TestNewAPIAccesstokenTemplate_ManualCredentialUsesAPIKeyFallback(t *testing.T) {
	// A credential created through the generic Credentials page stores the
	// token under api_key; the template must still work.
	srv, _ := newAPIServer(t, newAPIUserSelf, "Bearer nap_fallback")
	def := buildDefinition(t, "newapi-balance-accesstoken", srv.URL, 2)

	indicators, err := runPipeline(t, def, staticResolver{2: {
		"api_key": "  Bearer nap_fallback \n",
		"user_id": "69",
	}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := indicators["balance"]; got != 89.27419 {
		t.Fatalf("balance = %v, want 89.27419", got)
	}
}

func TestNewAPIAccesstokenTemplate_MissingTokenFailsClearly(t *testing.T) {
	srv, _ := newAPIServer(t, newAPIUserSelf, "Bearer nap_testtoken")
	def := buildDefinition(t, "newapi-balance-accesstoken", srv.URL, 3)

	_, err := runPipeline(t, def, staticResolver{3: {"user_id": "69"}})
	if err == nil {
		t.Fatal("expected error for credential without access token")
	}
	if !strings.Contains(err.Error(), "access_token") {
		t.Fatalf("error should mention access_token, got: %v", err)
	}
}

func TestRixAPIAccesstokenTemplate_Balance(t *testing.T) {
	srv, _ := newAPIServer(t, `{"success":true,"data":{"balance":12.5}}`, "Bearer nap_rix")
	def := buildDefinition(t, "rixapi-balance-accesstoken", srv.URL, 4)

	indicators, err := runPipeline(t, def, staticResolver{4: {"access_token": "nap_rix"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := indicators["balance"]; got != 12.5 {
		t.Fatalf("balance = %v, want 12.5", got)
	}
}

// Guard the template JSON shape stays serializable and the new normalization
// step is present in both new-api token templates.
func TestNewAPITemplates_HaveTokenNormalization(t *testing.T) {
	for _, name := range []string{"newapi-balance-accesstoken", "rixapi-balance-accesstoken"} {
		tpl, ok := templates.Get(name)
		if !ok {
			t.Fatalf("template %q missing", name)
		}
		if _, err := json.Marshal(tpl); err != nil {
			t.Fatalf("%s: not serializable: %v", name, err)
		}
		var found bool
		for _, step := range tpl.Pipeline.Steps {
			if step.Kind == "script.js.extract" && step.Name == "normalize access token" {
				found = true
				if src, _ := step.Config["source"].(string); !strings.Contains(src, "access_token") {
					t.Fatalf("%s: normalization script must read access_token", name)
				}
			}
		}
		if !found {
			t.Fatalf("%s: missing token normalization step", name)
		}
	}
}
