package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/store/models"
)

const collectorCleanupBatchSize = 500

// CollectorRunWaiter waits until a Collector no longer has an active run.
type CollectorRunWaiter interface {
	WaitInactive(context.Context, uint) error
}

// CollectorCleaner serializes deferred history cleanup so SQLite only sees
// short, bounded delete transactions from one cleanup worker at a time.
type CollectorCleaner struct {
	db      *gorm.DB
	waiter  CollectorRunWaiter
	logger  zerolog.Logger
	wake    chan struct{}
	mu      sync.Mutex
	queue   []uint
	queued  map[uint]struct{}
	start   sync.Once
	waitGrp sync.WaitGroup
}

func NewCollectorCleaner(db *gorm.DB, waiter CollectorRunWaiter) *CollectorCleaner {
	return &CollectorCleaner{
		db:     db,
		waiter: waiter,
		logger: log.With().Str("component", "collector.cleaner").Logger(),
		wake:   make(chan struct{}, 1),
		queued: make(map[uint]struct{}),
	}
}

func (c *CollectorCleaner) Start(ctx context.Context) {
	c.start.Do(func() {
		c.waitGrp.Add(1)
		go func() {
			defer c.waitGrp.Done()
			c.run(ctx)
		}()
	})
}

// Enqueue is non-blocking and deduplicates cleanup requests by Collector ID.
func (c *CollectorCleaner) Enqueue(id uint) {
	if id == 0 {
		return
	}
	c.mu.Lock()
	if _, exists := c.queued[id]; exists {
		c.mu.Unlock()
		return
	}
	c.queued[id] = struct{}{}
	c.queue = append(c.queue, id)
	c.mu.Unlock()

	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *CollectorCleaner) Wait() { c.waitGrp.Wait() }

func (c *CollectorCleaner) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		id, ok := c.next()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-c.wake:
			}
			continue
		}

		if err := c.cleanupWithRetry(ctx, id); err != nil && ctx.Err() == nil {
			c.logger.Error().Err(err).Uint("collector_id", id).Msg("collector cleanup stopped")
		}
		c.complete(id)
	}
}

func (c *CollectorCleaner) next() (uint, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) == 0 {
		return 0, false
	}
	id := c.queue[0]
	c.queue = c.queue[1:]
	return id, true
}

func (c *CollectorCleaner) complete(id uint) {
	c.mu.Lock()
	delete(c.queued, id)
	c.mu.Unlock()
}

func (c *CollectorCleaner) cleanupWithRetry(ctx context.Context, id uint) error {
	if c.waiter != nil {
		if err := c.waiter.WaitInactive(ctx, id); err != nil {
			return err
		}
	}

	delay := 250 * time.Millisecond
	for {
		err := DeleteCollectorData(ctx, c.db, id)
		if err == nil {
			c.logger.Info().Uint("collector_id", id).Msg("collector history cleaned")
			return nil
		}
		c.logger.Warn().Err(err).Uint("collector_id", id).Dur("retry_in", delay).Msg("collector cleanup failed")

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
}

func DeleteCollectorData(ctx context.Context, db *gorm.DB, collectorID uint) error {
	return deleteCollectorData(ctx, db, collectorID, collectorCleanupBatchSize)
}

func deleteCollectorData(ctx context.Context, db *gorm.DB, collectorID uint, batchSize int) error {
	if collectorID == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = collectorCleanupBatchSize
	}
	if err := deleteCollectorStepLogs(ctx, db, collectorID, batchSize); err != nil {
		return fmt.Errorf("delete step logs: %w", err)
	}
	if err := deleteCollectorDataPoints(ctx, db, collectorID, batchSize); err != nil {
		return fmt.Errorf("delete data points: %w", err)
	}
	if err := deleteCollectorRuns(ctx, db, collectorID, batchSize); err != nil {
		return fmt.Errorf("delete runs: %w", err)
	}
	if err := deleteCollectorIndicators(ctx, db, collectorID, batchSize); err != nil {
		return fmt.Errorf("delete indicators: %w", err)
	}
	return nil
}

func deleteCollectorStepLogs(ctx context.Context, db *gorm.DB, collectorID uint, batchSize int) error {
	for {
		var ids []uint64
		err := db.WithContext(ctx).
			Model(&models.StepLog{}).
			Joins("JOIN runs ON runs.id = step_logs.run_id").
			Where("runs.collector_id = ?", collectorID).
			Order("step_logs.id").
			Limit(batchSize).
			Pluck("step_logs.id", &ids).Error
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.StepLog{}).Error; err != nil {
			return err
		}
	}
}

func deleteCollectorDataPoints(ctx context.Context, db *gorm.DB, collectorID uint, batchSize int) error {
	for {
		var ids []uint64
		err := db.WithContext(ctx).
			Model(&models.DataPoint{}).
			Where("collector_id = ?", collectorID).
			Order("id").
			Limit(batchSize).
			Pluck("id", &ids).Error
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.DataPoint{}).Error; err != nil {
			return err
		}
	}
}

func deleteCollectorRuns(ctx context.Context, db *gorm.DB, collectorID uint, batchSize int) error {
	for {
		var ids []uint64
		err := db.WithContext(ctx).
			Model(&models.Run{}).
			Where("collector_id = ?", collectorID).
			Order("id").
			Limit(batchSize).
			Pluck("id", &ids).Error
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.Run{}).Error; err != nil {
			return err
		}
	}
}

func deleteCollectorIndicators(ctx context.Context, db *gorm.DB, collectorID uint, batchSize int) error {
	for {
		var ids []uint
		err := db.WithContext(ctx).
			Unscoped().
			Model(&models.Indicator{}).
			Where("collector_id = ?", collectorID).
			Order("id").
			Limit(batchSize).
			Pluck("id", &ids).Error
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.WithContext(ctx).Where("indicator_id IN ?", ids).Delete(&models.RuleNotificationState{}).Error; err != nil {
			return err
		}
		if err := db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&models.Indicator{}).Error; err != nil {
			return err
		}
	}
}
