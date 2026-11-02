// Package session persists reusable login state (cookies, derived vars and the
// user agent) for a collector so scheduled runs replay an existing session
// instead of logging in again. Sites with connection/device limits treat every
// fresh login as a new device; replaying the saved cookies keeps the device
// identity stable and lets pipelines only log in when the session is actually
// unusable.
package session

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// maxCookies bounds the persisted jar so a misbehaving site cannot grow the
// encrypted payload without limit.
const maxCookies = 128

// Cookie is the serialisable form of an http.Cookie. Expires is a Unix
// timestamp in seconds; 0 means "session cookie" (kept across runs on purpose,
// since persistence is the whole point).
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	HostOnly bool   `json:"host_only,omitempty"`
	Path     string `json:"path,omitempty"`
	Expires  int64  `json:"expires,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
	SameSite int    `json:"same_site,omitempty"`
}

// State is the decrypted content of one persisted session row.
type State struct {
	Cookies     []Cookie       `json:"cookies,omitempty"`
	Vars        map[string]any `json:"vars,omitempty"`
	UserAgent   string         `json:"user_agent,omitempty"`
	LoginCount  int            `json:"login_count,omitempty"`
	LastLoginAt *time.Time     `json:"last_login_at,omitempty"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`

	// CollectorID is filled from the owning row on Load and by the caller before
	// Save; it is not part of the encrypted payload.
	CollectorID uint `json:"-"`
}

// NewState returns an empty session state.
func NewState() *State {
	return &State{Vars: map[string]any{}}
}

// Clone returns a deep-enough copy (Vars hold JSON scalars in practice).
func (s *State) Clone() *State {
	if s == nil {
		return nil
	}
	out := &State{
		Cookies:     append([]Cookie(nil), s.Cookies...),
		Vars:        make(map[string]any, len(s.Vars)),
		UserAgent:   s.UserAgent,
		LoginCount:  s.LoginCount,
		LastLoginAt: cloneTime(s.LastLoginAt),
		ExpiresAt:   cloneTime(s.ExpiresAt),
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
		CollectorID: s.CollectorID,
	}
	for k, v := range s.Vars {
		out.Vars[k] = v
	}
	return out
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// HasData reports whether the state carries anything worth persisting.
func (s *State) HasData() bool {
	if s == nil {
		return false
	}
	return len(s.Cookies) > 0 || len(s.Vars) > 0
}

// Expired reports whether the optional session TTL has passed.
func (s *State) Expired(now time.Time) bool {
	if s == nil || s.ExpiresAt == nil {
		return false
	}
	return !s.ExpiresAt.After(now)
}

// VarKeys returns the sorted persisted variable names (for display).
func (s *State) VarKeys() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.Vars))
	for k := range s.Vars {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CookieCount returns the number of live cookies at now.
func (s *State) CookieCount(now time.Time) int {
	if s == nil {
		return 0
	}
	n := 0
	for _, c := range s.Cookies {
		if c.Expires == 0 || time.Unix(c.Expires, 0).After(now) {
			n++
		}
	}
	return n
}

// MatchCookies returns the cookies that should be sent to u, most specific path
// first (RFC 6265 ordering).
func (s *State) MatchCookies(u *url.URL, now time.Time) []Cookie {
	if s == nil || u == nil {
		return nil
	}
	host := canonicalHost(u)
	path := requestPath(u)
	var out []Cookie
	for _, c := range s.Cookies {
		if c.Name == "" {
			continue
		}
		if c.Expires != 0 && !time.Unix(c.Expires, 0).After(now) {
			continue
		}
		if c.Secure && !strings.EqualFold(u.Scheme, "https") {
			continue
		}
		if !domainMatches(host, c.Domain, c.HostOnly) {
			continue
		}
		if !pathMatches(path, cookiePath(c)) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := cookiePath(out[i]), cookiePath(out[j])
		if len(pi) != len(pj) {
			return len(pi) > len(pj)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// HTTPCookies is the http.Cookie view of MatchCookies, used by the jar.
func (s *State) HTTPCookies(u *url.URL, now time.Time) []*http.Cookie {
	matched := s.MatchCookies(u, now)
	out := make([]*http.Cookie, 0, len(matched))
	for _, c := range matched {
		out = append(out, &http.Cookie{Name: c.Name, Value: c.Value})
	}
	return out
}

// MergeSetCookies merges Set-Cookie response values into the state for the
// request URL u. Expired/removed cookies are dropped.
func (s *State) MergeSetCookies(u *url.URL, cookies []*http.Cookie, now time.Time) {
	if s == nil || u == nil || len(cookies) == 0 {
		return
	}
	host := canonicalHost(u)
	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		domain := canonicalDomain(c.Domain)
		hostOnly := domain == ""
		if hostOnly {
			domain = host
		} else if !domainMatches(host, domain, false) {
			// A site must not set cookies for a domain it does not own.
			continue
		}
		path := c.Path
		if path == "" || !strings.HasPrefix(path, "/") {
			path = defaultPath(u)
		}
		// MaxAge < 0 or a past Expires means delete.
		if c.MaxAge < 0 || (!c.Expires.IsZero() && !c.Expires.After(now)) {
			s.deleteCookie(c.Name, domain, path, now)
			continue
		}
		var expires int64
		switch {
		case c.MaxAge > 0:
			expires = now.Add(time.Duration(c.MaxAge) * time.Second).Unix()
		case !c.Expires.IsZero():
			expires = c.Expires.Unix()
		}
		s.setCookie(Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domain,
			HostOnly: hostOnly,
			Path:     path,
			Expires:  expires,
			Secure:   c.Secure,
			HTTPOnly: c.HttpOnly,
			SameSite: int(c.SameSite),
		})
	}
	s.prune(now)
}

// SetRequestCookies overwrites cookies from an explicit `Cookie` request header
// (legacy pipelines) so they take precedence over the saved jar.
func (s *State) SetRequestCookies(u *url.URL, rawHeader string) {
	if s == nil || u == nil || strings.TrimSpace(rawHeader) == "" {
		return
	}
	host := canonicalHost(u)
	path := requestPath(u)
	for _, pair := range strings.Split(rawHeader, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, value, ok := strings.Cut(pair, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		// Drop saved cookies with the same name that would be sent to u.
		kept := s.Cookies[:0]
		for _, existing := range s.Cookies {
			sameName := existing.Name == name && domainMatches(host, existing.Domain, existing.HostOnly) &&
				pathMatches(path, cookiePath(existing))
			if !sameName {
				kept = append(kept, existing)
			}
		}
		s.Cookies = kept
		s.setCookie(Cookie{
			Name:     name,
			Value:    strings.TrimSpace(value),
			Domain:   host,
			HostOnly: true,
			Path:     "/",
		})
	}
}

func (s *State) setCookie(c Cookie) {
	for i, existing := range s.Cookies {
		if existing.Name == c.Name && existing.Domain == c.Domain && cookiePath(existing) == cookiePath(c) {
			s.Cookies[i] = c
			return
		}
	}
	s.Cookies = append(s.Cookies, c)
}

func (s *State) deleteCookie(name, domain, path string, now time.Time) {
	kept := s.Cookies[:0]
	for _, existing := range s.Cookies {
		if existing.Name == name && existing.Domain == domain && cookiePath(existing) == path {
			continue
		}
		if existing.Expires != 0 && !time.Unix(existing.Expires, 0).After(now) {
			continue
		}
		kept = append(kept, existing)
	}
	s.Cookies = kept
}

func (s *State) prune(now time.Time) {
	if len(s.Cookies) == 0 {
		return
	}
	kept := s.Cookies[:0]
	for _, c := range s.Cookies {
		if c.Expires != 0 && !time.Unix(c.Expires, 0).After(now) {
			continue
		}
		kept = append(kept, c)
	}
	s.Cookies = kept
	if len(s.Cookies) > maxCookies {
		s.Cookies = s.Cookies[len(s.Cookies)-maxCookies:]
	}
}

// FormatCookieHeader renders name/value pairs as a Cookie request header.
func FormatCookieHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookies[name])
	}
	return strings.Join(parts, "; ")
}

// Jar implements http.CookieJar backed by a State, so resty's redirect
// handling captures Set-Cookie from every hop, not only the final response.
type Jar struct {
	state *State
	now   func() time.Time
}

// NewJar wraps state in a cookie jar. The state is mutated in place.
func NewJar(state *State) *Jar {
	if state == nil {
		state = NewState()
	}
	return &Jar{state: state, now: time.Now}
}

// State returns the wrapped state.
func (j *Jar) State() *State { return j.state }

// Cookies implements http.CookieJar.
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	return j.state.HTTPCookies(u, j.now())
}

// SetCookies implements http.CookieJar.
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.state.MergeSetCookies(u, cookies, j.now())
}

// SetRequestCookies seeds explicit header cookies for u (legacy pipelines).
func (j *Jar) SetRequestCookies(u *url.URL, rawHeader string) {
	j.state.SetRequestCookies(u, rawHeader)
}

func canonicalHost(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimSuffix(host, ".")
}

func canonicalDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(domain, ".")
	return strings.TrimSuffix(domain, ".")
}

func requestPath(u *url.URL) string {
	if u == nil || u.Path == "" {
		return "/"
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "/"
	}
	return u.Path
}

func cookiePath(c Cookie) string {
	if c.Path == "" {
		return "/"
	}
	return c.Path
}

// defaultPath computes the RFC 6265 default-path of a request URL.
func defaultPath(u *url.URL) string {
	path := requestPath(u)
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return "/"
	}
	return path[:i]
}

func domainMatches(host, domain string, hostOnly bool) bool {
	if domain == "" {
		return false
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	domain = canonicalDomain(domain)
	if host == domain {
		return true
	}
	if hostOnly {
		return false
	}
	return strings.HasSuffix(host, "."+domain)
}

func pathMatches(reqPath, cookiePath string) bool {
	if cookiePath == "" || cookiePath == "/" {
		return true
	}
	if reqPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(reqPath, cookiePath) {
		return false
	}
	if strings.HasSuffix(cookiePath, "/") {
		return true
	}
	return reqPath[len(cookiePath)] == '/'
}
