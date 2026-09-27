package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type WebhookRequest struct {
	Body   []byte
	Header map[string][]string
}

type WebhookCallbackSender interface {
	SendWebhookCallback(ctx context.Context, callbackURL string, output string) error
}

// WebhookAdapter parses an inbound webhook body into a Turn. It names no user:
// the webhook key already identifies the account, and the handler that
// resolved it is the only place that may say who the turn belongs to.
type WebhookAdapter struct {
	MessagePath    string
	CallbackSender WebhookCallbackSender
}

func NewWebhookAdapter(messagePath string) *WebhookAdapter {
	if messagePath == "" {
		messagePath = "message"
	}
	return &WebhookAdapter{MessagePath: messagePath}
}

func (a *WebhookAdapter) Receive(ctx context.Context, raw any) (Turn, error) {
	req, ok := raw.(*WebhookRequest)
	if !ok {
		return Turn{}, fmt.Errorf("webhook: raw must be *WebhookRequest, got %T", raw)
	}
	message, err := getJSONPathString(req.Body, a.MessagePath)
	if err != nil {
		return Turn{}, err
	}
	if message == "" {
		return Turn{}, fmt.Errorf("webhook: message empty at path %q", a.MessagePath)
	}
	rawMap := make(map[string]any)
	if len(req.Body) > 0 {
		_ = json.Unmarshal(req.Body, &rawMap)
	}
	callbackURL, _ := getJSONPathString(req.Body, "callback_url")
	if callbackURL != "" {
		if rawMap == nil {
			rawMap = make(map[string]any)
		}
		rawMap["callback_url"] = callbackURL
	}
	return Turn{
		Channel:        ChannelWebhook,
		ConversationID: "",
		Message:        message,
		Raw:            rawMap,
	}, nil
}

func (a *WebhookAdapter) Send(ctx context.Context, target string, output string) error {
	if target == "" {
		return nil
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return nil
	}
	if a.CallbackSender == nil {
		return nil
	}
	return a.CallbackSender.SendWebhookCallback(ctx, target, output)
}

func getJSONPathString(body []byte, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path required")
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}
	parts := strings.SplitN(path, ".", 2)
	var v any = m
	for _, key := range parts {
		mm, ok := v.(map[string]any)
		if !ok {
			return "", nil
		}
		v = mm[key]
		if v == nil {
			return "", nil
		}
	}
	switch s := v.(type) {
	case string:
		return s, nil
	case nil:
		return "", nil
	default:
		return fmt.Sprint(v), nil
	}
}
