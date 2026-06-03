---
name: create-collector-template
description: Create a new built-in collector template for the SiphonGear platform. Use when adding a new service integration to monitor balances, credits, or metrics from an external website/API.
---

# Create Collector Template

## Quick start

To add a new built-in template:

1. Reverse-engineer the target API flow (login → fetch data → extract value)
2. Map the flow to pipeline steps using the [step catalog](REFERENCE.md#step-catalog)
3. Add a `Register(...)` call in `internal/templates/builtin.go`
4. Update the built-in templates table in `README.md`

## Workflow

### Phase 1: API discovery

Use browser tools (Playwright / curl) to capture the real HTTP requests:

1. Navigate to the login page and inspect the form
2. Capture the login `POST` request (URL, headers, body, response)
3. Identify authentication mechanism: token in JSON response, session cookie in `Set-Cookie`, or custom header
4. Find the page/endpoint that returns the target value (balance, credits, count)
5. Inspect the response format: JSON path, HTML pattern, or regex

### Phase 2: Pipeline design

Map the captured flow to SiphonGear steps following the [pipeline patterns](REFERENCE.md#pipeline-patterns):

| Flow step | SiphonGear steps |
|---|---|
| Load credentials | `input.credential` |
| Login request | `fetch.http` + `save_headers_as` |
| Parse login response | `parse.json` (optional) |
| Extract auth token/cookie | `script.js.extract` (regex on Set-Cookie) or `extract.jsonpath` |
| Fetch target data | `fetch.http` (with cookie/Authorization header) |
| Parse data response | `parse.json` |
| Extract target value | `extract.jsonpath` / `script.js.extract` (regex on HTML) |

### Phase 3: Code

Add a `Register(Template{...})` call at the end of `init()` in `internal/templates/builtin.go`. See [REFERENCE.md](REFERENCE.md#template-fields) for the full Template struct.

Key conventions:
- **Credential placeholder**: `"credential_id": 0` (frontend substitutes the real ID)
- **Runtime templates**: `{{.vars.cred.email}}`, `{{.vars.token}}`, `{{.vars.session_token}}` (Go text/template)
- **Frontend placeholders**: `{{BASE_URL}}` (substituted before saving)
- **Credential fields**: accessed as `{{.vars.<var_name>.<field>}}`, default var_name is `cred`
- **Response headers**: capture with `save_headers_as`, access in script via `payload.vars.<name>`
- **Response body**: capture with `save_body_as`, access in script via `payload.vars.<name>`

### Phase 4: Verify

1. Run `go build ./... && go vet ./...`
2. Test end-to-end with curl to confirm the exact regex/paths work on live data
3. Run `go test ./internal/pipeline/...` to ensure existing tests pass

### Phase 5: Document

Add the new template to the built-in templates table in `README.md`:

```
| `template-name` | Description | Credential fields | interval 30m |
```

## Examples

### Example: Token-based API (JSON)

See `deepseek-balance` in `internal/templates/builtin.go` — the simplest pattern:
API Key → GET /user/balance → JSONPath extract balance.

### Example: Cookie-based API (HTML scraping)

See `gpt2image-balance` in `internal/templates/builtin.go`:
Email+Password → POST login (Set-Cookie) → regex extract session_token → GET dashboard (Cookie) → regex extract balance from HTML.

### Example: Cookie-based API (JSON)

See `newapi-balance` in `internal/templates/builtin.go`:
Username+Password → POST login (Set-Cookie) → regex extract session cookie → GET /user/self (Cookie) → JSONPath extract balance.

## Advanced features

See [REFERENCE.md](REFERENCE.md) for:
- Full Template struct fields
- Complete step catalog with schema
- Pipeline patterns and edge cases
- Common regex patterns for cookie/HTML extraction
