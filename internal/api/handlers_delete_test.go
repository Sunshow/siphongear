package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/events"
	"github.com/sunshow/siphongear/internal/scheduler"
	"github.com/sunshow/siphongear/internal/store"
	"github.com/sunshow/siphongear/internal/store/models"
)

type recordingCollectorCleaner struct {
	ids []uint
}

func (c *recordingCollectorCleaner) Enqueue(id uint) {
	c.ids = append(c.ids, id)
}

func TestDeleteCollectorSoftDeletesBeforeDeferredCleanup(t *testing.T) {
	db := openDeleteTestDB(t, 50*time.Millisecond)
	collector := models.Collector{Name: "delete me"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	indicator := models.Indicator{CollectorID: collector.ID, Key: "balance"}
	if err := db.Create(&indicator).Error; err != nil {
		t.Fatal(err)
	}
	run := models.Run{CollectorID: collector.ID, Trigger: "manual", Status: "success", StartedAt: time.Now()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StepLog{RunID: run.ID, Kind: "input.static"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.DataPoint{
		CollectorID: collector.ID,
		IndicatorID: indicator.ID,
		RunID:       run.ID,
		Ts:          time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	cleaner := &recordingCollectorCleaner{}
	server := &Server{
		DB:               db,
		Scheduler:        scheduler.New(db, nil, events.NewBus()),
		CollectorCleaner: cleaner,
	}
	recorder := deleteCollectorRequest(server, collector.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(cleaner.ids) != 1 || cleaner.ids[0] != collector.ID {
		t.Fatalf("queued collector ids = %v", cleaner.ids)
	}
	if err := db.First(&models.Collector{}, collector.ID).Error; err != gorm.ErrRecordNotFound {
		t.Fatalf("collector should be soft-deleted, got %v", err)
	}
	assertDeleteCount(t, db, &models.Run{}, "collector_id = ?", collector.ID, 1)
	assertDeleteCount(t, db, &models.StepLog{}, "run_id = ?", run.ID, 1)
	assertDeleteCount(t, db, &models.DataPoint{}, "collector_id = ?", collector.ID, 1)
	assertDeleteCount(t, db, &models.Indicator{}, "collector_id = ?", collector.ID, 1)
}

func TestDeleteCollectorAlreadyDeletedDoesNotWaitForWriteLock(t *testing.T) {
	db := openDeleteTestDB(t, 50*time.Millisecond)
	collector := models.Collector{Name: "already deleted"}
	if err := db.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&collector).Error; err != nil {
		t.Fatal(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK") //nolint:errcheck

	cleaner := &recordingCollectorCleaner{}
	server := &Server{
		DB:               db,
		Scheduler:        scheduler.New(db, nil, events.NewBus()),
		CollectorCleaner: cleaner,
	}
	recorder := deleteCollectorRequest(server, collector.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(cleaner.ids) != 1 || cleaner.ids[0] != collector.ID {
		t.Fatalf("queued collector ids = %v", cleaner.ids)
	}
}

func openDeleteTestDB(t *testing.T, busyTimeout time.Duration) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "delete.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(" + strconv.FormatInt(busyTimeout.Milliseconds(), 10) + ")"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func deleteCollectorRequest(server *Server, id uint) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/collectors/"+strconv.FormatUint(uint64(id), 10), nil)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(id), 10)}}
	server.deleteCollector(ctx)
	return recorder
}

func assertDeleteCount(t *testing.T, db *gorm.DB, model any, query string, arg any, want int64) {
	t.Helper()
	var got int64
	if err := db.Model(model).Where(query, arg).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%T count = %d, want %d", model, got, want)
	}
}
