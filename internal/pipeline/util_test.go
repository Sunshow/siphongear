package pipeline_test

import (
	"strings"
	"testing"

	"github.com/sunshow/siphongear/internal/pipeline"
)

func testPayload() *pipeline.Payload {
	p := pipeline.NewPayload()
	p.Vars["cred"] = map[string]any{
		"api_key":  "sk-test",
		"password": "hunter2",
	}
	return p
}

func TestRenderTemplate_MissingFieldFails(t *testing.T) {
	// A credential accidentally stored under api_key instead of access_token
	// used to render `Bearer <no value>` and produce a confusing 401 upstream.
	_, err := pipeline.RenderTemplate(`Bearer {{.vars.cred.access_token}}`, testPayload(), nil)
	if err == nil {
		t.Fatal("expected error for unresolved template variable")
	}
	if !strings.Contains(err.Error(), "unresolved variable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderTemplate_MissingNestedMapFails(t *testing.T) {
	p := pipeline.NewPayload()
	if _, err := pipeline.RenderTemplate(`{{.vars.cred.username}}`, p, nil); err == nil {
		t.Fatal("expected error when cred var is absent")
	}
}

func TestRenderTemplate_Resolved(t *testing.T) {
	out, err := pipeline.RenderTemplate(`Bearer {{.vars.cred.api_key}}`, testPayload(), nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "Bearer sk-test" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderTemplate_OptionalFallbacksStillWork(t *testing.T) {
	// `if` and `default` are the supported ways to reference optional values;
	// they must not trip the unresolved-variable check.
	out, err := pipeline.RenderTemplate(`{{default "fallback" .vars.cred.access_token}}`, testPayload(), nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "fallback" {
		t.Fatalf("got %q", out)
	}
	out, err = pipeline.RenderTemplate(`{{if .vars.cred.access_token}}x{{else}}y{{end}}`, testPayload(), nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "y" {
		t.Fatalf("got %q", out)
	}
}
