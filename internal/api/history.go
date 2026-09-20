package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/store/models"
)

type runHistory struct {
	models.Run
	DataPoints []models.DataPoint `json:"data_points"`
}

func (s *Server) listRuns(c *gin.Context) {
	limit, offset, err := historyPagination(c, 50, 200)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q, err := historyTimeRange(c, s.DB.WithContext(c.Request.Context()).Model(&models.Run{}).
		Where("collector_id = ?", c.Param("id")), "started_at")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if ind := c.Query("indicator_id"); ind != "" {
		id, err := strconv.ParseUint(ind, 10, 64)
		if err != nil || id == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid indicator_id"})
			return
		}
		q = q.Where("id IN (?)", s.DB.Model(&models.DataPoint{}).Select("run_id").
			Where("collector_id = ? AND indicator_id = ?", c.Param("id"), id))
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var runs []models.Run
	if err := q.Order("started_at desc, id desc").Limit(limit).Offset(offset).Find(&runs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rows := make([]runHistory, len(runs))
	ids := make([]uint64, len(runs))
	byID := make(map[uint64]int, len(runs))
	for i, run := range runs {
		rows[i] = runHistory{Run: run, DataPoints: []models.DataPoint{}}
		ids[i] = run.ID
		byID[run.ID] = i
	}
	if len(ids) > 0 {
		var points []models.DataPoint
		// Load by the actual page of runs, so old pages and runs with many
		// indicators never lose values to an unrelated data-point limit.
		if err := s.DB.WithContext(c.Request.Context()).
			Where("collector_id = ? AND run_id IN ?", c.Param("id"), ids).
			Order("indicator_id asc, id asc").Find(&points).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, point := range points {
			i := byID[point.RunID]
			rows[i].DataPoints = append(rows[i].DataPoints, point)
		}
	}
	// Preserve the array response for existing callers.
	c.Header("X-Total-Count", strconv.FormatInt(total, 10))
	c.JSON(http.StatusOK, rows)
}

func (s *Server) listDataPoints(c *gin.Context) {
	limit, offset, err := historyPagination(c, 500, 5000)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q, err := historyTimeRange(c, s.DB.WithContext(c.Request.Context()).
		Where("collector_id = ?", c.Param("id")), "ts")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if ind := c.Query("indicator_id"); ind != "" {
		q = q.Where("indicator_id = ?", ind)
	}
	if rid := c.Query("run_id"); rid != "" {
		q = q.Where("run_id = ?", rid)
	}
	rows := []models.DataPoint{}
	if err := q.Order("ts desc, id desc").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func historyPagination(c *gin.Context, defaultLimit, maxLimit int) (int, int, error) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultLimit)))
	if err != nil || limit <= 0 || limit > maxLimit {
		return 0, 0, fmt.Errorf("limit must be between 1 and %d", maxLimit)
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		return 0, 0, fmt.Errorf("offset must be a non-negative integer")
	}
	return limit, offset, nil
}

// Date ranges are [from, to): the UI sends local midnight of the day after the
// selected end date, keeping that entire date (including sub-second timestamps).
func historyTimeRange(c *gin.Context, q *gorm.DB, column string) (*gorm.DB, error) {
	var from, to time.Time
	for _, param := range []string{"from", "to"} {
		value := c.Query(param)
		if value == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, fmt.Errorf("%s must be an RFC3339 timestamp", param)
		}
		// Match the location used by time.Now() when writing timestamps,
		// including SQLite's textual timestamp representation.
		t = t.In(time.Local)
		if param == "from" {
			from = t
			q = q.Where(column+" >= ?", t)
		} else {
			to = t
			q = q.Where(column+" < ?", t)
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return nil, fmt.Errorf("from must be earlier than to")
	}
	return q, nil
}
