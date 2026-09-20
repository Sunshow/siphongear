package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/store/models"
)

const historyCleanupBatchSize = 100

// HistoryCleaner expires history at startup and every hour. One worker keeps
// cleanup passes from overlapping and stops with the server's context.
type HistoryCleaner struct {
	db            *gorm.DB
	retentionDays int
	interval      time.Duration
	start         sync.Once
	wg            sync.WaitGroup
}

func NewHistoryCleaner(db *gorm.DB, retentionDays int) *HistoryCleaner {
	return &HistoryCleaner{db: db, retentionDays: retentionDays, interval: time.Hour}
}

func (c *HistoryCleaner) Start(ctx context.Context) {
	c.start.Do(func() {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			ticker := time.NewTicker(c.interval)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				// Calculate in UTC so each retention day is 24 hours even across DST,
				// then use the same location as the timestamps written by the runner.
				cutoff := time.Now().UTC().AddDate(0, 0, -c.retentionDays).In(time.Local)
				if err := DeleteHistoryBefore(ctx, c.db, cutoff); err != nil {
					if ctx.Err() == nil {
						log.Error().Err(err).Msg("history cleanup failed")
					}
				} else {
					log.Info().Int("retention_days", c.retentionDays).Time("cutoff", cutoff).Msg("history cleanup completed")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (c *HistoryCleaner) Wait() { c.wg.Wait() }

// DeleteHistoryBefore removes expired runs with their step logs and data points,
// and expires notification delivery logs. Definitions and alert state are kept.
func DeleteHistoryBefore(ctx context.Context, db *gorm.DB, cutoff time.Time) error {
	for {
		var ids []uint64
		err := db.WithContext(ctx).Model(&models.Run{}).
			Where("started_at < ? AND status <> ?", cutoff, "running").
			// Keep a run that only just finished: its data points may still be saving.
			Where("finished_at IS NULL OR finished_at < ?", cutoff).
			Order("started_at asc, id asc").Limit(historyCleanupBatchSize).Pluck("id", &ids).Error
		if err != nil {
			return fmt.Errorf("select expired runs: %w", err)
		}
		if len(ids) == 0 {
			break
		}
		// Commit each batch independently; a failed delete cannot leave half a run.
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("run_id IN ?", ids).Delete(&models.StepLog{}).Error; err != nil {
				return err
			}
			if err := tx.Where("run_id IN ?", ids).Delete(&models.DataPoint{}).Error; err != nil {
				return err
			}
			return tx.Where("id IN ?", ids).Delete(&models.Run{}).Error
		}); err != nil {
			return fmt.Errorf("delete expired runs: %w", err)
		}
	}

	for {
		var ids []uint64
		if err := db.WithContext(ctx).Model(&models.NotificationLog{}).
			Where("created_at < ?", cutoff).Order("created_at asc, id asc").
			Limit(historyCleanupBatchSize).Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("select expired notification logs: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.NotificationLog{}).Error; err != nil {
			return fmt.Errorf("delete expired notification logs: %w", err)
		}
	}
}
