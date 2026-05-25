package templates

import (
	"sort"
	"sync"

	"github.com/sunshow/siphongear/internal/pipeline"
)

// TemplateIndicator predefines an indicator that the template wants to bind.
type TemplateIndicator struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Unit    string `json:"unit"`
	Display string `json:"display"`
}

// TemplateVariable describes a placeholder the user must supply when applying.
type TemplateVariable struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Default     string `json:"default,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required"`
}

// TemplateCredentialField describes one input field for a credential payload.
type TemplateCredentialField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"` // text | password
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
}

// TemplateCredentialHint suggests the credential type and shape when applying a template.
type TemplateCredentialHint struct {
	Type   string                    `json:"type"`   // password | token | cookie | custom
	Fields []TemplateCredentialField `json:"fields"`
}

// Template is a recipe that pre-fills a Collector pipeline.
type Template struct {
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	Source          string                  `json:"source,omitempty"` // builtin|user
	NeedsCredential bool                    `json:"needs_credential"`
	CredentialHint  *TemplateCredentialHint `json:"credential_hint,omitempty"`
	ScheduleType    string                  `json:"schedule_type"`
	ScheduleSpec    string                  `json:"schedule_spec"`
	Timeout         int                     `json:"timeout"`
	Variables       []TemplateVariable      `json:"variables"`
	Pipeline        pipeline.Definition     `json:"pipeline"`
	Indicators      []TemplateIndicator     `json:"indicators"`
}

var (
	mu       sync.RWMutex
	registry = map[string]Template{}
)

func Register(t Template) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[t.Name]; dup {
		panic("duplicate template name: " + t.Name)
	}
	t.Source = "builtin"
	registry[t.Name] = t
}

// BuiltinList returns only built-in templates.
func BuiltinList() []Template {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Template, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// BuiltinGet returns a built-in template by name.
func BuiltinGet(name string) (Template, bool) {
	mu.RLock()
	defer mu.RUnlock()
	t, ok := registry[name]
	return t, ok
}

func List() []Template {
	return BuiltinList()
}

func Get(name string) (Template, bool) {
	return BuiltinGet(name)
}
