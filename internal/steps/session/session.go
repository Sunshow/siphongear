// Package session provides pipeline steps that load and persist the reusable
// login state managed by internal/session.
package session

import (
	"fmt"
	"strings"
	"time"

	"github.com/sunshow/siphongear/internal/pipeline"
	sessionsvc "github.com/sunshow/siphongear/internal/session"
)

const (
	varSessionOK         = "session_ok"
	varSessionKey        = "session_key"
	varSessionUpdatedAt  = "session_updated_at"
	varSessionLoginCount = "session_login_count"
	varSessionSaved      = "session_saved"
	maxSessionKeyLength  = 191
	sessionTimeFormat    = time.RFC3339
)

// loadStep restores persisted session metadata/vars and computes session_ok.
type loadStep struct {
	key     string
	require []string
}

func (s *loadStep) Kind() string { return "session.load" }

func (s *loadStep) Run(ctx *pipeline.Context, in *pipeline.Payload) (*pipeline.Payload, error) {
	if ctx.Sessions == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	key, err := resolveSessionKey(s.key, in, ctx)
	if err != nil {
		return nil, err
	}
	state, err := ctx.Sessions.Load(ctx, key)
	if err != nil {
		// A corrupt/unreadable session must not break collection: fall back to
		// "no session" so the pipeline can log in and overwrite it.
		ctx.Logger.Warn().Err(err).Str("session_key", key).Msg("load session failed; treating as empty")
		state = nil
	}

	out := in.Clone()
	now := runNow(ctx)
	ok := state != nil && state.HasData() && !state.Expired(now)
	if ok {
		for _, name := range s.require {
			if !hasNonEmptyVar(state.Vars, name) {
				ok = false
				break
			}
		}
	}

	out.Vars[varSessionKey] = key
	out.Vars[varSessionOK] = ok
	out.Vars[varSessionUpdatedAt] = ""
	out.Vars[varSessionLoginCount] = 0
	if state != nil {
		if !state.UpdatedAt.IsZero() {
			out.Vars[varSessionUpdatedAt] = state.UpdatedAt.Format(sessionTimeFormat)
		}
		out.Vars[varSessionLoginCount] = state.LoginCount
		for name, value := range state.Vars {
			out.Vars[name] = value
		}
	}
	ctx.Logger.Debug().
		Str("session_key", key).
		Bool("session_ok", ok).
		Msg("session loaded")
	return out, nil
}

// saveStep persists selected payload vars (plus already-captured cookies) for
// the next scheduled run.
type saveStep struct {
	key   string
	vars  []string
	ttl   time.Duration
	login bool
}

func (s *saveStep) Kind() string { return "session.save" }

func (s *saveStep) Run(ctx *pipeline.Context, in *pipeline.Payload) (*pipeline.Payload, error) {
	if ctx.Sessions == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	key, err := resolveSessionKey(s.key, in, ctx)
	if err != nil {
		return nil, err
	}
	state, err := ctx.Sessions.Load(ctx, key)
	if err != nil {
		ctx.Logger.Warn().Err(err).Str("session_key", key).Msg("load session failed; starting a fresh one")
		state = nil
	}
	if state == nil {
		state = sessionsvc.NewState()
	}
	state.CollectorID = ctx.CollectorID
	now := runNow(ctx)
	if s.login {
		state.LoginCount++
		loginAt := now
		state.LastLoginAt = &loginAt
		state.ExpiresAt = nil
	}
	if s.ttl > 0 {
		expiresAt := now.Add(s.ttl)
		state.ExpiresAt = &expiresAt
	}
	for _, name := range s.vars {
		value, exists := in.Vars[name]
		if !exists || value == nil {
			continue
		}
		if state.Vars == nil {
			state.Vars = map[string]any{}
		}
		state.Vars[name] = value
	}
	if err := ctx.Sessions.Save(ctx, key, state); err != nil {
		return nil, err
	}
	out := in.Clone()
	out.Vars[varSessionSaved] = true
	ctx.Logger.Debug().Str("session_key", key).Bool("login", s.login).Msg("session saved")
	return out, nil
}

func newLoad(cfg map[string]any) (pipeline.Step, error) {
	return &loadStep{
		key:     pipeline.CfgString(cfg, "key"),
		require: cfgStringSlice(cfg, "require_vars"),
	}, nil
}

func newSave(cfg map[string]any) (pipeline.Step, error) {
	ttl := time.Duration(0)
	if raw := strings.TrimSpace(pipeline.CfgString(cfg, "ttl")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("session.save: invalid ttl %q: %w", raw, err)
		}
		if parsed < 0 {
			return nil, fmt.Errorf("session.save: ttl must be positive")
		}
		ttl = parsed
	}
	return &saveStep{
		key:   pipeline.CfgString(cfg, "key"),
		vars:  cfgStringSlice(cfg, "vars"),
		ttl:   ttl,
		login: pipeline.CfgBool(cfg, "login", false),
	}, nil
}

// resolveSessionKey renders the optional key template, falling back to the
// collector default provided by the runner.
func resolveSessionKey(keyTpl string, in *pipeline.Payload, ctx *pipeline.Context) (string, error) {
	key := ""
	if strings.TrimSpace(keyTpl) != "" {
		rendered, err := pipeline.RenderTemplate(keyTpl, in, nil)
		if err != nil {
			return "", fmt.Errorf("render session key: %w", err)
		}
		key = strings.TrimSpace(rendered)
	}
	if key == "" {
		key = strings.TrimSpace(ctx.SessionKey)
	}
	if key == "" {
		return "", fmt.Errorf("session key is empty: set a runner context or a session_key")
	}
	if len(key) > maxSessionKeyLength {
		return "", fmt.Errorf("session key too long (%d > %d)", len(key), maxSessionKeyLength)
	}
	return key, nil
}

func runNow(ctx *pipeline.Context) time.Time {
	if ctx != nil && !ctx.Now.IsZero() {
		return ctx.Now
	}
	return time.Now()
}

func hasNonEmptyVar(vars map[string]any, name string) bool {
	if vars == nil {
		return false
	}
	value, ok := vars[name]
	if !ok || value == nil {
		return false
	}
	if s, isString := value.(string); isString {
		return strings.TrimSpace(s) != ""
	}
	return true
}

func cfgStringSlice(cfg map[string]any, key string) []string {
	raw := pipeline.CfgSlice(cfg, key)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(fmt.Sprint(item))
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func init() {
	pipeline.Register(pipeline.StepMeta{
		Kind:        "session.load",
		Stage:       "input",
		Description: "Restore persisted session metadata/vars and set vars.session_ok for conditional login steps",
		Schema: map[string]any{
			"key": map[string]any{"type": "string", "label": "Session key (optional)", "hint": "Defaults to collector:<id>. Template rendering is supported."},
			"require_vars": map[string]any{"type": "array", "label": "Required session vars",
				"hint": "Session is only considered usable when all of these vars were saved (e.g. user_id)."},
		},
	}, newLoad)

	pipeline.Register(pipeline.StepMeta{
		Kind:        "session.save",
		Stage:       "extract",
		Description: "Persist selected vars + captured cookies for the next run; mark login to track re-login counts",
		Schema: map[string]any{
			"key":   map[string]any{"type": "string", "label": "Session key (optional)", "hint": "Defaults to collector:<id>."},
			"vars":  map[string]any{"type": "array", "label": "Vars to persist", "hint": "Payload var names kept for the next run, e.g. token / user_id."},
			"ttl":   map[string]any{"type": "string", "label": "TTL (optional)", "hint": "Go duration like 168h. After it expires session.load reports session_ok=false."},
			"login": map[string]any{"type": "boolean", "label": "Mark as login", "hint": "Counts a successful (re)login and clears the TTL."},
		},
	}, newSave)
}
