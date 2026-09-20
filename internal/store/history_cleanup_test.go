package store

import (
	"context"
	"testing"
	"time"

	"github.com/sunshow/siphongear/internal/store/models"
)

func TestDeleteHistoryBefore(t *testing.T) {
	db := openCleanupTestDB(t)
	cutoff := time.Date(2026, 9, 13, 12, 0, 0, 0, time.Local)
	old := cutoff.Add(-time.Hour)
	recent := cutoff.Add(time.Hour)
	collector := models.Collector{Name: "keep definitions"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	indicator := models.Indicator{CollectorID: collector.ID, Key: "balance"}
	if err := db.Create(&indicator).Error; err != nil {
		t.Fatal(err)
	}
	state := models.RuleNotificationState{RuleID: 1, IndicatorID: indicator.ID, LastEventAt: old}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	// More than one batch, including failed and legacy runs without finished_at.
	for i := 0; i < historyCleanupBatchSize+1; i++ {
		run := models.Run{CollectorID: collector.ID, Status: "success", StartedAt: old, FinishedAt: &old}
		if i == 0 {
			run.Status = "failed"
			run.FinishedAt = nil
		}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.StepLog{RunID: run.ID, CreatedAt: old}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.DataPoint{CollectorID: collector.ID, IndicatorID: indicator.ID, RunID: run.ID, Ts: old}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.NotificationLog{CreatedAt: old}).Error; err != nil {
			t.Fatal(err)
		}
	}
	keep := []models.Run{
		{CollectorID: collector.ID, Status: "success", StartedAt: cutoff, FinishedAt: &cutoff},
		{CollectorID: collector.ID, Status: "failed", StartedAt: recent, FinishedAt: &recent},
		{CollectorID: collector.ID, Status: "running", StartedAt: old},
		{CollectorID: collector.ID, Status: "success", StartedAt: old, FinishedAt: &recent},
	}
	for i := range keep {
		if err := db.Create(&keep[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.StepLog{RunID: keep[i].ID, CreatedAt: keep[i].StartedAt}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.DataPoint{CollectorID: collector.ID, IndicatorID: indicator.ID, RunID: keep[i].ID, Ts: keep[i].StartedAt}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.NotificationLog{CreatedAt: cutoff}).Error; err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ { // Repeated cleanup must be safe.
		if err := DeleteHistoryBefore(context.Background(), db, cutoff); err != nil {
			t.Fatal(err)
		}
		for _, model := range []any{&models.Run{}, &models.StepLog{}, &models.DataPoint{}} {
			assertCleanupCount(t, db, model, "1 = ?", 1, int64(len(keep)))
		}
		assertCleanupCount(t, db, &models.NotificationLog{}, "1 = ?", 1, 1)
		assertCleanupCount(t, db, &models.Collector{}, "id = ?", collector.ID, 1)
		assertCleanupCount(t, db, &models.Indicator{}, "id = ?", indicator.ID, 1)
		assertCleanupCount(t, db, &models.RuleNotificationState{}, "indicator_id = ?", indicator.ID, 1)
		for _, run := range keep {
			assertCleanupCount(t, db, &models.Run{}, "id = ?", run.ID, 1)
		}
	}
}

func TestHistoryCleanupRollsBackFailedBatch(t *testing.T) {
	db := openCleanupTestDB(t)
	old := time.Now().AddDate(0, 0, -8)
	run := models.Run{Status: "success", StartedAt: old, FinishedAt: &old}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StepLog{RunID: run.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.DataPoint{RunID: run.ID, Ts: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER block_history_delete BEFORE DELETE ON runs BEGIN SELECT RAISE(ABORT, 'delete blocked'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := DeleteHistoryBefore(context.Background(), db, time.Now()); err == nil {
		t.Fatal("expected cleanup failure")
	}
	assertCleanupCount(t, db, &models.Run{}, "id = ?", run.ID, 1)
	assertCleanupCount(t, db, &models.StepLog{}, "run_id = ?", run.ID, 1)
	assertCleanupCount(t, db, &models.DataPoint{}, "run_id = ?", run.ID, 1)
}

func TestHistoryCleanerStartupPeriodicAndShutdown(t *testing.T) {
	db := openCleanupTestDB(t)
	cleaner := NewHistoryCleaner(db, 14)
	cleaner.interval = 20 * time.Millisecond
	old := time.Now().AddDate(0, 0, -15)
	recent := models.Run{Status: "success", StartedAt: time.Now().AddDate(0, 0, -10)}
	if err := db.Create(&recent).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); cleaner.Wait() }()
	for i := 0; i < 2; i++ {
		run := models.Run{Status: "success", StartedAt: old, FinishedAt: &old}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			cleaner.Start(ctx)
			cleaner.Start(ctx)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			var count int64
			if err := db.Model(&models.Run{}).Where("id = ?", run.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("expired run was not cleaned")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	done := make(chan struct{})
	go func() { cleaner.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleaner did not stop")
	}
	assertCleanupCount(t, db, &models.Run{}, "id = ?", recent.ID, 1)
	if err := DeleteHistoryBefore(ctx, db, time.Now()); err == nil {
		t.Fatal("expected cancellation error")
	}
}
