package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/store/models"
)

func TestRunHistoryDateOrderPaginationAndDataPoints(t *testing.T) {
	// Exercise browser UTC dates against timestamps stored in the server's zone.
	originalLocation := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	defer func() { time.Local = originalLocation }()
	db := openDeleteTestDB(t, time.Second)
	server := &Server{DB: db}
	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 1)
	runs := []models.Run{
		{ID: 10, CollectorID: 1, Status: "success", StartedAt: start},
		{ID: 2, CollectorID: 1, Status: "success", StartedAt: end.Add(-time.Nanosecond)},
		{ID: 3, CollectorID: 1, Status: "failed", StartedAt: end.Add(-time.Nanosecond)},
		{ID: 11, CollectorID: 1, Status: "success", StartedAt: start.Add(-time.Nanosecond)},
		{ID: 12, CollectorID: 1, Status: "success", StartedAt: end},
		{ID: 13, CollectorID: 2, Status: "success", StartedAt: start.Add(time.Hour)},
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	points := []models.DataPoint{
		{CollectorID: 1, IndicatorID: 1, RunID: 2, ValueNum: &zero, Ts: end},
		{CollectorID: 1, IndicatorID: 2, RunID: 2, Ts: end},
		{CollectorID: 1, IndicatorID: 1, RunID: 10, Ts: start},
		{CollectorID: 2, IndicatorID: 1, RunID: 13, Ts: start},
	}
	if err := db.Create(&points).Error; err != nil {
		t.Fatal(err)
	}
	params := url.Values{"from": {start.UTC().Format(time.RFC3339Nano)}, "to": {end.UTC().Format(time.RFC3339Nano)}, "limit": {"2"}}
	recorder := historyRequest(server.listRuns, "1", params)
	rows := decodeRunHistory(t, recorder, 3)
	if len(rows) != 2 || rows[0].ID != 3 || rows[1].ID != 2 {
		t.Fatalf("unexpected first page: %+v", rows)
	}
	if rows[0].DataPoints == nil || len(rows[0].DataPoints) != 0 {
		t.Fatalf("run with no points should have an empty array: %+v", rows[0])
	}
	if len(rows[1].DataPoints) != 2 || rows[1].DataPoints[0].ValueNum == nil || *rows[1].DataPoints[0].ValueNum != 0 {
		t.Fatalf("run points missing: %+v", rows[1])
	}
	params.Set("offset", "2")
	rows = decodeRunHistory(t, historyRequest(server.listRuns, "1", params), 3)
	if len(rows) != 1 || rows[0].ID != 10 || len(rows[0].DataPoints) != 1 {
		t.Fatalf("unexpected second page: %+v", rows)
	}
	params.Set("offset", "0")
	params.Set("indicator_id", "2")
	rows = decodeRunHistory(t, historyRequest(server.listRuns, "1", params), 1)
	if len(rows) != 1 || rows[0].ID != 2 {
		t.Fatalf("indicator filter returned: %+v", rows)
	}
	params.Set("indicator_id", "999")
	rows = decodeRunHistory(t, historyRequest(server.listRuns, "1", params), 0)
	if rows == nil || len(rows) != 0 {
		t.Fatalf("empty history = %+v", rows)
	}
}

func TestRunHistoryDoesNotTruncateDataPoints(t *testing.T) {
	db := openDeleteTestDB(t, time.Second)
	server := &Server{DB: db}
	now := time.Now()
	run := models.Run{CollectorID: 1, Status: "success", StartedAt: now}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	points := make([]models.DataPoint, 2100)
	for i := range points {
		points[i] = models.DataPoint{CollectorID: 1, IndicatorID: uint(i + 1), RunID: run.ID, Ts: now}
	}
	if err := db.CreateInBatches(&points, 100).Error; err != nil {
		t.Fatal(err)
	}
	rows := decodeRunHistory(t, historyRequest(server.listRuns, "1", nil), 1)
	if len(rows) != 1 || len(rows[0].DataPoints) != len(points) {
		t.Fatalf("expected all %d points on the run", len(points))
	}
}

func TestHistoryRejectsInvalidFilters(t *testing.T) {
	server := &Server{DB: openDeleteTestDB(t, time.Second)}
	for _, query := range []string{
		"from=not-a-date", "to=not-a-date",
		"from=2026-09-20T00:00:00Z&to=2026-09-19T00:00:00Z",
		"from=2026-09-20T00:00:00Z&to=2026-09-20T00:00:00Z",
		"limit=0", "limit=-1", "limit=bad", "limit=99999", "offset=-1", "offset=bad",
	} {
		t.Run(query, func(t *testing.T) {
			params, err := url.ParseQuery(query)
			if err != nil {
				t.Fatal(err)
			}
			for _, handler := range []gin.HandlerFunc{server.listRuns, server.listDataPoints} {
				response := historyRequest(handler, "1", params)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
				}
			}
		})
	}
	for _, id := range []string{"0", "-1", "bad"} {
		response := historyRequest(server.listRuns, "1", url.Values{"indicator_id": {id}})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("indicator_id %q returned %d", id, response.Code)
		}
	}
}

func TestDataPointsNewestFirst(t *testing.T) {
	db := openDeleteTestDB(t, time.Second)
	server := &Server{DB: db}
	now := time.Now().In(time.Local)
	points := []models.DataPoint{
		{ID: 10, CollectorID: 1, RunID: 1, IndicatorID: 1, Ts: now.Add(-time.Hour)},
		{ID: 2, CollectorID: 1, RunID: 1, IndicatorID: 1, Ts: now},
		{ID: 3, CollectorID: 1, RunID: 1, IndicatorID: 1, Ts: now},
		{ID: 4, CollectorID: 2, RunID: 2, IndicatorID: 2, Ts: now},
	}
	if err := db.Create(&points).Error; err != nil {
		t.Fatal(err)
	}
	response := historyRequest(server.listDataPoints, "1", url.Values{"limit": {"2"}, "run_id": {"1"}, "indicator_id": {"1"}})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var rows []models.DataPoint
	if err := sonic.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != 3 || rows[1].ID != 2 {
		t.Fatalf("unexpected order: %+v", rows)
	}
}

func historyRequest(handler gin.HandlerFunc, collectorID string, params url.Values) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/history?"+params.Encode(), nil)
	ctx.Params = gin.Params{{Key: "id", Value: collectorID}}
	handler(ctx)
	return recorder
}

func decodeRunHistory(t *testing.T, recorder *httptest.ResponseRecorder, total int) []runHistory {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Total-Count"); got != strconv.Itoa(total) {
		t.Fatalf("total = %s, want %d", got, total)
	}
	var rows []runHistory
	if err := sonic.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}
