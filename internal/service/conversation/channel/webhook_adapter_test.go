package channel

import (
	"context"
	"testing"
)

func TestWebhookAdapter_Receive(t *testing.T) {
	ctx := context.Background()
	adapter := NewWebhookAdapter("message")

	t.Run("valid body", func(t *testing.T) {
		req := &WebhookRequest{
			Body: []byte(`{"message":"hello world"}`),
		}
		turn, err := adapter.Receive(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Channel != ChannelWebhook {
			t.Errorf("channel = %q, want webhook", turn.Channel)
		}
		if turn.Message != "hello world" {
			t.Errorf("message = %q, want hello world", turn.Message)
		}
		if turn.UserID != "" {
			t.Errorf("user_id = %q, want empty: only the webhook key names the account", turn.UserID)
		}
	})

	t.Run("custom path body.text", func(t *testing.T) {
		a := NewWebhookAdapter("body.text")
		req := &WebhookRequest{
			Body: []byte(`{"body":{"text":"nested"}}`),
		}
		turn, err := a.Receive(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Message != "nested" {
			t.Errorf("message = %q, want nested", turn.Message)
		}
	})

	t.Run("missing message at path", func(t *testing.T) {
		req := &WebhookRequest{
			Body: []byte(`{"other":"value"}`),
		}
		_, err := adapter.Receive(ctx, req)
		if err == nil {
			t.Fatal("expected error for missing message")
		}
	})

	t.Run("invalid raw type", func(t *testing.T) {
		_, err := adapter.Receive(ctx, "not a WebhookRequest")
		if err == nil {
			t.Fatal("expected error for wrong type")
		}
	})
}

func TestWebhookAdapter_Receive_callbackUrl(t *testing.T) {
	ctx := context.Background()
	adapter := NewWebhookAdapter("message")
	req := &WebhookRequest{
		Body: []byte(`{"message":"run","callback_url":"https://example.com/cb"}`),
	}
	turn, err := adapter.Receive(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Raw == nil {
		t.Fatal("Raw nil")
	}
	if turn.Raw["callback_url"] != "https://example.com/cb" {
		t.Errorf("callback_url = %v", turn.Raw["callback_url"])
	}
}

// Send is no-op when conversationID is not a URL; we don't test HTTP POST here.
func TestWebhookAdapter_Send_emptyNoOp(t *testing.T) {
	ctx := context.Background()
	adapter := NewWebhookAdapter("message")
	if err := adapter.Send(ctx, "", "output"); err != nil {
		t.Error(err)
	}
}
