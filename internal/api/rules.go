package api

import (
	"net/http"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"

	"github.com/sunshow/siphongear/internal/notify"
	"github.com/sunshow/siphongear/internal/rules"
	"github.com/sunshow/siphongear/internal/store/models"
)

type ruleIn struct {
	Name             string            `json:"name"`
	Enabled          bool              `json:"enabled"`
	Priority         int               `json:"priority"`
	IndicatorKey     string            `json:"indicator_key"`
	TargetType       string            `json:"target_type"`
	TargetTags       []string          `json:"target_tags"`
	Conditions       []rules.Condition `json:"conditions"`
	Actions          []rules.Action    `json:"actions"`
	NotifyChannelIDs []uint            `json:"notify_channel_ids"`
	NotifyTitleTpl   string            `json:"notify_title_tpl"`
	NotifyBodyTpl    string            `json:"notify_body_tpl"`
}

type ruleOut struct {
	models.ThresholdRule
	TargetTagsArr       []string          `json:"target_tags_arr"`
	Conditions          []rules.Condition `json:"conditions"`
	Actions             []rules.Action    `json:"actions"`
	NotifyChannelIDsArr []uint            `json:"notify_channel_ids_arr"`
}

func toRuleOut(r models.ThresholdRule) ruleOut {
	conds, _ := rules.ParseConditions(r.ConditionJSON)
	acts, _ := rules.ParseActions(r.ActionJSON)
	return ruleOut{
		ThresholdRule:       r,
		TargetTagsArr:       rules.ParseTargetTags(r.TargetTags),
		Conditions:          conds,
		Actions:             acts,
		NotifyChannelIDsArr: notify.ParseChannelIDs(r.NotifyChannelIDs),
	}
}

func validateRuleIn(in *ruleIn) error {
	in.Name = strings.TrimSpace(in.Name)
	in.IndicatorKey = strings.TrimSpace(in.IndicatorKey)
	in.TargetType = strings.TrimSpace(in.TargetType)
	if in.Name == "" {
		return errBadRequest("name is required")
	}
	if in.IndicatorKey == "" {
		return errBadRequest("indicator_key is required")
	}
	switch in.TargetType {
	case rules.TargetAll, rules.TargetTags:
	default:
		return errBadRequest("target_type must be 'all' or 'tags'")
	}
	if in.TargetType == rules.TargetTags && len(in.TargetTags) == 0 {
		return errBadRequest("target_tags is required when target_type is 'tags'")
	}
	if err := rules.ValidateConditions(in.Conditions); err != nil {
		return errBadRequest(err.Error())
	}
	if err := rules.ValidateActions(in.Actions); err != nil {
		return errBadRequest(err.Error())
	}
	return nil
}

type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }
func errBadRequest(m string) error { return &httpError{code: 400, msg: m} }

func writeRuleErr(c *gin.Context, err error) {
	if e, ok := err.(*httpError); ok {
		c.JSON(e.code, gin.H{"error": e.msg})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

func applyRuleIn(in *ruleIn, row *models.ThresholdRule) error {
	tagsCSV := ""
	if in.TargetType == rules.TargetTags {
		clean := rules.ParseTargetTags(strings.Join(in.TargetTags, ","))
		tagsCSV = strings.Join(clean, ",")
	}
	condJSON, err := sonic.MarshalString(in.Conditions)
	if err != nil {
		return err
	}
	actJSON, err := sonic.MarshalString(in.Actions)
	if err != nil {
		return err
	}
	row.Name = in.Name
	row.Enabled = in.Enabled
	row.Priority = in.Priority
	row.IndicatorKey = in.IndicatorKey
	row.TargetType = in.TargetType
	row.TargetTags = tagsCSV
	row.ConditionJSON = condJSON
	row.ActionJSON = actJSON
	row.NotifyChannelIDs = notify.FormatChannelIDs(in.NotifyChannelIDs)
	row.NotifyTitleTpl = strings.TrimSpace(in.NotifyTitleTpl)
	row.NotifyBodyTpl = strings.TrimRight(in.NotifyBodyTpl, "\n\r\t ")
	return nil
}

func (s *Server) validateNotifyChannelIDs(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var count int64
	if err := s.DB.Model(&models.NotificationChannel{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return err
	}
	if int(count) != len(ids) {
		return errBadRequest("one or more notify_channel_ids do not exist")
	}
	return nil
}

func (s *Server) listRules(c *gin.Context) {
	var rows []models.ThresholdRule
	if err := s.DB.Order("priority asc, id asc").Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	out := make([]ruleOut, 0, len(rows))
	for _, r := range rows {
		out = append(out, toRuleOut(r))
	}
	c.JSON(200, out)
}

func (s *Server) getRule(c *gin.Context) {
	var row models.ThresholdRule
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	c.JSON(200, toRuleOut(row))
}

func (s *Server) createRule(c *gin.Context) {
	var in ruleIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := validateRuleIn(&in); err != nil {
		writeRuleErr(c, err)
		return
	}
	if err := s.validateNotifyChannelIDs(in.NotifyChannelIDs); err != nil {
		writeRuleErr(c, err)
		return
	}
	var row models.ThresholdRule
	if err := applyRuleIn(&in, &row); err != nil {
		writeRuleErr(c, err)
		return
	}
	if err := s.DB.Create(&row).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, toRuleOut(row))
}

func (s *Server) updateRule(c *gin.Context) {
	var row models.ThresholdRule
	if err := s.DB.First(&row, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "not found"})
		return
	}
	var in ruleIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := validateRuleIn(&in); err != nil {
		writeRuleErr(c, err)
		return
	}
	if err := s.validateNotifyChannelIDs(in.NotifyChannelIDs); err != nil {
		writeRuleErr(c, err)
		return
	}
	if err := applyRuleIn(&in, &row); err != nil {
		writeRuleErr(c, err)
		return
	}
	if err := s.DB.Save(&row).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, toRuleOut(row))
}

func (s *Server) deleteRule(c *gin.Context) {
	id := c.Param("id")
	if err := s.DB.Delete(&models.ThresholdRule{}, id).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Where("rule_id = ?", id).Delete(&models.RuleNotificationState{}).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

type rulePreviewIn struct {
	TitleTpl string `json:"title_tpl"`
	BodyTpl  string `json:"body_tpl"`
	Severity string `json:"severity"`
}

func (s *Server) previewRuleNotify(c *gin.Context) {
	var in rulePreviewIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	msg, titleErr, bodyErr := notify.PreviewMessage(in.TitleTpl, in.BodyTpl, in.Severity)
	c.JSON(200, gin.H{
		"title":     msg.Title,
		"body":      msg.Body,
		"severity":  msg.Severity,
		"title_err": titleErr,
		"body_err":  bodyErr,
	})
}
