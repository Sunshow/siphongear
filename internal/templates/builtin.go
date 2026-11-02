package templates

import (
	"github.com/sunshow/siphongear/internal/pipeline"
)

// newapiTokenNormalizeScript accepts the credential field names users commonly
// store a new-api access token under (access_token / api_key / token) and
// produces a clean `newapi_token` plus optional `newapi_uid` for later steps.
// NewAPI >= v1.0.0-rc.41 requires a scoped PAT (nap_ prefix); older builds used
// a legacy access token plus the New-Api-User header. Both work through this
// normalization, and a missing token fails with an actionable message instead
// of sending `Authorization: Bearer <no value>`.
const newapiTokenNormalizeScript = `var c = payload.vars.cred || {};
function pick(k) {
  var v = c[k];
  if (v === undefined || v === null) return "";
  return String(v).trim();
}
var token = pick("access_token") || pick("api_key") || pick("token");
if (/^Bearer[ \t]+/i.test(token)) token = token.replace(/^Bearer[ \t]+/i, "").trim();
if (!token) throw new Error("credential has no access_token/api_key - create a new-api access token in 个人设置 -> 安全设置 -> 访问令牌 (nap_ prefix, profile:read scope)");
var uid = pick("user_id") || pick("uid");
return { vars: { newapi_token: token, newapi_uid: uid } };`

// newapiSessionProbeScript flips vars.session_ok when the saved-cookie probe
// request did not actually return an authenticated NewAPI payload.
const newapiSessionProbeScript = `var status = Number(payload.vars.self_status || 0);
var ok = false;
if (status === 200) {
  try {
    var body = JSON.parse(String(payload.body || ""));
    ok = !!body && body.success === true;
  } catch (e) { ok = false; }
}
return { vars: { session_ok: ok } };`

// sub2apiSessionProbeScript flips vars.session_ok when the persisted token was
// rejected by the profile endpoint.
const sub2apiSessionProbeScript = `var status = Number(payload.vars.profile_status || 0);
var ok = false;
if (status === 200) {
  try {
    var body = JSON.parse(String(payload.body || ""));
    ok = !!body && body.data !== undefined && body.data !== null;
  } catch (e) { ok = false; }
}
return { vars: { session_ok: ok } };`

// udealproxySessionProbeScript flips vars.session_ok when the persisted token
// was rejected by the balance endpoint.
const udealproxySessionProbeScript = `var status = Number(payload.vars.balance_status || 0);
var ok = false;
if (status === 200) {
  try {
    var body = JSON.parse(String(payload.body || ""));
    ok = !!body && body.data !== undefined && body.data !== null;
  } catch (e) { ok = false; }
}
return { vars: { session_ok: ok } };`

// gpt2imageSessionProbeScript detects an expired better-auth session: the
// dashboard request "succeeds" but renders the sign-in page instead.
const gpt2imageSessionProbeScript = `var status = Number(payload.vars.dashboard_status || 0);
var html = String(payload.vars.dashboard_html || "");
return { vars: { session_ok: status === 200 && html.indexOf("Credits Balance") >= 0 } };`

func init() {
	Register(Template{
		Name:            "sub2api-balance",
		Description:     "sub2api 风格网关：复用持久化会话（cookie + token）优先免登录，会话失效才邮箱+密码重登 -> /api/v1/user/profile 取 balance",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "password",
			Fields: []TemplateCredentialField{
				{Name: "email", Label: "Email", Type: "text", Required: true, Placeholder: "user@example.com"},
				{Name: "password", Label: "Password", Type: "password", Required: true},
			},
		},
		ScheduleType: "interval",
		ScheduleSpec: "30m",
		Timeout:      30,
		Variables: []TemplateVariable{
			{Name: "base_url", Label: "Base URL", Placeholder: "http://example.com:port", Required: true},
		},
		Pipeline: pipeline.Definition{
			Steps: []pipeline.StepConfig{
				{Kind: "input.credential", Name: "load credential",
					Config: map[string]any{
						"credential_id": 0,
						"var_name":      "cred",
					}},
				{Kind: "session.load", Name: "restore session",
					Config: map[string]any{
						"require_vars": []any{"token"},
					}},
				{Kind: "fetch.http", Name: "fetch profile (saved session)", When: "vars.session_ok", OnError: "continue",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/v1/user/profile",
						"headers": map[string]any{
							"Authorization": "Bearer {{.vars.token}}",
						},
						"timeout":        15,
						"save_status_as": "profile_status",
					}},
				{Kind: "script.js.transform", Name: "evaluate saved session",
					Config: map[string]any{
						"source":     sub2apiSessionProbeScript,
						"timeout_ms": 2000,
					}},
				{Kind: "fetch.http", Name: "login", When: "!vars.session_ok",
					Config: map[string]any{
						"method":  "POST",
						"url":     "{{BASE_URL}}/api/v1/auth/login",
						"headers": map[string]any{"Content-Type": "application/json"},
						"body":    `{"email":"{{.vars.cred.email}}","password":"{{.vars.cred.password}}"}`,
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse login", When: "!vars.session_ok"},
				{Kind: "extract.jsonpath", Name: "extract token", When: "!vars.session_ok",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "token", "path": "$.data.access_token", "type": "string"},
						},
					}},
				{Kind: "session.save", Name: "remember session", When: "!vars.session_ok",
					Config: map[string]any{
						"vars":  []any{"token"},
						"login": true,
					}},
				{Kind: "fetch.http", Name: "fetch profile", When: "!vars.session_ok",
					Config: map[string]any{
						"method":  "GET",
						"url":     "{{BASE_URL}}/api/v1/user/profile",
						"headers": map[string]any{"Authorization": "Bearer {{.vars.token}}"},
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse profile"},
				{Kind: "extract.jsonpath", Name: "extract balance",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "balance", "path": "$.data.balance", "type": "number"},
						},
					}},
			},
			Indicators: []pipeline.IndicatorBind{{Key: "balance"}},
		},
		Indicators: []TemplateIndicator{
			{Key: "balance", Name: "余额", Type: "number", Display: "line"},
		},
	})

	Register(Template{
		Name:            "newapi-balance",
		Description:     "NewAPI (https://github.com/QuantumNous/new-api) 网关：复用持久化的 cookie 会话优先免登录，会话失效才用户名/密码重登 -> /api/user/self 取 quota，按 /api/status 的 quota_per_unit 折算成 USD",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "password",
			Fields: []TemplateCredentialField{
				{Name: "username", Label: "Username", Type: "text", Required: true, Placeholder: "your-username"},
				{Name: "password", Label: "Password", Type: "password", Required: true},
			},
		},
		ScheduleType: "interval",
		ScheduleSpec: "30m",
		Timeout:      30,
		Variables: []TemplateVariable{
			{Name: "base_url", Label: "Base URL", Placeholder: "http://host:3000", Required: true},
		},
		Pipeline: pipeline.Definition{
			Steps: []pipeline.StepConfig{
				{Kind: "input.credential", Name: "load credential",
					Config: map[string]any{
						"credential_id": 0,
						"var_name":      "cred",
					}},
				{Kind: "session.load", Name: "restore session",
					Config: map[string]any{
						"require_vars": []any{"user_id"},
					}},
				{Kind: "fetch.http", Name: "fetch self (saved session)", When: "vars.session_ok", OnError: "continue",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/user/self",
						"headers": map[string]any{
							"New-Api-User": "{{.vars.user_id}}",
						},
						"timeout":        15,
						"save_status_as": "self_status",
					}},
				{Kind: "script.js.transform", Name: "evaluate saved session",
					Config: map[string]any{
						"source":     newapiSessionProbeScript,
						"timeout_ms": 2000,
					}},
				{Kind: "fetch.http", Name: "login", When: "!vars.session_ok",
					Config: map[string]any{
						"method":  "POST",
						"url":     "{{BASE_URL}}/api/user/login",
						"headers": map[string]any{"Content-Type": "application/json"},
						"body":    `{"username":"{{.vars.cred.username}}","password":"{{.vars.cred.password}}"}`,
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse login", When: "!vars.session_ok"},
				{Kind: "extract.jsonpath", Name: "extract user_id", When: "!vars.session_ok",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "user_id", "path": "$.data.id", "type": "string"},
						},
					}},
				{Kind: "session.save", Name: "remember session", When: "!vars.session_ok",
					Config: map[string]any{
						"vars":  []any{"user_id"},
						"login": true,
					}},
				{Kind: "fetch.http", Name: "fetch self", When: "!vars.session_ok",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/user/self",
						"headers": map[string]any{
							"New-Api-User": "{{.vars.user_id}}",
						},
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse self"},
				{Kind: "extract.jsonpath", Name: "extract quota",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "quota_raw", "path": "$.data.quota", "type": "number"},
							map[string]any{"name": "used_quota_raw", "path": "$.data.used_quota", "type": "number"},
						},
					}},
				{Kind: "fetch.http", Name: "fetch status",
					Config: map[string]any{
						"method":  "GET",
						"url":     "{{BASE_URL}}/api/status",
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse status"},
				{Kind: "extract.jsonpath", Name: "extract quota_per_unit",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "quota_per_unit", "path": "$.data.quota_per_unit", "type": "number"},
						},
					}},
				{Kind: "script.js.extract", Name: "compute balance",
					Config: map[string]any{
						"source": `var qpu = Number(payload.vars.quota_per_unit) || 500000;
var q = Number(payload.vars.quota_raw) || 0;
var u = Number(payload.vars.used_quota_raw) || 0;
return { vars: {
    balance: q / qpu,
    used:    u / qpu,
} };`,
						"timeout_ms": 2000,
					}},
			},
			Indicators: []pipeline.IndicatorBind{
				{Key: "balance"},
				{Key: "used"},
			},
		},
		Indicators: []TemplateIndicator{
			{Key: "balance", Name: "余额", Type: "number", Unit: "USD", Display: "line"},
			{Key: "used", Name: "已用", Type: "number", Unit: "USD", Display: "line"},
		},
	})

	Register(Template{
		Name:            "newapi-balance-accesstoken",
		Description:     "NewAPI (https://github.com/QuantumNous/new-api) 网关：使用「访问令牌」直接调 /api/user/self，按 /api/status 的 quota_per_unit 折算成 USD。新版 NewAPI 需使用 nap_ 前缀的 scoped access token（scope: profile:read），旧版系统访问令牌已退役",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "token",
			Fields: []TemplateCredentialField{
				{Name: "access_token", Label: "Access Token", Type: "password", Required: true, Placeholder: "nap_...（新版：个人设置 → 安全设置 → 访问令牌，勾选 Profile 读取权限）"},
				{Name: "user_id", Label: "User ID", Type: "text", Required: false, Placeholder: "如 69（旧版 NewAPI 必填，新版可留空）"},
			},
		},
		ScheduleType: "interval",
		ScheduleSpec: "30m",
		Timeout:      30,
		Variables: []TemplateVariable{
			{Name: "base_url", Label: "Base URL", Placeholder: "http://host:3000", Required: true},
		},
		Pipeline: pipeline.Definition{
			Steps: []pipeline.StepConfig{
				{Kind: "input.credential", Name: "load credential",
					Config: map[string]any{
						"credential_id": 0,
						"var_name":      "cred",
					}},
				{Kind: "script.js.extract", Name: "normalize access token",
					Config: map[string]any{
						"source":     newapiTokenNormalizeScript,
						"timeout_ms": 2000,
					}},
				{Kind: "fetch.http", Name: "fetch self",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/user/self",
						"headers": map[string]any{
							"Authorization": "Bearer {{.vars.newapi_token}}",
							"New-Api-User":  "{{.vars.newapi_uid}}",
						},
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse self"},
				{Kind: "extract.jsonpath", Name: "extract quota",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "quota_raw", "path": "$.data.quota", "type": "number"},
							map[string]any{"name": "used_quota_raw", "path": "$.data.used_quota", "type": "number"},
						},
					}},
				{Kind: "fetch.http", Name: "fetch status",
					Config: map[string]any{
						"method":  "GET",
						"url":     "{{BASE_URL}}/api/status",
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse status"},
				{Kind: "extract.jsonpath", Name: "extract quota_per_unit",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "quota_per_unit", "path": "$.data.quota_per_unit", "type": "number"},
						},
					}},
				{Kind: "script.js.extract", Name: "compute balance",
					Config: map[string]any{
						"source": `var qpu = Number(payload.vars.quota_per_unit) || 500000;
var q = Number(payload.vars.quota_raw) || 0;
var u = Number(payload.vars.used_quota_raw) || 0;
return { vars: {
    balance: q / qpu,
    used:    u / qpu,
} };`,
						"timeout_ms": 2000,
					}},
			},
			Indicators: []pipeline.IndicatorBind{
				{Key: "balance"},
				{Key: "used"},
			},
		},
		Indicators: []TemplateIndicator{
			{Key: "balance", Name: "余额", Type: "number", Unit: "USD", Display: "line"},
			{Key: "used", Name: "已用", Type: "number", Unit: "USD", Display: "line"},
		},
	})

	Register(Template{
		Name:            "deepseek-balance",
		Description:     "DeepSeek 官方 API：使用 API Key 调 /user/balance 取余额（默认 CNY）",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "token",
			Fields: []TemplateCredentialField{
				{Name: "api_key", Label: "API Key", Type: "password", Required: true, Placeholder: "sk-xxxxxxxx (DeepSeek 控制台 -> API keys)"},
			},
		},
		ScheduleType: "interval",
		ScheduleSpec: "30m",
		Timeout:      30,
		Variables:    []TemplateVariable{},
		Pipeline: pipeline.Definition{
			Steps: []pipeline.StepConfig{
				{Kind: "input.credential", Name: "load credential",
					Config: map[string]any{
						"credential_id": 0,
						"var_name":      "cred",
					}},
				{Kind: "fetch.http", Name: "fetch balance",
					Config: map[string]any{
						"method": "GET",
						"url":    "https://api.deepseek.com/user/balance",
						"headers": map[string]any{
							"Authorization": "Bearer {{.vars.cred.api_key}}",
							"Accept":        "application/json",
						},
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse balance"},
				{Kind: "extract.jsonpath", Name: "extract balance",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "balance", "path": "$.balance_infos[0].total_balance", "type": "number"},
							map[string]any{"name": "granted", "path": "$.balance_infos[0].granted_balance", "type": "number"},
							map[string]any{"name": "topped_up", "path": "$.balance_infos[0].topped_up_balance", "type": "number"},
							map[string]any{"name": "currency", "path": "$.balance_infos[0].currency", "type": "string"},
							map[string]any{"name": "is_available", "path": "$.is_available", "type": "bool"},
						},
					}},
			},
			Indicators: []pipeline.IndicatorBind{
				{Key: "balance"},
				{Key: "granted"},
				{Key: "topped_up"},
			},
		},
		Indicators: []TemplateIndicator{
			{Key: "balance", Name: "余额", Type: "number", Unit: "CNY", Display: "line"},
			{Key: "granted", Name: "赠金", Type: "number", Unit: "CNY", Display: "line"},
			{Key: "topped_up", Name: "充值", Type: "number", Unit: "CNY", Display: "line"},
		},
	})

	Register(Template{
		Name:            "rixapi-balance-accesstoken",
		Description:     "RixAPI / 类 NewAPI 商业分支：使用「访问令牌」+ user_id 直接调 /api/user/self 取 balance（已是 USD）。新版分支的 scoped access token（nap_ 前缀）同样适用",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "token",
			Fields: []TemplateCredentialField{
				{Name: "access_token", Label: "Access Token", Type: "password", Required: true, Placeholder: "nap_...（新版：个人设置 → 安全设置 → 访问令牌）"},
				{Name: "user_id", Label: "User ID", Type: "text", Required: false, Placeholder: "如 12（旧版 NewAPI 必填，新版可留空）"},
			},
		},
		ScheduleType: "interval",
		ScheduleSpec: "30m",
		Timeout:      30,
		Variables: []TemplateVariable{
			{Name: "base_url", Label: "Base URL", Placeholder: "https://host", Required: true},
		},
		Pipeline: pipeline.Definition{
			Steps: []pipeline.StepConfig{
				{Kind: "input.credential", Name: "load credential",
					Config: map[string]any{
						"credential_id": 0,
						"var_name":      "cred",
					}},
				{Kind: "script.js.extract", Name: "normalize access token",
					Config: map[string]any{
						"source":     newapiTokenNormalizeScript,
						"timeout_ms": 2000,
					}},
				{Kind: "fetch.http", Name: "fetch self",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/user/self",
						"headers": map[string]any{
							"Authorization": "Bearer {{.vars.newapi_token}}",
							"New-Api-User":  "{{.vars.newapi_uid}}",
						},
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse self"},
				{Kind: "extract.jsonpath", Name: "extract balance",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "balance", "path": "$.data.balance", "type": "number"},
						},
					}},
			},
			Indicators: []pipeline.IndicatorBind{
				{Key: "balance"},
			},
		},
		Indicators: []TemplateIndicator{
			{Key: "balance", Name: "余额", Type: "number", Unit: "USD", Display: "line"},
		},
	})

	Register(Template{
		Name:            "udealproxy-balance",
		Description:     "UDealProxy 海外住宅代理：复用持久化 token 会话优先免登录，会话失效才邮箱+密码 (md5) 重登 -> /customer_wallet/get_balance 取住宅 GB 余额（十进制 GB）",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "password",
			Fields: []TemplateCredentialField{
				{Name: "email", Label: "Email", Type: "text", Required: true, Placeholder: "user@example.com"},
				{Name: "password", Label: "Password", Type: "password", Required: true},
			},
		},
		ScheduleType: "interval",
		ScheduleSpec: "30m",
		Timeout:      30,
		Variables:    []TemplateVariable{},
		Pipeline: pipeline.Definition{
			Steps: []pipeline.StepConfig{
				{Kind: "input.credential", Name: "load credential",
					Config: map[string]any{
						"credential_id": 0,
						"var_name":      "cred",
					}},
				{Kind: "session.load", Name: "restore session",
					Config: map[string]any{
						"require_vars": []any{"token"},
					}},
				{Kind: "fetch.http", Name: "fetch balance (saved session)", When: "vars.session_ok", OnError: "continue",
					Config: map[string]any{
						"method": "GET",
						"url":    "https://www.udealproxy.com/api/star-service-customer/customer_wallet/get_balance",
						"headers": map[string]any{
							"X-MCSHONG-AUTH-TOKEN": "{{.vars.token}}",
							"language":             "en",
						},
						"timeout":        15,
						"save_status_as": "balance_status",
					}},
				{Kind: "script.js.transform", Name: "evaluate saved session",
					Config: map[string]any{
						"source":     udealproxySessionProbeScript,
						"timeout_ms": 2000,
					}},
				{Kind: "script.js.input", Name: "md5 password", When: "!vars.session_ok",
					Config: map[string]any{
						"source":     `return { vars: { password_hash: crypto.md5(payload.vars.cred.password) } };`,
						"timeout_ms": 2000,
					}},
				{Kind: "fetch.http", Name: "login", When: "!vars.session_ok",
					Config: map[string]any{
						"method": "POST",
						"url":    "https://www.udealproxy.com/api/star-base-auth-server/customer_auth/login",
						"headers": map[string]any{
							"Content-Type": "application/json",
							"language":     "en",
						},
						"body":    `{"account":"{{.vars.cred.email}}","password":"{{.vars.password_hash}}"}`,
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse login", When: "!vars.session_ok"},
				{Kind: "extract.jsonpath", Name: "extract token", When: "!vars.session_ok",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "token", "path": "$.data.token", "type": "string"},
						},
					}},
				{Kind: "session.save", Name: "remember session", When: "!vars.session_ok",
					Config: map[string]any{
						"vars":  []any{"token"},
						"login": true,
					}},
				{Kind: "fetch.http", Name: "fetch balance", When: "!vars.session_ok",
					Config: map[string]any{
						"method": "GET",
						"url":    "https://www.udealproxy.com/api/star-service-customer/customer_wallet/get_balance",
						"headers": map[string]any{
							"X-MCSHONG-AUTH-TOKEN": "{{.vars.token}}",
							"language":             "en",
						},
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse balance"},
				{Kind: "extract.jsonpath", Name: "extract fields",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "netflow_bytes", "path": "$.data.netflowBalance", "type": "number"},
							map[string]any{"name": "plus_bytes", "path": "$.data.plusNetflowBalance", "type": "number"},
							map[string]any{"name": "transfer_bytes", "path": "$.data.transferNetflowRemain", "type": "number"},
							map[string]any{"name": "wallet", "path": "$.data.walletBalance", "type": "number"},
							map[string]any{"name": "ip_balance", "path": "$.data.ipBalance", "type": "number"},
						},
					}},
				{Kind: "script.js.extract", Name: "bytes -> GB",
					Config: map[string]any{
						"source": `var n = Number(payload.vars.netflow_bytes) || 0;
var p = Number(payload.vars.plus_bytes) || 0;
var t = Number(payload.vars.transfer_bytes) || 0;
return { vars: { balance_gb: (n + p + t) / 1e9 } };`,
						"timeout_ms": 2000,
					}},
			},
			Indicators: []pipeline.IndicatorBind{
				{Key: "balance_gb"},
				{Key: "wallet"},
				{Key: "ip_balance"},
			},
		},
		Indicators: []TemplateIndicator{
			{Key: "balance_gb", Name: "住宅余额", Type: "number", Unit: "GB", Display: "line"},
			{Key: "wallet", Name: "钱包", Type: "number", Display: "line"},
			{Key: "ip_balance", Name: "IP余额", Type: "number", Display: "line"},
		},
	})

	Register(Template{
		Name:            "gpt2image-balance",
		Description:     "GPT2IMAGE 积分查询：复用持久化的 better-auth cookie 会话优先免登录，会话失效才邮箱+密码重登，从 Dashboard HTML 提取可用积分（Credits Balance）",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "password",
			Fields: []TemplateCredentialField{
				{Name: "email", Label: "Email", Type: "text", Required: true, Placeholder: "user@example.com"},
				{Name: "password", Label: "Password", Type: "password", Required: true},
			},
		},
		ScheduleType: "interval",
		ScheduleSpec: "30m",
		Timeout:      30,
		Variables: []TemplateVariable{
			{Name: "base_url", Label: "Base URL", Default: "https://gpt2image.superapi.buzz", Placeholder: "https://gpt2image.superapi.buzz", Required: true},
		},
		Pipeline: pipeline.Definition{
			Steps: []pipeline.StepConfig{
				{Kind: "input.credential", Name: "load credential",
					Config: map[string]any{
						"credential_id": 0,
						"var_name":      "cred",
					}},
				{Kind: "session.load", Name: "restore session"},
				{Kind: "fetch.http", Name: "fetch dashboard (saved session)", When: "vars.session_ok", OnError: "continue",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/en/dashboard",
						"headers": map[string]any{
							"Accept": "text/html,application/xhtml+xml",
						},
						"timeout":        15,
						"save_body_as":   "dashboard_html",
						"save_status_as": "dashboard_status",
					}},
				{Kind: "script.js.transform", Name: "evaluate saved session",
					Config: map[string]any{
						"source":     gpt2imageSessionProbeScript,
						"timeout_ms": 2000,
					}},
				{Kind: "fetch.http", Name: "login", When: "!vars.session_ok",
					Config: map[string]any{
						"method":  "POST",
						"url":     "{{BASE_URL}}/api/auth/sign-in/email",
						"headers": map[string]any{"Content-Type": "application/json"},
						"body":    `{"email":"{{.vars.cred.email}}","password":"{{.vars.cred.password}}","callbackURL":"/dashboard"}`,
						"timeout": 15,
					}},
				{Kind: "session.save", Name: "remember login", When: "!vars.session_ok",
					Config: map[string]any{
						"login": true,
					}},
				{Kind: "fetch.http", Name: "fetch dashboard", When: "!vars.session_ok",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/en/dashboard",
						"headers": map[string]any{
							"Accept": "text/html,application/xhtml+xml",
						},
						"timeout":      15,
						"save_body_as": "dashboard_html",
					}},
				{Kind: "script.js.extract", Name: "extract balance",
					Config: map[string]any{
						"source": `var html = payload.vars.dashboard_html || "";
// Anchor on real DOM labels ("...</div>"), not marketing copy / RSC JSON that also contains these strings.
var m = html.match(/Credits Balance<\/div>[\s\S]*?data-slot="card-content"[^>]*>[\s\S]*?>([\d,]+(?:\.[\d]+)?)</)
  || html.match(/Credits Balance<\/div>[\s\S]*?font-serif text-3xl[^>]*>([\d,]+(?:\.[\d]+)?)</)
  || html.match(/Credits Balance<\/div>[\s\S]{0,2000}?>([\d,]+(?:\.[\d]+)?)</);
if (!m) throw new Error("balance not found in dashboard HTML");
var balance = Number(m[1].replace(/,/g, ''));
var m2 = html.match(/Images Generated<\/div>[\s\S]*?data-slot="card-content"[^>]*>[\s\S]*?>([\d,]+)</)
  || html.match(/Images Generated<\/div>[\s\S]*?font-serif text-3xl[^>]*>([\d,]+)</)
  || html.match(/Images Generated<\/div>[\s\S]{0,2000}?>([\d,]+)</);
var images = m2 ? Number(m2[1].replace(/,/g, '')) : 0;
return { vars: { balance: balance, images_generated: images } };`,
						"timeout_ms": 2000,
					}},
			},
			Indicators: []pipeline.IndicatorBind{
				{Key: "balance"},
				{Key: "images_generated"},
			},
		},
		Indicators: []TemplateIndicator{
			{Key: "balance", Name: "积分", Type: "number", Display: "line"},
			{Key: "images_generated", Name: "已生图", Type: "number", Display: "line"},
		},
	})
}
