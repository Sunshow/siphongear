package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/session"
	"github.com/sunshow/siphongear/internal/store/models"
)

func newSessionTestServer(t *testing.T) (*Server, *session.Manager) {
	t.Helper()
	db := openDeleteTestDB(t, time.Second)
	cipher, err := crypto.New("api-session-test-master-key-32b!!")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	manager := session.NewManager(db, cipher)
	return &Server{DB: db, Sessions: manager}, manager
}

func seedSession(t *testing.T, manager *session.Manager, key string, collectorID uint) {
	t.Helper()
	state := session.NewState()
	state.CollectorID = collectorID
	state.Vars["token"] = "secret-value"
	if err := manager.Save(context.Background(), key, state); err != nil {
		t.Fatalf("seed session %s: %v", key, err)
	}
}

func listSessionsRequest(server *Server, query string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/sessions?"+query, nil)
	server.listSessions(ctx)
	return recorder
}

func TestListSessionsFiltersByCollector(t *testing.T) {
	server, manager := newSessionTestServer(t)
	seedSession(t, manager, "collector:1", 1)
	seedSession(t, manager, "collector:2", 2)

	recorder := listSessionsRequest(server, "collector_id=2")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var rows []session.Info
	if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != "collector:2" || rows[0].CollectorID != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Status != "active" || rows[0].VarKeys != "token" {
		t.Fatalf("row metadata = %+v", rows[0])
	}
	if jsonContains(recorder.Body.String(), "secret-value") {
		t.Fatalf("session secret value leaked through the API: %s", recorder.Body.String())
	}
}

func jsonContains(body, substr string) bool {
	for i := 0; i+len(substr) <= len(body); i++ {
		if body[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDeleteSessionRemovesRow(t *testing.T) {
	server, manager := newSessionTestServer(t)
	seedSession(t, manager, "collector:3", 3)
	rows, err := manager.List(context.Background(), session.ListFilter{CollectorID: 3})
	if err != nil || len(rows) != 1 {
		t.Fatalf("seed rows = %+v, %v", rows, err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/"+strconv.FormatUint(uint64(rows[0].ID), 10), nil)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(rows[0].ID), 10)}}
	server.deleteSession(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	remaining, err := manager.List(context.Background(), session.ListFilter{CollectorID: 3})
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining = %+v, err = %v", remaining, err)
	}

	// Deleting again reports 404.
	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/999", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "999"}}
	server.deleteSession(ctx)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d", recorder.Code)
	}
}

func TestClearCollectorSessions(t *testing.T) {
	server, manager := newSessionTestServer(t)
	seedSession(t, manager, "collector:4", 4)
	seedSession(t, manager, "collector:5", 5)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/collectors/4/sessions", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "4"}}
	server.clearCollectorSessions(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		OK      bool  `json:"ok"`
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK || resp.Deleted != 1 {
		t.Fatalf("response = %+v", resp)
	}
	remaining, err := manager.List(context.Background(), session.ListFilter{})
	if err != nil || len(remaining) != 1 || remaining[0].Key != "collector:5" {
		t.Fatalf("remaining = %+v, err = %v", remaining, err)
	}
}

func TestDeleteCollectorRemovesItsSessions(t *testing.T) {
	server, manager := newSessionTestServer(t)
	collector := models.Collector{Name: "with session"}
	if err := server.DB.Create(&collector).Error; err != nil {
		t.Fatal(err)
	}
	seedSession(t, manager, "collector:"+strconv.FormatUint(uint64(collector.ID), 10), collector.ID)

	recorder := deleteCollectorRequest(server, collector.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	rows, err := manager.List(context.Background(), session.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("sessions should be removed with the collector, got %+v", rows)
	}
}
