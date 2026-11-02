package pipeline

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// RenderTemplate evaluates a Go text/template against the payload + extra data.
func RenderTemplate(tpl string, p *Payload, extra map[string]any) (string, error) {
	if tpl == "" {
		return "", nil
	}
	if !strings.Contains(tpl, "{{") {
		return tpl, nil
	}
	t, err := template.New("tpl").Funcs(template.FuncMap{
		"now":     func() string { return time.Now().Format(time.RFC3339) },
		"unix":    func() int64 { return time.Now().Unix() },
		"default": func(d, v any) any { if v == nil || v == "" { return d }; return v },
	}).Parse(tpl)
	if err != nil {
		return "", err
	}
	data := map[string]any{
		"vars": p.Vars,
		"meta": p.Meta,
		"obj":  p.Object,
	}
	for k, v := range extra {
		data[k] = v
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	out := buf.String()
	// text/template renders a missing map key as the literal "<no value>"
	// instead of failing. Letting that string through turns config mistakes
	// (e.g. a credential without the referenced field) into confusing
	// upstream errors such as `Authorization: Bearer <no value>`. Fail loudly
	// instead; `{{if ...}}` and `default` still allow optional values.
	if strings.Contains(out, missingValuePlaceholder) {
		return "", fmt.Errorf("render template %q: unresolved variable, check that it exists in vars/meta", tpl)
	}
	return out, nil
}

// missingValuePlaceholder is what text/template writes for a missing map key.
const missingValuePlaceholder = "<no value>"

// CfgString reads a string config value, optionally rendering as template.
func CfgString(cfg map[string]any, key string) string {
	if v, ok := cfg[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	return ""
}

func CfgInt(cfg map[string]any, key string, def int) int {
	if v, ok := cfg[key]; ok {
		switch x := v.(type) {
		case int:
			return x
		case int64:
			return int(x)
		case float64:
			return int(x)
		case string:
			if n, err := strconv.Atoi(x); err == nil {
				return n
			}
		}
	}
	return def
}

func CfgBool(cfg map[string]any, key string, def bool) bool {
	if v, ok := cfg[key]; ok {
		switch x := v.(type) {
		case bool:
			return x
		case string:
			b, err := strconv.ParseBool(x)
			if err == nil {
				return b
			}
		}
	}
	return def
}

func CfgMap(cfg map[string]any, key string) map[string]any {
	if v, ok := cfg[key]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

func CfgStringMap(cfg map[string]any, key string) map[string]string {
	out := map[string]string{}
	if v, ok := cfg[key]; ok {
		if m, ok := v.(map[string]any); ok {
			for k, vv := range m {
				out[k] = fmt.Sprint(vv)
			}
		}
	}
	return out
}

func CfgSlice(cfg map[string]any, key string) []any {
	if v, ok := cfg[key]; ok {
		if a, ok := v.([]any); ok {
			return a
		}
	}
	return nil
}

// lookupPath resolves a dotted path "vars.foo.bar" / "obj.balance" against payload.
func lookupPath(p *Payload, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var cur any
	switch parts[0] {
	case "vars":
		cur = anyMap(p.Vars)
		parts = parts[1:]
	case "meta":
		cur = anyStringMap(p.Meta)
		parts = parts[1:]
	case "obj":
		cur = p.Object
		parts = parts[1:]
	default:
		cur = anyMap(p.Vars)
	}
	for _, k := range parts {
		switch m := cur.(type) {
		case map[string]any:
			v, ok := m[k]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(m) {
				return nil, false
			}
			cur = m[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func anyMap(m map[string]any) map[string]any { return m }
func anyStringMap(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
