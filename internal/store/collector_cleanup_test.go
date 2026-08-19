package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/config"
	"github.com/sunshow/siphongear/internal/store/models"
)

func TestDeleteCollectorDataRemovesOnlyOwnedRows(t *testing.T) {
	db := openCleanupTestDB(t)
	target := models.Collector{Name: "target"}
	keep := models.Collector{Name: "keep"}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&keep).Error; err != nil {
		t.Fatal(err)
	}
	targetIndicator := models.Indicator{CollectorID: target.ID, Key: "target"}
	keepIndicator := models.Indicator{CollectorID: keep.ID, Key: "keep"}
	if err := db.Create(&targetIndicator).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&keepIndicator).Error; err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		run := models.Run{CollectorID: target.ID, Trigger: "manual", Status: "success", StartedAt: time.Now()}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.StepLog{RunID: run.ID, Kind: "input.static"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.DataPoint{
			CollectorID: target.ID,
			IndicatorID: targetIndicator.ID,
			RunID:       run.ID,
			Ts:          time.Now(),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	keepRun := models.Run{CollectorID: keep.ID, Trigger: "manual", Status: "success", StartedAt: time.Now()}
	if err := db.Create(&keepRun).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StepLog{RunID: keepRun.ID, Kind: "input.static"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.DataPoint{
		CollectorID: keep.ID,
		IndicatorID: keepIndicator.ID,
		RunID:       keepRun.ID,
		Ts:          time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.RuleNotificationState{RuleID: 1, IndicatorID: targetIndicator.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.RuleNotificationState{RuleID: 1, IndicatorID: keepIndicator.ID}).Error; err != nil {
		t.Fatal(err)
	}

	if err := deleteCollectorData(context.Background(), db, target.ID, 2); err != nil {
		t.Fatal(err)
	}

	assertCleanupCount(t, db, &models.Collector{}, "id = ?", target.ID, 1)
	assertCleanupCount(t, db, &models.Run{}, "collector_id = ?", target.ID, 0)
	assertCleanupCount(t, db, &models.DataPoint{}, "collector_id = ?", target.ID, 0)
	assertCleanupCountUnscoped(t, db, &models.Indicator{}, "collector_id = ?", target.ID, 0)
	assertCleanupCount(t, db, &models.RuleNotificationState{}, "indicator_id = ?", targetIndicator.ID, 0)

	assertCleanupCount(t, db, &models.Run{}, "collector_id = ?", keep.ID, 1)
	assertCleanupCount(t, db, &models.StepLog{}, "1 = ?", 1, 1)
	assertCleanupCount(t, db, &models.StepLog{}, "run_id = ?", keepRun.ID, 1)
	assertCleanupCount(t, db, &models.DataPoint{}, "collector_id = ?", keep.ID, 1)
	assertCleanupCount(t, db, &models.Indicator{}, "collector_id = ?", keep.ID, 1)
	assertCleanupCount(t, db, &models.RuleNotificationState{}, "indicator_id = ?", keepIndicator.ID, 1)
}

type blockingRunWaiter struct {
	started chan uint
	release chan struct{}
}

func (w *blockingRunWaiter) WaitInactive(ctx context.Context, id uint) error {
	w.started <- id
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.release:
		return nil
	}
}

func TestCollectorCleanerWaitsForActiveRun(t *testing.T) {
	db := openCleanupTestDB(t)
	collector := models.Collector{Name: "running"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	run := models.Run{CollectorID: collector.ID, Trigger: "manual", Status: "running", StartedAt: time.Now()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StepLog{RunID: run.ID, Kind: "input.static"}).Error; err != nil {
		t.Fatal(err)
	}

	waiter := &blockingRunWaiter{started: make(chan uint, 1), release: make(chan struct{})}
	cleaner := NewCollectorCleaner(db, waiter)
	ctx, cancel := context.WithCancel(context.Background())
	cleaner.Start(ctx)
	cleaner.Enqueue(collector.ID)

	select {
	case id := <-waiter.started:
		if id != collector.ID {
			t.Fatalf("waited for collector %d, want %d", id, collector.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("cleaner did not wait for the active run")
	}
	assertCleanupCount(t, db, &models.Run{}, "collector_id = ?", collector.ID, 1)

	close(waiter.release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var count int64
		if err := db.Model(&models.Run{}).Where("collector_id = ?", collector.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("collector history was not cleaned")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	cleaner.Wait()
	assertCleanupCount(t, db, &models.StepLog{}, "run_id = ?", run.ID, 0)
}

func TestPruneOrphansRemovesStepLogsInOnePass(t *testing.T) {
	db := openCleanupTestDB(t)
	collector := models.Collector{Name: "deleted"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	run := models.Run{CollectorID: collector.ID, Trigger: "manual", Status: "success", StartedAt: time.Now()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StepLog{RunID: run.ID, Kind: "input.static"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&collector).Error; err != nil {
		t.Fatal(err)
	}

	if err := PruneOrphans(db); err != nil {
		t.Fatal(err)
	}

	assertCleanupCount(t, db, &models.Run{}, "collector_id = ?", collector.ID, 0)
	assertCleanupCount(t, db, &models.StepLog{}, "run_id = ?", run.ID, 0)
}

func openCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    filepath.Join(t.TempDir(), "cleanup.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func assertCleanupCount(t *testing.T, db *gorm.DB, model any, query string, arg any, want int64) {
	t.Helper()
	var got int64
	if err := db.Model(model).Where(query, arg).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%T count = %d, want %d", model, got, want)
	}
}

func assertCleanupCountUnscoped(t *testing.T, db *gorm.DB, model any, query string, arg any, want int64) {
	t.Helper()
	var got int64
	if err := db.Unscoped().Model(model).Where(query, arg).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%T count = %d, want %d", model, got, want)
	}
}
