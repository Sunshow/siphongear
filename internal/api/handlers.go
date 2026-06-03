package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/auth"
	"github.com/sunshow/siphongear/internal/pipeline"
	"github.com/sunshow/siphongear/internal/rules"
	"github.com/sunshow/siphongear/internal/store/models"
)

// ---- auth ----

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (s *Server) handleLogin(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var u models.User
	if err := s.DB.Where("username = ?", req.Username).First(&u).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if !auth.CheckPassword(req.Password, u.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, err := s.JWT.Sign(u.ID, u.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"token": token, "user": u})
}

func (s *Server) handleMe(c *gin.Context) {
	uid := c.GetUint(ctxUserID)
	var u models.User
	if err := s.DB.First(&u, uid).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(200, u)
}

type changePassReq struct {
	Old string `json:"old_password" binding:"required"`
	New string `json:"new_password" binding:"required,min=6"`
}

func (s *Server) handleChangePassword(c *gin.Context) {
	uid := c.GetUint(ctxUserID)
	var req changePassReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var u models.User
	if err := s.DB.First(&u, uid).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if !auth.CheckPassword(req.Old, u.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "wrong password"})
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Model(&u).Update("password_hash", hash).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- registry ----

func (s *Server) handleListStepMeta(c *gin.Context) {
	c.JSON(200, pipeline.ListMeta())
}

// ---- sites ----

func (s *Server) listSites(c *gin.Context) {
	var rows []models.Site
	if err := s.DB.Order("id desc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Server) createSite(c *gin.Context) {
	var row models.Site
	if err := c.ShouldBindJSON(&row); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	row.ID = 0
	if err := s.DB.Create(&row).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Server) getSite(c *gin.Context) {
	var row models.Site
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	c.JSON(200, row)
}

func (s *Server) updateSite(c *gin.Context) {
	var row models.Site
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	var update models.Site
	if err := c.ShouldBindJSON(&update); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	update.ID = row.ID
	update.CreatedAt = row.CreatedAt
	if err := s.DB.Save(&update).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, update)
}

func (s *Server) deleteSite(c *gin.Context) {
	if err := s.DB.Delete(&models.Site{}, c.Param("id")).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- credentials ----

type credentialIn struct {
	SiteID  uint           `json:"site_id"`
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

func (s *Server) listCredentials(c *gin.Context) {
	var rows []models.Credential
	q := s.DB.Order("id desc")
	if sid := c.Query("site_id"); sid != "" {
		q = q.Where("site_id = ?", sid)
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Server) createCredential(c *gin.Context) {
	var in credentialIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	plain, _ := sonic.Marshal(in.Payload)
	enc, err := s.Cipher.EncryptString(string(plain))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	row := models.Credential{
		SiteID:  in.SiteID,
		Name:    in.Name,
		Type:    in.Type,
		Payload: enc,
	}
	if err := s.DB.Create(&row).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Server) getCredential(c *gin.Context) {
	var row models.Credential
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	resp := gin.H{
		"id":         row.ID,
		"site_id":    row.SiteID,
		"name":       row.Name,
		"type":       row.Type,
		"created_at": row.CreatedAt,
		"updated_at": row.UpdatedAt,
	}
	if row.Payload != "" {
		plain, err := s.Cipher.DecryptString(row.Payload)
		if err == nil {
			var payload map[string]any
			if sonic.UnmarshalString(plain, &payload) == nil {
				resp["payload"] = payload
			}
		}
	}
	c.JSON(200, resp)
}

func (s *Server) updateCredential(c *gin.Context) {
	var row models.Credential
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	var in credentialIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	row.SiteID = in.SiteID
	row.Name = in.Name
	row.Type = in.Type
	if in.Payload != nil {
		plain, _ := sonic.Marshal(in.Payload)
		enc, err := s.Cipher.EncryptString(string(plain))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		row.Payload = enc
	}
	if err := s.DB.Save(&row).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Server) deleteCredential(c *gin.Context) {
	if err := s.DB.Delete(&models.Credential{}, c.Param("id")).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- collectors ----

func (s *Server) listCollectors(c *gin.Context) {
	var rows []models.Collector
	q := s.DB.Order("id desc")
	if sid := c.Query("site_id"); sid != "" {
		q = q.Where("site_id = ?", sid)
	}
	if err := q.Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Server) createCollector(c *gin.Context) {
	var row models.Collector
	if err := c.ShouldBindJSON(&row); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	row.ID = 0
	if err := validatePipelineJSON(row.PipelineJSON); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Create(&row).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_ = s.Scheduler.Reload()
	c.JSON(200, row)
}

func (s *Server) getCollector(c *gin.Context) {
	var row models.Collector
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	c.JSON(200, row)
}

func (s *Server) updateCollector(c *gin.Context) {
	var row models.Collector
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	var in models.Collector
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := validatePipelineJSON(in.PipelineJSON); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	in.ID = row.ID
	in.CreatedAt = row.CreatedAt
	if err := s.DB.Save(&in).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_ = s.Scheduler.Reload()
	c.JSON(200, in)
}

func (s *Server) deleteCollector(c *gin.Context) {
	id := c.Param("id")
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id IN (?)",
			tx.Model(&models.Run{}).Select("id").Where("collector_id = ?", id),
		).Delete(&models.StepLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("collector_id = ?", id).Delete(&models.Run{}).Error; err != nil {
			return err
		}
		if err := tx.Where("collector_id = ?", id).Delete(&models.DataPoint{}).Error; err != nil {
			return err
		}
		if err := tx.Where("collector_id = ?", id).Delete(&models.Indicator{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Collector{}, id).Error
	})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_ = s.Scheduler.Reload()
	c.JSON(200, gin.H{"ok": true})
}

func validatePipelineJSON(s string) error {
	if s == "" {
		return nil
	}
	var def pipeline.Definition
	if err := sonic.UnmarshalString(s, &def); err != nil {
		return err
	}
	return def.Validate()
}

type runReq struct {
	Params map[string]any `json:"params"`
}

func (s *Server) runCollector(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req runReq
	_ = c.ShouldBindJSON(&req)
	res, err := s.Runner.Trigger(c.Request.Context(), uint(id), "manual", req.Params, false)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error(), "run": res})
		return
	}
	c.JSON(200, gin.H{"run": res.Run, "indicators": res.Result.Indicators})
}

func (s *Server) dryRunCollector(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var req runReq
	_ = c.ShouldBindJSON(&req)
	res, err := s.Runner.Trigger(c.Request.Context(), uint(id), "dryrun", req.Params, true)
	if err != nil {
		c.JSON(200, gin.H{"error": err.Error(), "run": res, "step_logs": res.StepLogs})
		return
	}
	c.JSON(200, gin.H{
		"run":        res.Run,
		"step_logs":  res.StepLogs,
		"indicators": res.Result.Indicators,
		"vars":       res.Result.Payload.Vars,
	})
}

func (s *Server) listRuns(c *gin.Context) {
	var rows []models.Run
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err := s.DB.Where("collector_id = ?", c.Param("id")).Order("id desc").Limit(limit).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Server) getRun(c *gin.Context) {
	var run models.Run
	if err := s.DB.First(&run, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	var logs []models.StepLog
	if err := s.DB.Where("run_id = ?", run.ID).Order("`index` asc").Find(&logs).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"run": run, "step_logs": logs})
}

func (s *Server) listDataPoints(c *gin.Context) {
	var rows []models.DataPoint
	q := s.DB.Where("collector_id = ?", c.Param("id"))
	if ind := c.Query("indicator_id"); ind != "" {
		q = q.Where("indicator_id = ?", ind)
	}
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			q = q.Where("ts >= ?", t)
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			q = q.Where("ts <= ?", t)
		}
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	if err := q.Order("ts asc").Limit(limit).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

// ---- indicators ----

func (s *Server) listIndicators(c *gin.Context) {
	var rows []models.Indicator
	if err := s.DB.Where("collector_id = ?", c.Param("id")).Order("id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Server) createIndicator(c *gin.Context) {
	cid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var row models.Indicator
	if err := c.ShouldBindJSON(&row); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	row.ID = 0
	row.CollectorID = uint(cid)
	if err := s.DB.Create(&row).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Server) updateIndicator(c *gin.Context) {
	var row models.Indicator
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	var in models.Indicator
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	in.ID = row.ID
	in.CollectorID = row.CollectorID
	in.CreatedAt = row.CreatedAt
	if err := s.DB.Save(&in).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, in)
}

func (s *Server) deleteIndicator(c *gin.Context) {
	if err := s.DB.Delete(&models.Indicator{}, c.Param("id")).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- dashboard ----

type dashboardCard struct {
	CollectorID   uint       `json:"collector_id"`
	CollectorName string     `json:"collector_name"`
	SiteID        uint       `json:"site_id"`
	SiteName      string     `json:"site_name"`
	SiteBaseURL   string     `json:"site_base_url"`
	IndicatorID   uint       `json:"indicator_id"`
	Key           string     `json:"key"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	Unit          string     `json:"unit"`
	Display       string     `json:"display"`
	ValueNum      *float64   `json:"value_num"`
	ValueStr      *string    `json:"value_str"`
	ValueJSON     *string    `json:"value_json"`
	Ts            *time.Time `json:"ts"`
	PrevValueNum  *float64     `json:"prev_value_num"`
	PrevValueStr  *string      `json:"prev_value_str"`
	PrevValueJSON *string      `json:"prev_value_json"`
	PrevTs        *time.Time   `json:"prev_ts"`
	SiteTags      []string     `json:"site_tags"`
	MatchedRule   *matchedRule `json:"matched_rule,omitempty"`
	LastStatus    string       `json:"last_status"`
}

type matchedRule struct {
	ID       uint     `json:"id"`
	Name     string   `json:"name"`
	Severity string   `json:"severity"`
	Actions  []string `json:"actions"`
}

func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) handleDashboard(c *gin.Context) {
	cards, err := s.buildDashboardCards(dashboardFilter{})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, cards)
}

type dashboardFilter struct {
	CollectorID  uint
	IndicatorKey string
	SiteID       uint
	Tag          string
}

func (s *Server) buildDashboardCards(filter dashboardFilter) ([]dashboardCard, error) {
	q := s.DB.
		Joins("JOIN collectors ON collectors.id = indicators.collector_id AND collectors.deleted_at IS NULL AND collectors.enabled = ?", true).
		Where("indicators.hidden = ?", false)
	if filter.CollectorID != 0 {
		q = q.Where("indicators.collector_id = ?", filter.CollectorID)
	}
	if filter.IndicatorKey != "" {
		q = q.Where("indicators.key = ?", filter.IndicatorKey)
	}
	if filter.SiteID != 0 {
		q = q.Where("collectors.site_id = ?", filter.SiteID)
	}
	var indicators []models.Indicator
	if err := q.Order("indicators.collector_id, indicators.id").Find(&indicators).Error; err != nil {
		return nil, err
	}
	if len(indicators) == 0 {
		return []dashboardCard{}, nil
	}
	type collInfo struct {
		Name       string
		SiteID     uint
		LastStatus string
	}
	collectorMap := map[uint]collInfo{}
	{
		var cs []models.Collector
		_ = s.DB.Find(&cs).Error
		for _, x := range cs {
			collectorMap[x.ID] = collInfo{Name: x.Name, SiteID: x.SiteID, LastStatus: x.LastStatus}
		}
	}
	type siteInfo struct {
		Name    string
		BaseURL string
		Tags    []string
	}
	siteMap := map[uint]siteInfo{}
	{
		var ss []models.Site
		_ = s.DB.Find(&ss).Error
		for _, x := range ss {
			siteMap[x.ID] = siteInfo{Name: x.Name, BaseURL: x.BaseURL, Tags: parseTags(x.Tags)}
		}
	}

	type ruleEntry struct {
		row           models.ThresholdRule
		conditions    []rules.Condition
		actions       []rules.Action
		actionTags    []string
		tagFilter     []string
		excludeFilter []string
	}
	var rulesByKey map[string][]ruleEntry
	{
		var rs []models.ThresholdRule
		_ = s.DB.Where("enabled = ?", true).Order("priority asc, id asc").Find(&rs).Error
		rulesByKey = make(map[string][]ruleEntry, len(rs))
		for _, r := range rs {
			conds, err := rules.ParseConditions(r.ConditionJSON)
			if err != nil {
				continue
			}
			acts, err := rules.ParseActions(r.ActionJSON)
			if err != nil {
				continue
			}
			actionTags := make([]string, 0, len(acts))
			for _, a := range acts {
				actionTags = append(actionTags, a.Type)
			}
			rulesByKey[r.IndicatorKey] = append(rulesByKey[r.IndicatorKey], ruleEntry{
				row:           r,
				conditions:    conds,
				actions:       acts,
				actionTags:    actionTags,
				tagFilter:     rules.ParseTargetTags(r.TargetTags),
				excludeFilter: rules.ParseTargetTags(r.ExcludeTags),
			})
		}
	}

	cards := make([]dashboardCard, 0, len(indicators))
	for _, ind := range indicators {
		var dps []models.DataPoint
		_ = s.DB.Where("indicator_id = ?", ind.ID).Order("ts desc").Limit(2).Find(&dps).Error
		ci := collectorMap[ind.CollectorID]
		si := siteMap[ci.SiteID]
		if filter.Tag != "" {
			matched := false
			for _, t := range si.Tags {
				if t == filter.Tag {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		card := dashboardCard{
			CollectorID:   ind.CollectorID,
			CollectorName: ci.Name,
			SiteID:        ci.SiteID,
			SiteName:      si.Name,
			SiteBaseURL:   si.BaseURL,
			IndicatorID:   ind.ID,
			Key:           ind.Key,
			Name:          ind.Name,
			Type:          ind.Type,
			Unit:          ind.Unit,
			Display:       ind.Display,
			SiteTags:      si.Tags,
			LastStatus:    ci.LastStatus,
		}
		if len(dps) >= 1 {
			cur := dps[0]
			card.ValueNum = cur.ValueNum
			card.ValueStr = cur.ValueStr
			card.ValueJSON = cur.ValueJSON
			t := cur.Ts
			card.Ts = &t
		}
		if len(dps) >= 2 {
			prev := dps[1]
			card.PrevValueNum = prev.ValueNum
			card.PrevValueStr = prev.ValueStr
			card.PrevValueJSON = prev.ValueJSON
			pt := prev.Ts
			card.PrevTs = &pt
		}
		for _, re := range rulesByKey[ind.Key] {
			if re.row.TargetType == rules.TargetTags {
				if !rules.TagsIntersect(re.tagFilter, card.SiteTags) {
					continue
				}
			}
			if len(re.excludeFilter) > 0 && rules.TagsIntersect(re.excludeFilter, card.SiteTags) {
				continue
			}
			if !rules.Evaluate(re.conditions, card.ValueNum) {
				continue
			}
			card.MatchedRule = &matchedRule{
				ID:       re.row.ID,
				Name:     re.row.Name,
				Severity: rules.SeverityWarning,
				Actions:  re.actionTags,
			}
			break
		}
		cards = append(cards, card)
	}
	return cards, nil
}
