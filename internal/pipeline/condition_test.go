package pipeline_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/sunshow/siphongear/internal/pipeline"
)

// failStep always errors; used to prove `when` skips and `on_error` continues.
type failStep struct{}

func (failStep) Kind() string { return "test.fail" }

func (failStep) Run(*pipeline.Context, *pipeline.Payload) (*pipeline.Payload, error) {
	return nil, fmt.Errorf("boom")
}

func init() {
	pipeline.Register(pipeline.StepMeta{Kind: "test.fail", Stage: "any", Description: "test step that always fails"},
		func(map[string]any) (pipeline.Step, error) { return failStep{}, nil })
}

type recordedStep struct {
	index   int
	kind    string
	snippet string
	err     error
}

type recordingListener struct {
	ends []recordedStep
}

func (l *recordingListener) OnStepStart(int, string) {}

func (l *recordingListener) OnStepEnd(index int, kind string, _ time.Duration, snippet string, err error) {
	l.ends = append(l.ends, recordedStep{index: index, kind: kind, snippet: snippet, err: err})
}

func newTestContext(listener pipeline.StepListener) *pipeline.Context {
	return &pipeline.Context{
		Context:      context.Background(),
		Logger:       zerolog.Nop(),
		Now:          time.Now(),
		StepListener: listener,
	}
}

func TestEvalCondition(t *testing.T) {
	payload := pipeline.NewPayload()
	payload.Vars["x"] = 3
	payload.Vars["ok"] = true
	payload.Body = []byte(`{"a":1}`)

	cases := []struct {
		expr string
		want bool
	}{
		{"vars.x > 2", true},
		{"vars.x === 3", true},
		{"payload.vars.ok", true},
		{"!vars.ok", false},
		{"vars.missing", false},
		{"payload.body.indexOf('\"a\"') >= 0", true},
		{"vars.x", true},
	}
	for _, tc := range cases {
		got, err := pipeline.EvalCondition(tc.expr, payload)
		if err != nil {
			t.Fatalf("%q: %v", tc.expr, err)
		}
		if got != tc.want {
			t.Fatalf("%q = %v, want %v", tc.expr, got, tc.want)
		}
	}

	if _, err := pipeline.EvalCondition("(", payload); err == nil {
		t.Fatal("expected syntax error")
	}
	if got, err := pipeline.EvalCondition("", payload); err != nil || !got {
		t.Fatalf("empty expression = %v, %v; want true", got, err)
	}
	if got, err := pipeline.EvalCondition("undefined", payload); err != nil || got {
		t.Fatalf("undefined = %v, %v; want false", got, err)
	}
}

func TestEngineWhenSkipsStep(t *testing.T) {
	def := pipeline.Definition{
		Steps: []pipeline.StepConfig{
			{Kind: "input.static", Config: map[string]any{"vars": map[string]any{"a": 1}}},
			{Kind: "test.fail", When: "vars.a !== 1"},
			{Kind: "input.static", Config: map[string]any{"vars": map[string]any{"b": 2}}},
		},
		Indicators: []pipeline.IndicatorBind{{Key: "a"}, {Key: "b"}},
	}
	listener := &recordingListener{}
	res, err := pipeline.NewEngine().Run(newTestContext(listener), def, pipeline.NewPayload())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Indicators["a"] != 1 || res.Indicators["b"] != 2 {
		t.Fatalf("indicators = %+v", res.Indicators)
	}
	if len(listener.ends) != 3 {
		t.Fatalf("listener recorded %d steps, want 3", len(listener.ends))
	}
	if listener.ends[1].snippet != "skipped (when=false)" || listener.ends[1].err != nil {
		t.Fatalf("skipped step record = %+v", listener.ends[1])
	}
}

func TestEngineWhenTrueExecutesAndFails(t *testing.T) {
	def := pipeline.Definition{
		Steps: []pipeline.StepConfig{
			{Kind: "input.static", Config: map[string]any{"vars": map[string]any{"a": 1}}},
			{Kind: "test.fail", When: "vars.a === 1"},
		},
	}
	if _, err := pipeline.NewEngine().Run(newTestContext(nil), def, pipeline.NewPayload()); err == nil {
		t.Fatal("expected failure")
	} else if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v", err)
	}
}

func TestEngineWhenErrorFailsRun(t *testing.T) {
	def := pipeline.Definition{
		Steps: []pipeline.StepConfig{{Kind: "input.static", When: "("}},
	}
	_, err := pipeline.NewEngine().Run(newTestContext(nil), def, pipeline.NewPayload())
	if err == nil || !strings.Contains(err.Error(), "when") {
		t.Fatalf("error = %v, want when condition error", err)
	}
}

func TestEngineOnErrorContinueKeepsPreviousPayload(t *testing.T) {
	def := pipeline.Definition{
		Steps: []pipeline.StepConfig{
			{Kind: "input.static", Config: map[string]any{"vars": map[string]any{"a": 1}}},
			{Kind: "test.fail", OnError: "continue"},
			{Kind: "input.static", Config: map[string]any{"vars": map[string]any{"b": 2}}},
		},
		Indicators: []pipeline.IndicatorBind{{Key: "a"}, {Key: "b"}},
	}
	listener := &recordingListener{}
	res, err := pipeline.NewEngine().Run(newTestContext(listener), def, pipeline.NewPayload())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Indicators["a"] != 1 || res.Indicators["b"] != 2 {
		t.Fatalf("indicators = %+v", res.Indicators)
	}
	if got, _ := res.Payload.Vars["last_error"].(string); !strings.Contains(got, "boom") {
		t.Fatalf("last_error = %q", got)
	}
	if res.Payload.Vars["last_error_step"] != "1" {
		t.Fatalf("last_error_step = %v", res.Payload.Vars["last_error_step"])
	}
	if len(listener.ends) != 3 || listener.ends[1].err == nil {
		t.Fatalf("listener should still record the failed step: %+v", listener.ends)
	}
}

func TestDefinitionValidateOnError(t *testing.T) {
	def := pipeline.Definition{Steps: []pipeline.StepConfig{{Kind: "input.static", OnError: "explode"}}}
	if err := def.Validate(); err == nil {
		t.Fatal("expected validation error for unknown on_error")
	}
	def.Steps[0].OnError = "continue"
	if err := def.Validate(); err != nil {
		t.Fatalf("continue should be valid: %v", err)
	}
}
