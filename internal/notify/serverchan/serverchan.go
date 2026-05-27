package serverchan

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/sunshow/siphongear/internal/notify"
)

const typeName = "serverchan"

type notifier struct {
	sendKey string
	channel string
	noip    int
}

func (n *notifier) Type() string { return typeName }

func (n *notifier) Send(ctx context.Context, msg notify.Message) error {
	if n.sendKey == "" {
		return fmt.Errorf("send_key is empty")
	}
	title := strings.TrimSpace(msg.Title)
	if title == "" {
		title = "SiphonGear"
	}
	if len(title) > 32 {
		title = title[:32]
	}

	endpoint := fmt.Sprintf("https://sctapi.ftqq.com/%s.send", url.PathEscape(n.sendKey))

	form := map[string]string{
		"title": title,
	}
	if msg.Body != "" {
		form["desp"] = msg.Body
	}
	if n.channel != "" {
		form["channel"] = n.channel
	}
	if n.noip == 1 {
		form["noip"] = "1"
	}

	client := resty.New().SetTimeout(15 * time.Second)
	resp, err := client.R().
		SetContext(ctx).
		SetFormData(form).
		Post(endpoint)
	if err != nil {
		return fmt.Errorf("serverchan post: %w", err)
	}
	if resp.StatusCode() >= 400 {
		body := string(resp.Body())
		if len(body) > 200 {
			body = body[:200] + "..."
		}
		return fmt.Errorf("serverchan http %d: %s", resp.StatusCode(), body)
	}
	return nil
}

func newNotifier(payload map[string]any) (notify.Notifier, error) {
	sendKey, _ := payload["send_key"].(string)
	sendKey = strings.TrimSpace(sendKey)
	if sendKey == "" {
		return nil, fmt.Errorf("send_key is required")
	}
	ch, _ := payload["channel"].(string)
	noip := 0
	switch v := payload["noip"].(type) {
	case bool:
		if v {
			noip = 1
		}
	case float64:
		if v == 1 {
			noip = 1
		}
	case int:
		if v == 1 {
			noip = 1
		}
	case string:
		if v == "1" || strings.EqualFold(v, "true") {
			noip = 1
		}
	}
	return &notifier{
		sendKey: sendKey,
		channel: strings.TrimSpace(ch),
		noip:    noip,
	}, nil
}

func init() {
	notify.Register(notify.Meta{
		Type:        typeName,
		Description: "Server酱 (sctapi.ftqq.com) — push to WeChat and other channels via SendKey",
		Schema: map[string]any{
			"send_key": map[string]any{
				"type":     "string",
				"label":    "SendKey",
				"required": true,
				"secret":   true,
			},
			"channel": map[string]any{
				"type":        "string",
				"label":       "Channel",
				"placeholder": "e.g., 9|66",
				"description": "Optional. Override message channel(s) for this push. Empty = use the default channels configured on the ServerChan dashboard. Up to 2 channels joined by '|'.",
			},
			"noip": map[string]any{
				"type":  "boolean",
				"label": "Hide caller IP",
			},
		},
	}, newNotifier)
}
