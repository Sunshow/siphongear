package session

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func cookieNames(cookies []Cookie) []string {
	out := make([]string, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, c.Name+"="+c.Value)
	}
	return out
}

func TestMatchCookies_HostDomainPathSecureAndExpiry(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	state := &State{Cookies: []Cookie{
		{Name: "host", Value: "a", Domain: "example.com", HostOnly: true, Path: "/"},
		{Name: "domain", Value: "b", Domain: "example.com", Path: "/"},
		{Name: "apiPath", Value: "c", Domain: "example.com", Path: "/api"},
		{Name: "secure", Value: "d", Domain: "example.com", Path: "/", Secure: true},
		{Name: "expired", Value: "e", Domain: "example.com", Path: "/", Expires: now.Add(-time.Minute).Unix()},
	}}

	got := cookieNames(state.MatchCookies(mustURL(t, "http://example.com/api/self"), now))
	want := map[string]bool{"host=a": true, "domain=b": true, "apiPath=c": true}
	if len(got) != len(want) {
		t.Fatalf("cookies = %v, want %v", got, want)
	}
	for _, c := range got {
		if !want[c] {
			t.Fatalf("unexpected cookie %q in %v", c, got)
		}
	}

	// Host-only cookies must not leak to subdomains; domain cookies must.
	got = cookieNames(state.MatchCookies(mustURL(t, "https://app.example.com/other"), now))
	if len(got) != 2 {
		t.Fatalf("subdomain cookies = %v, want only domain+secure", got)
	}

	// HTTPS sends the secure cookie, HTTP must not.
	httpCookies := cookieNames(state.MatchCookies(mustURL(t, "http://example.com/"), now))
	for _, c := range httpCookies {
		if c == "secure=d" {
			t.Fatalf("secure cookie sent over http: %v", httpCookies)
		}
	}
	httpsCookies := cookieNames(state.MatchCookies(mustURL(t, "https://example.com/"), now))
	if len(httpsCookies) != 3 {
		t.Fatalf("https cookies = %v, want host+domain+secure", httpsCookies)
	}
}

func TestMergeSetCookies_AddUpdateDeleteAndDomainCheck(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	u := mustURL(t, "https://login.example.com/api/user/login")
	state := NewState()

	expires := now.Add(48 * time.Hour)
	state.MergeSetCookies(u, []*http.Cookie{
		{Name: "sid", Value: "one", Path: "/"},
		{Name: "device", Value: "dev-1", Path: "/"},
		{Name: "remember", Value: "yes", Path: "/", Expires: expires},
		{Name: "evil", Value: "x", Domain: "attacker.com", Path: "/"},
	}, now)
	if len(state.Cookies) != 3 {
		t.Fatalf("cookies = %+v, want 3", state.Cookies)
	}
	for _, c := range state.Cookies {
		if c.Name == "evil" {
			t.Fatalf("cookie for a foreign domain was accepted")
		}
	}

	// Update the same name/path in place.
	state.MergeSetCookies(u, []*http.Cookie{{Name: "sid", Value: "two", Path: "/"}}, now)
	if got := findCookie(state, "sid"); got == nil || got.Value != "two" {
		t.Fatalf("sid = %+v, want updated value", got)
	}

	// MaxAge<0 deletes.
	state.MergeSetCookies(u, []*http.Cookie{{Name: "sid", MaxAge: -1, Path: "/"}}, now)
	if got := findCookie(state, "sid"); got != nil {
		t.Fatalf("sid should be deleted, got %+v", got)
	}

	// Expired Set-Cookie is ignored/deleted.
	state.MergeSetCookies(u, []*http.Cookie{{Name: "device", Path: "/", Expires: now.Add(-time.Hour)}}, now)
	if got := findCookie(state, "device"); got != nil {
		t.Fatalf("expired cookie should be dropped, got %+v", got)
	}
}

func findCookie(state *State, name string) *Cookie {
	for i := range state.Cookies {
		if state.Cookies[i].Name == name {
			return &state.Cookies[i]
		}
	}
	return nil
}

func TestSetRequestCookies_OverridesSavedValues(t *testing.T) {
	now := time.Now()
	state := &State{Cookies: []Cookie{
		{Name: "sid", Value: "saved", Domain: "example.com", HostOnly: true, Path: "/"},
		{Name: "keep", Value: "k", Domain: "example.com", HostOnly: true, Path: "/"},
	}}
	state.SetRequestCookies(mustURL(t, "http://example.com/api/self"), "sid=explicit; extra=1")
	got := map[string]string{}
	for _, c := range state.MatchCookies(mustURL(t, "http://example.com/api/self"), now) {
		got[c.Name] = c.Value
	}
	if got["sid"] != "explicit" || got["extra"] != "1" || got["keep"] != "k" {
		t.Fatalf("merged cookies = %v", got)
	}
	if len(got) != 3 {
		t.Fatalf("expected exactly one header value per name, got %v", got)
	}
}

func TestStateExpiryAndData(t *testing.T) {
	now := time.Now()
	if (&State{}).HasData() {
		t.Fatal("empty state should not report data")
	}
	expired := now.Add(-time.Second)
	state := NewState()
	state.Vars["token"] = "t"
	state.ExpiresAt = &expired
	if !state.Expired(now) {
		t.Fatal("state should be expired")
	}
	future := now.Add(time.Hour)
	state.ExpiresAt = &future
	if state.Expired(now) {
		t.Fatal("state should not be expired")
	}
	if !state.HasData() {
		t.Fatal("vars-only state should report data")
	}
}

func TestJarImplementsHTTPCookieJar(t *testing.T) {
	now := time.Now()
	state := NewState()
	jar := &Jar{state: state, now: func() time.Time { return now }}
	u := mustURL(t, "https://example.com/login")
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "abc", Path: "/"}})
	cookies := jar.Cookies(mustURL(t, "https://example.com/dashboard"))
	if len(cookies) != 1 || cookies[0].Name != "sid" || cookies[0].Value != "abc" {
		t.Fatalf("jar cookies = %+v", cookies)
	}
}

func TestFormatCookieHeader(t *testing.T) {
	got := FormatCookieHeader(map[string]string{"b": "2", "a": "1"})
	if got != "a=1; b=2" {
		t.Fatalf("header = %q", got)
	}
}
