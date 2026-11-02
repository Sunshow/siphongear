package session_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/session"
	"github.com/sunshow/siphongear/internal/store"
	"github.com/sunshow/siphongear/internal/store/models"
)

func openSessionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "sessions.db") + "?_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newTestManager(t *testing.T, db *gorm.DB) *session.Manager {
	t.Helper()
	cipher, err := crypto.New("session-test-master-key-32-bytes!!")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return session.NewManager(db, cipher)
}

func TestManagerSaveLoadRoundTripEncryptsPayload(t *testing.T) {
	db := openSessionTestDB(t)
	manager := newTestManager(t, db)
	ctx := context.Background()

	state := session.NewState()
	state.CollectorID = 7
	state.UserAgent = "DeviceUA/1.0"
	state.Cookies = []session.Cookie{{Name: "sid", Value: "super-secret-cookie", Domain: "example.com", HostOnly: true, Path: "/"}}
	state.Vars["user_id"] = "69"
	state.LoginCount = 3
	loginAt := time.Now().Add(-time.Hour)
	state.LastLoginAt = &loginAt

	if err := manager.Save(ctx, "collector:7", state); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := manager.Load(ctx, "collector:7")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected session")
	}
	if loaded.UserAgent != "DeviceUA/1.0" || loaded.CollectorID != 7 || loaded.LoginCount != 3 {
		t.Fatalf("round-tripped state = %+v", loaded)
	}
	if len(loaded.Cookies) != 1 || loaded.Cookies[0].Value != "super-secret-cookie" {
		t.Fatalf("cookies = %+v", loaded.Cookies)
	}
	if loaded.Vars["user_id"] != "69" {
		t.Fatalf("vars = %+v", loaded.Vars)
	}
	if loaded.LastLoginAt == nil || !loaded.LastLoginAt.Equal(loginAt) {
		t.Fatalf("last login = %v", loaded.LastLoginAt)
	}

	var row models.Session
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("load raw row: %v", err)
	}
	for _, secret := range []string{"super-secret-cookie", "user_id", "DeviceUA"} {
		if strings.Contains(row.Payload, secret) {
			t.Fatalf("payload is not encrypted: %q found in %q", secret, row.Payload)
		}
	}
	if row.CookieCount != 1 || row.VarKeys != "user_id" || row.LoginCount != 3 {
		t.Fatalf("row metadata = %+v", row)
	}
}

func TestManagerSaveEmptyStateRemovesRow(t *testing.T) {
	db := openSessionTestDB(t)
	manager := newTestManager(t, db)
	ctx := context.Background()

	state := session.NewState()
	state.CollectorID = 1
	state.Vars["token"] = "abc"
	if err := manager.Save(ctx, "collector:1", state); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := manager.Save(ctx, "collector:1", session.NewState()); err != nil {
		t.Fatalf("save empty: %v", err)
	}
	var count int64
	if err := db.Model(&models.Session{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no rows, got %d", count)
	}
}

func TestManagerListAndDeleteByCollector(t *testing.T) {
	db := openSessionTestDB(t)
	manager := newTestManager(t, db)
	ctx := context.Background()

	for _, tc := range []struct {
		key         string
		collectorID uint
	}{
		{"collector:1", 1},
		{"collector:2", 2},
	} {
		state := session.NewState()
		state.CollectorID = tc.collectorID
		state.Vars["token"] = tc.key
		if err := manager.Save(ctx, tc.key, state); err != nil {
			t.Fatalf("save %s: %v", tc.key, err)
		}
	}

	rows, err := manager.List(ctx, session.ListFilter{CollectorID: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != "collector:2" || rows[0].Status != "active" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].VarKeys != "token" || rows[0].LastUsedAt == nil {
		t.Fatalf("row metadata = %+v", rows[0])
	}

	deleted, err := manager.DeleteByCollector(ctx, 1)
	if err != nil || deleted != 1 {
		t.Fatalf("delete by collector = %d, %v", deleted, err)
	}
	all, err := manager.List(ctx, session.ListFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 1 || all[0].Key != "collector:2" {
		t.Fatalf("remaining = %+v", all)
	}

	if err := manager.Delete(ctx, all[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := manager.Delete(ctx, all[0].ID); err != session.ErrNotFound {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
}

func TestManagerExpiredSessionStatus(t *testing.T) {
	db := openSessionTestDB(t)
	manager := newTestManager(t, db)
	ctx := context.Background()

	state := session.NewState()
	state.CollectorID = 3
	state.Vars["token"] = "t"
	past := time.Now().Add(-time.Minute)
	state.ExpiresAt = &past
	if err := manager.Save(ctx, "collector:3", state); err != nil {
		t.Fatalf("save: %v", err)
	}

	rows, err := manager.List(ctx, session.ListFilter{CollectorID: 3})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != "expired" {
		t.Fatalf("rows = %+v, want expired", rows)
	}
}

func TestManagerUnknownKeyReturnsNil(t *testing.T) {
	db := openSessionTestDB(t)
	manager := newTestManager(t, db)
	loaded, err := manager.Load(context.Background(), "collector:404")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil state, got %+v", loaded)
	}
}
