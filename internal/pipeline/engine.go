package pipeline

import (
	"fmt"
	"time"
)

// Engine runs a pipeline.
type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

// Result captures the final payload and indicator extraction.
type Result struct {
	Payload    *Payload
	Indicators map[string]any
}

func (e *Engine) Run(ctx *Context, def Definition, initial *Payload) (*Result, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	current := initial
	if current == nil {
		current = NewPayload()
	}
	for i, sc := range def.Steps {
		if !sc.IsEnabled() {
			continue
		}
		if sc.When != "" {
			ok, err := EvalCondition(sc.When, current)
			if err != nil {
				return nil, fmt.Errorf("step %d (%s) when: %w", i, sc.Kind, err)
			}
			if !ok {
				if ctx.StepListener != nil {
					ctx.StepListener.OnStepStart(i, sc.Kind)
					ctx.StepListener.OnStepEnd(i, sc.Kind, 0, "skipped (when=false)", nil)
				}
				continue
			}
		}
		step, err := Build(sc.Kind, sc.Config)
		if err != nil {
			return nil, fmt.Errorf("step %d build: %w", i, err)
		}
		if ctx.StepListener != nil {
			ctx.StepListener.OnStepStart(i, sc.Kind)
		}
		start := time.Now()
		next, runErr := step.Run(ctx, current)
		dur := time.Since(start)
		snippet := summarize(next)
		if runErr != nil {
			snippet = ""
		}
		if ctx.StepListener != nil {
			ctx.StepListener.OnStepEnd(i, sc.Kind, dur, snippet, runErr)
		}
		if runErr != nil {
			if sc.ContinueOnError() {
				// Keep the payload from before the failed step and expose the
				// failure to later steps / `when` expressions instead of aborting.
				if current.Vars == nil {
					current.Vars = map[string]any{}
				}
				current.Vars["last_error"] = runErr.Error()
				current.Vars["last_error_step"] = fmt.Sprintf("%d", i)
				continue
			}
			return nil, fmt.Errorf("step %d (%s): %w", i, sc.Kind, runErr)
		}
		current = next
	}

	res := &Result{Payload: current, Indicators: map[string]any{}}
	for _, ib := range def.Indicators {
		var v any
		var ok bool
		if ib.Path != "" {
			v, ok = lookupPath(current, ib.Path)
		} else {
			v, ok = current.Vars[ib.Key]
		}
		if ok {
			res.Indicators[ib.Key] = v
		}
	}
	return res, nil
}

func summarize(p *Payload) string {
	if p == nil {
		return ""
	}
	const max = 600
	if p.Body != nil && len(p.Body) > 0 {
		if len(p.Body) <= max {
			return string(p.Body)
		}
		return string(p.Body[:max]) + "..."
	}
	if p.Object != nil {
		return fmt.Sprintf("%v", p.Object)
	}
	return fmt.Sprintf("vars=%d meta=%d", len(p.Vars), len(p.Meta))
}
