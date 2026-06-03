# Template Reference

## Template fields

```go
type Template struct {
    Name            string                  // unique identifier, e.g. "myapi-balance"
    Description     string                  // human-readable one-liner
    Source          string                  // auto-set to "builtin" by Register()
    NeedsCredential bool                    // set true if credential required
    CredentialHint  *TemplateCredentialHint // shape of credential payload
    ScheduleType    string                  // "none" | "interval" | "cron" | "event"
    ScheduleSpec    string                  // e.g. "30m", cron expression
    Timeout         int                     // seconds, typically 30
    Variables       []TemplateVariable      // user-supplied placeholders like base_url
    Pipeline        pipeline.Definition     // step pipeline + indicator binds
    Indicators      []TemplateIndicator     // metadata for indicator rows to create
}
```

### TemplateCredentialHint

```go
type TemplateCredentialHint struct {
    Type   string                      // "password" | "token" | "cookie" | "custom"
    Fields []TemplateCredentialField
}

type TemplateCredentialField struct {
    Name        string // field key in credential payload
    Label       string // display label in UI
    Type        string // "text" | "password"
    Required    bool
    Placeholder string
}
```

### TemplateVariable

```go
type TemplateVariable struct {
    Name        string // e.g. "base_url"
    Label       string
    Default     string
    Placeholder string
    Required    bool
}
```

### TemplateIndicator

```go
type TemplateIndicator struct {
    Key     string // matches IndicatorBind.Key in pipeline
    Name    string // display name
    Type    string // "number" | "string" | "bool" | "json"
    Unit    string // e.g. "USD", "CNY", "GB"
    Display string // "gauge" | "line" | "table"
}
```

### StepConfig

```go
type StepConfig struct {
    Kind    string         // registered step kind, e.g. "fetch.http"
    Name    string         // human-readable label for this step
    Config  map[string]any // step-specific configuration
    Enabled *bool          // omit for default true
}

type IndicatorBind struct {
    Key  string // vars key to persist as data point
    Path string // optional dotted path (when vars[key] is nested)
}
```

## Step catalog

### input.credential

Loads and decrypts a credential into `vars.<var_name>`.

```go
Config: map[string]any{
    "credential_id": 0,           // 0 = placeholder, frontend substitutes
    "var_name":      "cred",      // access via {{.vars.cred.email}}
}
```

### fetch.http

Makes an HTTP request via resty. All string configs support Go `text/template`.

```go
Config: map[string]any{
    "method":           "POST",
    "url":              "{{BASE_URL}}/api/login",
    "headers":          map[string]any{"Content-Type": "application/json"},
    "body":             `{"email":"{{.vars.cred.email}}"}`,
    "query":            map[string]string{},           // optional query params
    "timeout":          15,
    "save_body_as":     "resp_body",                   // optional, saves body string to vars
    "save_status_as":   "resp_status",                 // optional, saves status code
    "save_headers_as":  "resp_headers",                // optional, saves response headers map
    "insecure_tls":     false,
    "proxy":            "",                            // optional proxy URL
}
```

**Cookie auth**: To send cookies, set the `Cookie` header:
```go
"headers": map[string]any{
    "Cookie": "__Secure-better-auth.session_token={{.vars.session_token}}",
    "Accept": "text/html,application/xhtml+xml",
}
```

**Capturing Set-Cookie**: Use `save_headers_as` to capture response headers, then extract cookies in `script.js.extract`:
```javascript
var h = payload.vars.login_headers || {};
var raw = h["Set-Cookie"] || h["set-cookie"];
if (Array.isArray(raw)) raw = raw.join("; ");
var m = raw.match(/session_token=([^;]+)/);
```

### script.js.extract

Runs a goja JavaScript snippet. Has access to `payload.vars`, `payload.body`, `payload.meta`.

```go
Config: map[string]any{
    "source":     `return { vars: { key: value } };`,
    "timeout_ms": 2000,
}
```

**Must return**: `{ vars: { ... } }` — keys added to `payload.Vars` for subsequent steps.

### script.js.input

Same as script.js.extract but runs before any data fetching. Used for pre-computation (e.g. hashing password).

### parse.json

Parses `payload.Body` into `payload.Object` using sonic (JSON).

```go
Config: map[string]any{} // no config needed
```

### extract.jsonpath

Extracts values from `payload.Object` (parsed JSON) into `payload.Vars`.

```go
Config: map[string]any{
    "mappings": []any{
        map[string]any{"name": "balance", "path": "$.data.balance", "type": "number"},
    },
}
```

Supported types: `string`, `number`, `bool`, `json`.

### extract.regex

Extracts from `payload.Body` string using named capture groups.

```go
Config: map[string]any{
    "pattern": `balance[^\d]+(?P<balance>\d+)`,
}
```

### transform.template

Renders `payload.Body` through Go text/template with current payload context.

### Other steps

| Kind | Purpose |
|---|---|
| `input.static` | Inject literal values into vars |
| `fetch.browser` | chromedp browser rendering |
| `transform.gunzip` | gzip decompress |
| `transform.charset` | Charset conversion |
| `parse.html` | goquery HTML parsing |
| `extract.css` | CSS selector extraction |
| `script.js.transform` | JS sandbox modifying body/vars |
| `script.js.input` | JS sandbox pre-computation |

## Pipeline patterns

### Pattern A: Token-based JSON API

```
input.credential
    → fetch.http (with Authorization: Bearer token)
    → parse.json
    → extract.jsonpath
```

Examples: `deepseek-balance`, `newapi-balance-accesstoken`

### Pattern B: Cookie-based JSON API

```
input.credential
    → fetch.http (POST login, save_headers_as)
    → parse.json (parse login response)
    → extract.jsonpath (extract token)
    → fetch.http (GET data, with Authorization header)
    → parse.json
    → extract.jsonpath
```

Examples: `sub2api-balance`

### Pattern C: Cookie-based with session extraction

```
input.credential
    → fetch.http (POST login, save_headers_as)
    → script.js.extract (regex on Set-Cookie to get session token)
    → fetch.http (GET data, with Cookie header)
    → parse.json (or script.js.extract for HTML)
    → extract.jsonpath (or regex in script)
```

Examples: `newapi-balance`, `gpt2image-balance`

### Pattern D: Pre-computation before login

```
input.credential
    → script.js.input (e.g. md5 hash password)
    → fetch.http (POST login)
    → ...
```

Examples: `udealproxy-balance`

## Common regex snippets

### Extract session cookie from Set-Cookie header

```javascript
var h = payload.vars.login_headers || {};
var raw = h["Set-Cookie"] || h["set-cookie"];
if (Array.isArray(raw)) raw = raw.join("; ");
raw = raw || "";
var m = raw.match(/cookie_name=([^;]+)/);
if (!m) throw new Error("cookie not found");
return { vars: { session_token: m[1] } };
```

### Extract number from HTML by contextual class

```javascript
var html = payload.vars.dashboard_html || "";
var m = html.match(/Section Title[\s\S]*?class-name[^>]*>([\d,]+)</);
if (!m) throw new Error("value not found");
return { vars: { value: Number(m[1].replace(/,/g, '')) } };
```

### Parse multiple Set-Cookie values (array join)

The `save_headers_as` saves each header value. If multiple `Set-Cookie` headers exist, it's an array. Always handle both:
```javascript
if (Array.isArray(raw)) raw = raw.join("; ");
raw = raw || "";
```

## Credential conventions

- **Always use `credential_id: 0`** — frontend substitutes the real credential ID when the user applies the template
- **`var_name` default is `cred`** — credentials are accessed via `{{.vars.cred.field_name}}`
- **Field names in credential_hint must match** the field names in the credential payload and the `{{.vars.cred.X}}` references
- **Never hardcode real credentials** in template source — use `{{.vars.cred.X}}` placeholders

## Git conventions

- Add new template code in `internal/templates/builtin.go` at the end of `init()`
- Update README.md built-in templates table
- Do NOT commit test credentials or API keys
- Keep template names kebab-case: `service-name-balance`
