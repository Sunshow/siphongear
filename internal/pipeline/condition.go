package pipeline

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// conditionTimeout bounds how long a step `when` expression may run.
const conditionTimeout = 3 * time.Second

// EvalCondition evaluates a step-level `when` JavaScript expression against the
// payload. The expression sees the same shape as script steps: `payload.vars`,
// `payload.meta`, `payload.body`, `payload.object`, plus bare `vars` and `meta`
// globals. Falsy results skip the step.
func EvalCondition(expr string, p *Payload) (bool, error) {
	if expr == "" {
		return true, nil
	}
	if p == nil {
		p = NewPayload()
	}
	rt := goja.New()
	rt.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	payload := map[string]any{
		"vars":   p.Vars,
		"meta":   p.Meta,
		"body":   string(p.Body),
		"object": p.Object,
	}
	_ = rt.Set("payload", payload)
	_ = rt.Set("vars", p.Vars)
	_ = rt.Set("meta", p.Meta)

	timer := time.AfterFunc(conditionTimeout, func() {
		rt.Interrupt(fmt.Errorf("condition timed out after %s", conditionTimeout))
	})
	defer timer.Stop()

	val, err := rt.RunString("(" + expr + ")")
	if err != nil {
		return false, err
	}
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return false, nil
	}
	return val.ToBoolean(), nil
}
