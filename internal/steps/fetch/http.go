package fetch

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/sunshow/siphongear/internal/pipeline"
	sessionsvc "github.com/sunshow/siphongear/internal/session"
)

type httpStep struct {
	method      string
	urlTpl      string
	headers     map[string]string
	bodyTpl     string
	queryTpl    map[string]string
	timeout     int
	proxyURL    string
	saveBody    string // var name to save response body string under (default "")
	saveStatus  string // var name to save status code under
	saveHeader  string // var name to save response headers
	insecureTLS bool
	expectJSON  bool

	// Session reuse: persist cookies/UA per collector and replay them on later
	// runs so password-login sites do not see a new device every schedule tick.
	useSession    bool
	sessionKeyTpl string
	userAgentTpl  string
	markLogin     bool
}

func (s *httpStep) Kind() string { return "fetch.http" }

func (s *httpStep) Run(ctx *pipeline.Context, in *pipeline.Payload) (*pipeline.Payload, error) {
	urlStr, err := pipeline.RenderTemplate(s.urlTpl, in, nil)
	if err != nil {
		return nil, fmt.Errorf("render url: %w", err)
	}
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return nil, fmt.Errorf("url is required")
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("parse url %q: %w", urlStr, err)
	}

	client := resty.New()
	if s.timeout > 0 {
		client.SetTimeout(time.Duration(s.timeout) * time.Second)
	}
	if s.proxyURL != "" {
		client.SetProxy(s.proxyURL)
	}
	if s.insecureTLS {
		client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
	}

	now := time.Now()
	if !ctx.Now.IsZero() {
		now = ctx.Now
	}

	// Load persisted session state before the request so cookies can be replayed.
	var (
		sessKey         string
		state           *sessionsvc.State
		sessionActive   = s.useSession && ctx.Sessions != nil
		persistedCookie int
		reusedPersisted bool
	)
	if sessionActive {
		sessKey = s.resolveSessionKey(in, ctx)
		if sessKey == "" {
			sessionActive = false
		} else {
			loaded, loadErr := ctx.Sessions.Load(ctx, sessKey)
			if loadErr != nil {
				// Do not overwrite a session we failed to read; run without it.
				ctx.Logger.Warn().Err(loadErr).Str("session_key", sessKey).Msg("load session failed; continuing without session")
				sessionActive = false
			} else {
				state = loaded
				if state != nil {
					persistedCookie = len(state.MatchCookies(parsedURL, now))
					reusedPersisted = persistedCookie > 0
				}
			}
		}
	}
	var jar *sessionsvc.Jar
	if sessionActive {
		if state == nil {
			state = sessionsvc.NewState()
		}
		jar = sessionsvc.NewJar(state)
		client.SetCookieJar(jar)
	}

	req := client.R().SetContext(ctx)
	explicitCookie := ""
	for k, v := range s.headers {
		rendered, err := pipeline.RenderTemplate(v, in, nil)
		if err != nil {
			return nil, fmt.Errorf("render header %s: %w", k, err)
		}
		if jar != nil && strings.EqualFold(k, "Cookie") {
			// Explicit cookies are seeded into the jar instead of being set as a
			// header, so they cannot be duplicated by the jar's own Cookie header.
			explicitCookie = rendered
			continue
		}
		req.SetHeader(k, rendered)
	}
	if jar != nil && strings.TrimSpace(explicitCookie) != "" {
		jar.SetRequestCookies(parsedURL, explicitCookie)
	}

	// Reuse the persisted user agent when none is configured, keeping a stable
	// device fingerprint across runs.
	userAgent := ""
	if strings.TrimSpace(s.userAgentTpl) != "" {
		rendered, err := pipeline.RenderTemplate(s.userAgentTpl, in, nil)
		if err != nil {
			return nil, fmt.Errorf("render user_agent: %w", err)
		}
		userAgent = strings.TrimSpace(rendered)
	}
	reusedPersisted = reusedPersisted || (state != nil && state.UserAgent != "" && userAgent == "")
	if userAgent == "" && state != nil {
		userAgent = state.UserAgent
	}
	if userAgent != "" {
		req.SetHeader("User-Agent", userAgent)
	}

	for k, v := range s.queryTpl {
		rendered, err := pipeline.RenderTemplate(v, in, nil)
		if err != nil {
			return nil, fmt.Errorf("render query %s: %w", k, err)
		}
		req.SetQueryParam(k, rendered)
	}
	if s.bodyTpl != "" {
		rendered, err := pipeline.RenderTemplate(s.bodyTpl, in, nil)
		if err != nil {
			return nil, fmt.Errorf("render body: %w", err)
		}
		req.SetBody(rendered)
	}

	method := strings.ToUpper(s.method)
	if method == "" {
		method = "GET"
	}
	resp, err := req.Execute(method, urlStr)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, urlStr, err)
	}

	out := in.Clone()
	out.Body = append([]byte(nil), resp.Body()...)
	out.Meta["status"] = fmt.Sprintf("%d", resp.StatusCode())
	out.Meta["content_type"] = resp.Header().Get("Content-Type")
	if s.saveBody != "" {
		out.Vars[s.saveBody] = string(resp.Body())
	}
	if s.saveStatus != "" {
		out.Vars[s.saveStatus] = resp.StatusCode()
	}
	if s.saveHeader != "" {
		hdr := map[string]any{}
		for k, v := range resp.Header() {
			if len(v) == 1 {
				hdr[k] = v[0]
			} else {
				vals := make([]any, len(v))
				for i, vv := range v {
					vals[i] = vv
				}
				hdr[k] = vals
			}
		}
		out.Vars[s.saveHeader] = hdr
	}

	if sessionActive && state != nil {
		if userAgent == "" && resp.RawResponse != nil && resp.RawResponse.Request != nil {
			userAgent = resp.RawResponse.Request.Header.Get("User-Agent")
		}
		if userAgent != "" {
			state.UserAgent = userAgent
		}
		state.CollectorID = ctx.CollectorID
		if s.markLogin {
			state.LoginCount++
			loginAt := now
			state.LastLoginAt = &loginAt
		}
		if saveErr := ctx.Sessions.Save(ctx, sessKey, state); saveErr != nil {
			ctx.Logger.Warn().Err(saveErr).Str("session_key", sessKey).Msg("save session failed")
		}
		out.Vars["session_key"] = sessKey
		out.Vars["session_reused"] = reusedPersisted
		out.Vars["session_invalid"] = reusedPersisted && (resp.StatusCode() == 401 || resp.StatusCode() == 403)
		out.Vars["session_cookie_count"] = state.CookieCount(now)
	}

	if resp.StatusCode() >= 400 {
		return out, fmt.Errorf("http %d: %s", resp.StatusCode(), truncate(string(resp.Body()), 200))
	}
	return out, nil
}

// resolveSessionKey renders the configured key template, falling back to the
// runner-provided collector key.
func (s *httpStep) resolveSessionKey(in *pipeline.Payload, ctx *pipeline.Context) string {
	key := ""
	if strings.TrimSpace(s.sessionKeyTpl) != "" {
		if rendered, err := pipeline.RenderTemplate(s.sessionKeyTpl, in, nil); err == nil {
			key = strings.TrimSpace(rendered)
		}
	}
	if key == "" {
		key = strings.TrimSpace(ctx.SessionKey)
	}
	if len(key) > 191 {
		ctx.Logger.Warn().Str("session_key", key).Msg("session key too long; ignoring session")
		return ""
	}
	return key
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func newHTTP(cfg map[string]any) (pipeline.Step, error) {
	return &httpStep{
		method:        pipeline.CfgString(cfg, "method"),
		urlTpl:        pipeline.CfgString(cfg, "url"),
		headers:       pipeline.CfgStringMap(cfg, "headers"),
		bodyTpl:       pipeline.CfgString(cfg, "body"),
		queryTpl:      pipeline.CfgStringMap(cfg, "query"),
		timeout:       pipeline.CfgInt(cfg, "timeout", 30),
		proxyURL:      pipeline.CfgString(cfg, "proxy"),
		saveBody:      pipeline.CfgString(cfg, "save_body_as"),
		saveStatus:    pipeline.CfgString(cfg, "save_status_as"),
		saveHeader:    pipeline.CfgString(cfg, "save_headers_as"),
		insecureTLS:   pipeline.CfgBool(cfg, "insecure_tls", false),
		expectJSON:    pipeline.CfgBool(cfg, "expect_json", false),
		useSession:    pipeline.CfgBool(cfg, "use_session", true),
		sessionKeyTpl: pipeline.CfgString(cfg, "session_key"),
		userAgentTpl:  pipeline.CfgString(cfg, "user_agent"),
		markLogin:     pipeline.CfgBool(cfg, "mark_login", false),
	}, nil
}

func init() {
	pipeline.Register(pipeline.StepMeta{
		Kind:        "fetch.http",
		Stage:       "fetch",
		Description: "Perform an HTTP request via resty (templates supported in url/headers/body); reuses the persisted collector session by default",
		Schema: map[string]any{
			"method":          map[string]any{"type": "string", "label": "Method", "default": "GET"},
			"url":             map[string]any{"type": "string", "label": "URL", "required": true},
			"headers":         map[string]any{"type": "object", "label": "Headers"},
			"query":           map[string]any{"type": "object", "label": "Query params"},
			"body":            map[string]any{"type": "text", "label": "Body"},
			"timeout":         map[string]any{"type": "number", "label": "Timeout (s)", "default": 30},
			"proxy":           map[string]any{"type": "string", "label": "Proxy URL"},
			"save_body_as":    map[string]any{"type": "string", "label": "Save body as var"},
			"save_status_as":  map[string]any{"type": "string", "label": "Save status as var"},
			"save_headers_as": map[string]any{"type": "string", "label": "Save headers as var"},
			"insecure_tls":    map[string]any{"type": "boolean", "label": "Skip TLS verify"},
			"use_session": map[string]any{"type": "boolean", "label": "Reuse persisted session", "default": true,
				"hint": "Sends saved cookies / user agent and stores Set-Cookie from every response, per collector."},
			"session_key": map[string]any{"type": "string", "label": "Session key (optional)",
				"hint": "Defaults to collector:<id>. Set a shared key to reuse one session across collectors."},
			"user_agent": map[string]any{"type": "string", "label": "User agent (optional)",
				"hint": "Leave empty to reuse the user agent persisted on the first run."},
			"mark_login": map[string]any{"type": "boolean", "label": "Mark as login",
				"hint": "Counts this request as a (re)login in the session metadata."},
		},
	}, newHTTP)
}
