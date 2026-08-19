package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/events"
	"github.com/sunshow/siphongear/internal/pipeline"
	"github.com/sunshow/siphongear/internal/store/models"
)

// Runner executes collectors and persists run/step/datapoint records.
type Runner struct {
	db     *gorm.DB
	cipher *crypto.Cipher
	bus    *events.Bus
	sem    chan struct{}
	mu     sync.Mutex
	active map[uint]chan struct{}
}

func New(db *gorm.DB, cipher *crypto.Cipher, bus *events.Bus, maxConcurrency int) *Runner {
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}
	return &Runner{
		db:     db,
		cipher: cipher,
		bus:    bus,
		sem:    make(chan struct{}, maxConcurrency),
		active: map[uint]chan struct{}{},
	}
}

// credResolver implements pipeline.CredentialResolver against the DB.
type credResolver struct {
	db     *gorm.DB
	cipher *crypto.Cipher
}

func (r *credResolver) Resolve(ctx context.Context, id uint) (map[string]any, error) {
	var c models.Credential
	if err := r.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	if c.Payload == "" {
		return map[string]any{}, nil
	}
	plain, err := r.cipher.DecryptString(c.Payload)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	var out map[string]any
	if err := sonic.UnmarshalString(plain, &out); err != nil {
		return nil, fmt.Errorf("parse credential: %w", err)
	}
	return out, nil
}

// stepRecorder writes StepLog entries for each step.
type stepRecorder struct {
	db    *gorm.DB
	runID uint64
}

func (r *stepRecorder) OnStepStart(index int, kind string) {}

func (r *stepRecorder) OnStepEnd(index int, kind string, dur time.Duration, snippet string, err error) {
	rec := &models.StepLog{
		RunID:      r.runID,
		Index:      index,
		Kind:       kind,
		Snippet:    truncate(snippet, 4000),
		DurationMS: dur.Milliseconds(),
	}
	if err != nil {
		rec.Error = err.Error()
	}
	if e := r.db.Create(rec).Error; e != nil {
		log.Warn().Err(e).Uint64("run_id", r.runID).Msg("save step log failed")
	}
}

// RunResult is returned from Trigger.
type RunResult struct {
	Run        *models.Run
	Result     *pipeline.Result
	StepLogs   []models.StepLog
}

// Trigger runs a collector by ID.
func (r *Runner) Trigger(ctx context.Context, collectorID uint, trigger string, params map[string]any, dryRun bool) (*RunResult, error) {
	r.mu.Lock()
	if _, running := r.active[collectorID]; running {
		r.mu.Unlock()
		return nil, fmt.Errorf("collector %d is already running", collectorID)
	}
	done := make(chan struct{})
	r.active[collectorID] = done
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.active, collectorID)
		close(done)
		r.mu.Unlock()
	}()

	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	var c models.Collector
	if err := r.db.WithContext(ctx).First(&c, collectorID).Error; err != nil {
		return nil, err
	}

	def := pipeline.Definition{}
	if c.PipelineJSON != "" {
		if err := sonic.UnmarshalString(c.PipelineJSON, &def); err != nil {
			return nil, fmt.Errorf("parse pipeline: %w", err)
		}
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}

	timeout := time.Duration(c.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	pipCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	run := &models.Run{
		CollectorID: collectorID,
		Trigger:     trigger,
		Status:      "running",
		StartedAt:   time.Now(),
	}
	if !dryRun {
		if err := r.db.Create(run).Error; err != nil {
			return nil, err
		}
	}

	logger := log.With().Uint64("run_id", run.ID).Uint("collector_id", collectorID).Logger()

	pCtx := &pipeline.Context{
		Context:     pipCtx,
		RunID:       run.ID,
		CollectorID: collectorID,
		Trigger:     trigger,
		Logger:      logger,
		Credentials: &credResolver{db: r.db, cipher: r.cipher},
		Now:         time.Now(),
	}
	if !dryRun {
		pCtx.StepListener = &stepRecorder{db: r.db, runID: run.ID}
	} else {
		pCtx.StepListener = &memListener{}
	}

	initial := pipeline.NewPayload()
	for k, v := range params {
		initial.Vars[k] = v
	}

	engine := pipeline.NewEngine()
	res, runErr := engine.Run(pCtx, def, initial)

	end := time.Now()
	run.FinishedAt = &end
	run.DurationMS = end.Sub(run.StartedAt).Milliseconds()
	if runErr != nil {
		run.Status = "failed"
		run.Error = runErr.Error()
	} else {
		run.Status = "success"
	}
	if !dryRun {
		_ = r.db.Save(run).Error
		_ = r.db.Model(&models.Collector{}).Where("id = ?", collectorID).Updates(map[string]any{
			"last_run_at": end,
			"last_status": run.Status,
		}).Error
		if runErr == nil && res != nil {
			r.persistDataPoints(collectorID, run.ID, end, res.Indicators)
			r.bus.Publish("run.completed", map[string]any{
				"collector_id": collectorID,
				"run_id":       run.ID,
				"status":       run.Status,
				"indicators":   res.Indicators,
			})
		}
	}

	out := &RunResult{Run: run, Result: res}
	if dryRun {
		if l, ok := pCtx.StepListener.(*memListener); ok {
			out.StepLogs = l.logs
		}
	}
	return out, runErr
}

// WaitInactive waits for a currently running Collector to finish. A Collector
// that is not active returns immediately.
func (r *Runner) WaitInactive(ctx context.Context, collectorID uint) error {
	r.mu.Lock()
	done, running := r.active[collectorID]
	r.mu.Unlock()
	if !running {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (r *Runner) persistDataPoints(collectorID uint, runID uint64, ts time.Time, indicators map[string]any) {
	if len(indicators) == 0 {
		return
	}
	var defs []models.Indicator
	if err := r.db.Where("collector_id = ?", collectorID).Find(&defs).Error; err != nil {
		log.Warn().Err(err).Msg("load indicators failed")
		return
	}
	byKey := map[string]models.Indicator{}
	for _, d := range defs {
		byKey[d.Key] = d
	}
	for key, val := range indicators {
		def, ok := byKey[key]
		if !ok {
			continue
		}
		dp := &models.DataPoint{
			CollectorID: collectorID,
			IndicatorID: def.ID,
			RunID:       runID,
			Ts:          ts,
		}
		switch v := val.(type) {
		case float64:
			dp.ValueNum = &v
		case int:
			f := float64(v)
			dp.ValueNum = &f
		case int64:
			f := float64(v)
			dp.ValueNum = &f
		case string:
			s := v
			dp.ValueStr = &s
		case bool:
			s := fmt.Sprint(v)
			dp.ValueStr = &s
		default:
			b, _ := sonic.Marshal(val)
			s := string(b)
			dp.ValueJSON = &s
		}
		if err := r.db.Create(dp).Error; err != nil {
			log.Warn().Err(err).Msg("save datapoint failed")
		}
	}
}

type memListener struct {
	logs []models.StepLog
}

func (m *memListener) OnStepStart(index int, kind string) {}

func (m *memListener) OnStepEnd(index int, kind string, dur time.Duration, snippet string, err error) {
	rec := models.StepLog{
		Index:      index,
		Kind:       kind,
		Snippet:    truncate(snippet, 4000),
		DurationMS: dur.Milliseconds(),
		CreatedAt:  time.Now(),
	}
	if err != nil {
		rec.Error = err.Error()
	}
	m.logs = append(m.logs, rec)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
