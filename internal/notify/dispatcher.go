package notify

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/bytedance/sonic"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/sunshow/siphongear/internal/crypto"
	"github.com/sunshow/siphongear/internal/events"
	"github.com/sunshow/siphongear/internal/rules"
	"github.com/sunshow/siphongear/internal/store/models"
)

// Dispatcher reacts to run.completed events, evaluates threshold rules and
// fans out notifications through configured channels with transition-only
// firing semantics.
type Dispatcher struct {
	db     *gorm.DB
	cipher *crypto.Cipher
	bus    *events.Bus
	logger zerolog.Logger
	wg     sync.WaitGroup
}

func NewDispatcher(db *gorm.DB, cipher *crypto.Cipher, bus *events.Bus) *Dispatcher {
	return &Dispatcher{
		db:     db,
		cipher: cipher,
		bus:    bus,
		logger: log.With().Str("component", "notify.dispatcher").Logger(),
	}
}

// Start subscribes to the run.completed topic and processes events until ctx
// is cancelled. Returns immediately; the loop runs in a goroutine.
func (d *Dispatcher) Start(ctx context.Context) {
	ch := d.bus.Subscribe("run.completed", 64)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				d.handle(ctx, ev)
			}
		}
	}()
}

// Wait blocks until the dispatcher loop returns; useful in tests.
func (d *Dispatcher) Wait() { d.wg.Wait() }

func (d *Dispatcher) handle(ctx context.Context, ev events.Event) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error().Interface("recover", r).Msg("dispatcher panic")
		}
	}()

	collectorID, _ := ev.Payload["collector_id"].(uint)
	if collectorID == 0 {
		if cid, ok := ev.Payload["collector_id"].(int); ok {
			collectorID = uint(cid)
		}
	}
	runID, _ := ev.Payload["run_id"].(uint64)
	indicators, _ := ev.Payload["indicators"].(map[string]any)
	if collectorID == 0 || len(indicators) == 0 {
		return
	}
	status, _ := ev.Payload["status"].(string)
	if status != "" && status != "success" {
		return
	}

	// Load site tags for the collector.
	var collector models.Collector
	if err := d.db.WithContext(ctx).First(&collector, collectorID).Error; err != nil {
		d.logger.Warn().Err(err).Uint("collector_id", collectorID).Msg("load collector failed")
		return
	}
	var site models.Site
	siteTags := []string(nil)
	if collector.SiteID != 0 {
		if err := d.db.WithContext(ctx).First(&site, collector.SiteID).Error; err == nil {
			siteTags = parseTags(site.Tags)
		}
	}

	// Load this collector's indicator definitions keyed by Key.
	var defs []models.Indicator
	if err := d.db.WithContext(ctx).Where("collector_id = ?", collectorID).Find(&defs).Error; err != nil {
		d.logger.Warn().Err(err).Uint("collector_id", collectorID).Msg("load indicators failed")
		return
	}
	byKey := map[string]models.Indicator{}
	for _, def := range defs {
		byKey[def.Key] = def
	}

	// Load enabled rules for any of the keys in this run.
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return
	}
	var ruleRows []models.ThresholdRule
	if err := d.db.WithContext(ctx).
		Where("enabled = ? AND indicator_key IN ?", true, keys).
		Order("priority asc, id asc").
		Find(&ruleRows).Error; err != nil {
		d.logger.Warn().Err(err).Msg("load rules failed")
		return
	}
	if len(ruleRows) == 0 {
		return
	}

	for _, rule := range ruleRows {
		ind, ok := byKey[rule.IndicatorKey]
		if !ok {
			continue
		}
		if rule.TargetType == rules.TargetTags {
			tagFilter := rules.ParseTargetTags(rule.TargetTags)
			if !rules.TagsIntersect(tagFilter, siteTags) {
				continue
			}
		}
		raw, present := indicators[rule.IndicatorKey]
		if !present {
			continue
		}
		num, hasNum := toFloat(raw)
		conds, err := rules.ParseConditions(rule.ConditionJSON)
		if err != nil {
			continue
		}
		var matched bool
		if hasNum {
			matched = rules.Evaluate(conds, &num)
		}

		// Load (or create) state and decide whether to fire.
		var st models.RuleNotificationState
		err = d.db.WithContext(ctx).
			Where("rule_id = ? AND indicator_id = ?", rule.ID, ind.ID).
			First(&st).Error
		known := err == nil
		now := time.Now()

		shouldFire := false
		severity := ""
		switch {
		case matched && (!known || !st.Matched):
			shouldFire = true
			severity = SeverityAlert
		case !matched && known && st.Matched:
			shouldFire = true
			severity = SeverityRecovery
		}

		// Persist state regardless of firing so transitions track even when
		// no channels are configured.
		st.RuleID = rule.ID
		st.IndicatorID = ind.ID
		st.Matched = matched
		st.LastEventAt = now
		if known {
			_ = d.db.WithContext(ctx).Save(&st).Error
		} else {
			_ = d.db.WithContext(ctx).Create(&st).Error
		}

		if !shouldFire {
			continue
		}

		channelIDs := parseChannelIDs(rule.NotifyChannelIDs)
		if len(channelIDs) == 0 {
			continue
		}

		msg := buildMessage(d, rule, ind, collector, site, raw, num, hasNum, severity, runID, now)
		d.fanout(ctx, rule.ID, ind.ID, collector.ID, channelIDs, msg)
	}
}

func (d *Dispatcher) fanout(ctx context.Context, ruleID, indicatorID, collectorID uint, channelIDs []uint, msg Message) {
	var channels []models.NotificationChannel
	if err := d.db.WithContext(ctx).Where("id IN ? AND enabled = ?", channelIDs, true).Find(&channels).Error; err != nil {
		d.logger.Warn().Err(err).Msg("load channels failed")
		return
	}
	for _, ch := range channels {
		ch := ch
		go func() {
			d.deliver(ctx, ch, ruleID, indicatorID, collectorID, msg)
		}()
	}
}

func (d *Dispatcher) deliver(ctx context.Context, ch models.NotificationChannel, ruleID, indicatorID, collectorID uint, msg Message) {
	logEntry := &models.NotificationLog{
		ChannelID:   ch.ID,
		RuleID:      ruleID,
		CollectorID: collectorID,
		IndicatorID: indicatorID,
		Severity:    msg.Severity,
		Title:       truncate(msg.Title, 250),
		Snippet:     truncate(msg.Body, 2000),
		Status:      "success",
		CreatedAt:   time.Now(),
	}
	if err := d.send(ctx, ch, msg); err != nil {
		logEntry.Status = "failed"
		logEntry.Error = err.Error()
		d.logger.Warn().Err(err).Uint("channel_id", ch.ID).Msg("notification failed")
	}
	if err := d.db.Create(logEntry).Error; err != nil {
		d.logger.Warn().Err(err).Msg("save notification log failed")
	}
}

func (d *Dispatcher) send(ctx context.Context, ch models.NotificationChannel, msg Message) error {
	if ch.Payload == "" {
		return fmt.Errorf("channel payload is empty")
	}
	plain, err := d.cipher.DecryptString(ch.Payload)
	if err != nil {
		return fmt.Errorf("decrypt channel: %w", err)
	}
	var payload map[string]any
	if err := sonic.UnmarshalString(plain, &payload); err != nil {
		return fmt.Errorf("parse channel: %w", err)
	}
	notifier, err := Build(ch.Type, payload)
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return notifier.Send(sendCtx, msg)
}

// SendOnce builds a notifier for the given channel and sends a one-off message
// (used for "test send" from the API).
func (d *Dispatcher) SendOnce(ctx context.Context, channelID uint, msg Message) error {
	var ch models.NotificationChannel
	if err := d.db.WithContext(ctx).First(&ch, channelID).Error; err != nil {
		return err
	}
	logEntry := &models.NotificationLog{
		ChannelID: ch.ID,
		Severity:  msg.Severity,
		Title:     truncate(msg.Title, 250),
		Snippet:   truncate(msg.Body, 2000),
		Status:    "success",
		CreatedAt: time.Now(),
	}
	err := d.send(ctx, ch, msg)
	if err != nil {
		logEntry.Status = "failed"
		logEntry.Error = err.Error()
	}
	_ = d.db.Create(logEntry).Error
	return err
}

// ---- helpers ----

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

// ParseChannelIDs is exported for use from API validation.
func ParseChannelIDs(s string) []uint { return parseChannelIDs(s) }

func parseChannelIDs(s string) []uint {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]uint, 0, len(parts))
	seen := make(map[uint]struct{}, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		var id uint
		_, err := fmt.Sscanf(t, "%d", &id)
		if err != nil || id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// FormatChannelIDs serialises a slice of IDs to the CSV form persisted on
// ThresholdRule.
func FormatChannelIDs(ids []uint) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	return strings.Join(parts, ",")
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(x), "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func formatValue(raw any, num float64, hasNum bool) string {
	if hasNum {
		s := fmt.Sprintf("%.4f", num)
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
		if s == "" {
			s = "0"
		}
		return s
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		b, _ := sonic.Marshal(v)
		return string(b)
	}
}

// MessageContext is the data exposed to user-supplied notification templates.
type MessageContext struct {
	Severity  string
	Rule      MsgRule
	Collector MsgCollector
	Site      MsgSite
	Indicator MsgIndicator
	Value     string
	ValueRaw  any
	ValueNum  *float64
	RunID     uint64
	Time      string
}

type MsgRule struct {
	ID           uint
	Name         string
	IndicatorKey string
}

type MsgCollector struct {
	ID   uint
	Name string
}

type MsgSite struct {
	ID      uint
	Name    string
	BaseURL string
	Tags    []string
}

type MsgIndicator struct {
	ID   uint
	Key  string
	Name string
	Type string
	Unit string
}

func newContext(rule models.ThresholdRule, ind models.Indicator, c models.Collector, site models.Site, raw any, num float64, hasNum bool, severity string, runID uint64, ts time.Time) MessageContext {
	indName := ind.Name
	if indName == "" {
		indName = ind.Key
	}
	val := formatValue(raw, num, hasNum)
	if ind.Unit != "" {
		val = val + " " + ind.Unit
	}
	mc := MessageContext{
		Severity: severity,
		Rule: MsgRule{
			ID:           rule.ID,
			Name:         rule.Name,
			IndicatorKey: rule.IndicatorKey,
		},
		Collector: MsgCollector{ID: c.ID, Name: c.Name},
		Site: MsgSite{
			ID:      site.ID,
			Name:    site.Name,
			BaseURL: site.BaseURL,
			Tags:    parseTags(site.Tags),
		},
		Indicator: MsgIndicator{
			ID:   ind.ID,
			Key:  ind.Key,
			Name: indName,
			Type: ind.Type,
			Unit: ind.Unit,
		},
		Value:    val,
		ValueRaw: raw,
		RunID:    runID,
		Time:     ts.Format(time.RFC3339),
	}
	if hasNum {
		n := num
		mc.ValueNum = &n
	}
	return mc
}

func defaultTitle(mc MessageContext) string {
	prefix := "[ALERT]"
	if mc.Severity == SeverityRecovery {
		prefix = "[RECOVERY]"
	}
	return fmt.Sprintf("%s %s · %s", prefix, mc.Collector.Name, mc.Indicator.Name)
}

func defaultBody(mc MessageContext) string {
	var b strings.Builder
	b.WriteString("**Rule**: " + mc.Rule.Name + "\n\n")
	b.WriteString("**Collector**: " + mc.Collector.Name + "\n\n")
	if mc.Site.Name != "" {
		b.WriteString("**Site**: " + mc.Site.Name + "\n\n")
	}
	b.WriteString("**Indicator**: " + mc.Indicator.Name + " (`" + mc.Indicator.Key + "`)\n\n")
	b.WriteString("**Value**: " + mc.Value + "\n\n")
	b.WriteString("**Severity**: " + mc.Severity + "\n\n")
	if mc.RunID != 0 {
		b.WriteString(fmt.Sprintf("**Run**: #%d\n\n", mc.RunID))
	}
	b.WriteString("**At**: " + mc.Time)
	return b.String()
}

var tplFuncs = template.FuncMap{
	"upper":  strings.ToUpper,
	"lower":  strings.ToLower,
	"printf": fmt.Sprintf,
	"default": func(d, v any) any {
		if v == nil {
			return d
		}
		if s, ok := v.(string); ok && s == "" {
			return d
		}
		return v
	},
	"now": func() string { return time.Now().Format(time.RFC3339) },
}

// RenderTemplate evaluates a Go text/template against a MessageContext. Empty
// template returns ("", nil). Returned string is trimmed of trailing newlines.
func RenderTemplate(tpl string, mc MessageContext) (string, error) {
	if strings.TrimSpace(tpl) == "" {
		return "", nil
	}
	t, err := template.New("notify").Funcs(tplFuncs).Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, mc); err != nil {
		return "", fmt.Errorf("exec: %w", err)
	}
	return buf.String(), nil
}

// renderOrDefault renders tpl against mc; on error or empty result it returns
// fallback and logs a warning if there was an error.
func (d *Dispatcher) renderOrDefault(tpl, fallback string, mc MessageContext, what string) string {
	if strings.TrimSpace(tpl) == "" {
		return fallback
	}
	out, err := RenderTemplate(tpl, mc)
	if err != nil {
		d.logger.Warn().Err(err).Str("field", what).Uint("rule_id", mc.Rule.ID).Msg("notify template render failed; falling back to default")
		return fallback
	}
	if strings.TrimSpace(out) == "" {
		return fallback
	}
	return out
}

func buildMessage(d *Dispatcher, rule models.ThresholdRule, ind models.Indicator, c models.Collector, site models.Site, raw any, num float64, hasNum bool, severity string, runID uint64, ts time.Time) Message {
	mc := newContext(rule, ind, c, site, raw, num, hasNum, severity, runID, ts)
	title := defaultTitle(mc)
	body := defaultBody(mc)
	if d != nil {
		title = d.renderOrDefault(rule.NotifyTitleTpl, title, mc, "title")
		body = d.renderOrDefault(rule.NotifyBodyTpl, body, mc, "body")
	}
	return Message{Title: title, Body: body, Severity: severity}
}

// PreviewMessage renders the supplied templates against a deterministic mock
// context for the rules preview API. severity defaults to "alert".
func PreviewMessage(titleTpl, bodyTpl, severity string) (Message, string, string) {
	if severity == "" {
		severity = SeverityAlert
	}
	num := 12.34
	mc := MessageContext{
		Severity: severity,
		Rule: MsgRule{
			ID:           1,
			Name:         "Sample rule",
			IndicatorKey: "balance",
		},
		Collector: MsgCollector{ID: 10, Name: "Sample Collector"},
		Site: MsgSite{
			ID:      5,
			Name:    "example.com",
			BaseURL: "https://example.com",
			Tags:    []string{"prod", "billing"},
		},
		Indicator: MsgIndicator{
			ID:   42,
			Key:  "balance",
			Name: "Balance",
			Type: "number",
			Unit: "CNY",
		},
		Value:    "12.34 CNY",
		ValueRaw: num,
		ValueNum: &num,
		RunID:    9999,
		Time:     time.Now().Format(time.RFC3339),
	}
	title := defaultTitle(mc)
	body := defaultBody(mc)
	titleErr := ""
	bodyErr := ""
	if strings.TrimSpace(titleTpl) != "" {
		if v, err := RenderTemplate(titleTpl, mc); err != nil {
			titleErr = err.Error()
		} else {
			title = v
		}
	}
	if strings.TrimSpace(bodyTpl) != "" {
		if v, err := RenderTemplate(bodyTpl, mc); err != nil {
			bodyErr = err.Error()
		} else {
			body = v
		}
	}
	return Message{Title: title, Body: body, Severity: severity}, titleErr, bodyErr
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
