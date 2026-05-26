package templates

import (
	"github.com/sunshow/siphongear/internal/pipeline"
)

func init() {
	Register(Template{
		Name:            "sub2api-balance",
		Description:     "sub2api 风格网关：邮箱+密码登录 -> /api/v1/user/profile 取 balance",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "password",
			Fields: []TemplateCredentialField{
				{Name: "email", Label: "Email", Type: "text", Required: true, Placeholder: "user@example.com"},
				{Name: "password", Label: "Password", Type: "password", Required: true},
			},
		},
		ScheduleType:    "interval",
		ScheduleSpec:    "30m",
		Timeout:         30,
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
				{Kind: "fetch.http", Name: "login",
					Config: map[string]any{
						"method":  "POST",
						"url":     "{{BASE_URL}}/api/v1/auth/login",
						"headers": map[string]any{"Content-Type": "application/json"},
						"body":    `{"email":"{{.vars.cred.email}}","password":"{{.vars.cred.password}}"}`,
						"timeout": 15,
					}},
				{Kind: "parse.json", Name: "parse login"},
				{Kind: "extract.jsonpath", Name: "extract token",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "token", "path": "$.data.access_token", "type": "string"},
						},
					}},
				{Kind: "fetch.http", Name: "fetch profile",
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
		Description:     "NewAPI (https://github.com/QuantumNous/new-api) 网关：用户名/密码登录 (cookie session) -> /api/user/self 取 quota，按 /api/status 的 quota_per_unit 折算成 USD",
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
				{Kind: "fetch.http", Name: "login",
					Config: map[string]any{
						"method":          "POST",
						"url":             "{{BASE_URL}}/api/user/login",
						"headers":         map[string]any{"Content-Type": "application/json"},
						"body":            `{"username":"{{.vars.cred.username}}","password":"{{.vars.cred.password}}"}`,
						"timeout":         15,
						"save_headers_as": "login_headers",
					}},
				{Kind: "parse.json", Name: "parse login"},
				{Kind: "extract.jsonpath", Name: "extract user_id",
					Config: map[string]any{
						"mappings": []any{
							map[string]any{"name": "user_id", "path": "$.data.id", "type": "string"},
						},
					}},
				{Kind: "script.js.extract", Name: "extract session cookie",
					Config: map[string]any{
						"source": `var h = payload.vars.login_headers || {};
var raw = h["Set-Cookie"];
if (Array.isArray(raw)) raw = raw.join("; ");
raw = raw || "";
var m = raw.match(/session=([^;]+)/);
if (!m) throw new Error("session cookie not found in login response");
return { vars: { session: m[1] } };`,
						"timeout_ms": 2000,
					}},
				{Kind: "fetch.http", Name: "fetch self",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/user/self",
						"headers": map[string]any{
							"Cookie":       "session={{.vars.session}}",
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
		Description:     "NewAPI (https://github.com/QuantumNous/new-api) 网关：使用「访问密钥」+ user_id 直接调 /api/user/self，按 /api/status 的 quota_per_unit 折算成 USD",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "token",
			Fields: []TemplateCredentialField{
				{Name: "access_token", Label: "Access Token", Type: "password", Required: true, Placeholder: "用户中心 -> 个人设置 -> 系统访问令牌"},
				{Name: "user_id", Label: "User ID", Type: "text", Required: true, Placeholder: "如 12 (用户中心可见)"},
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
				{Kind: "fetch.http", Name: "fetch self",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/user/self",
						"headers": map[string]any{
							"Authorization": "Bearer {{.vars.cred.access_token}}",
							"New-Api-User":  "{{.vars.cred.user_id}}",
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
		Variables: []TemplateVariable{
			{Name: "base_url", Label: "Base URL", Default: "https://api.deepseek.com", Placeholder: "https://api.deepseek.com", Required: true},
		},
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
						"url":    "{{BASE_URL}}/user/balance",
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
		Description:     "RixAPI / 类 NewAPI 商业分支：使用「访问密钥」+ user_id 直接调 /api/user/self 取 balance（已是 USD）",
		NeedsCredential: true,
		CredentialHint: &TemplateCredentialHint{
			Type: "token",
			Fields: []TemplateCredentialField{
				{Name: "access_token", Label: "Access Token", Type: "password", Required: true, Placeholder: "用户中心 -> 个人设置 -> 系统访问令牌"},
				{Name: "user_id", Label: "User ID", Type: "text", Required: true, Placeholder: "如 12 (用户中心可见)"},
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
				{Kind: "fetch.http", Name: "fetch self",
					Config: map[string]any{
						"method": "GET",
						"url":    "{{BASE_URL}}/api/user/self",
						"headers": map[string]any{
							"Authorization": "Bearer {{.vars.cred.access_token}}",
							"New-Api-User":  "{{.vars.cred.user_id}}",
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
}
