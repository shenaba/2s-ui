package notify

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/shenaba/2s-ui/util/common"
)

// webhookPayload is the JSON a webhook receiver gets.
//
// Both the machine-readable kind and the rendered text are included: a receiver
// that just forwards to a chat wants Text, while one that routes or filters
// wants Kind and Subject without having to parse prose.
type webhookPayload struct {
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	Text      string `json:"text"`
	Host      string `json:"host"`
	Timestamp int64  `json:"timestamp"`
}

// sendWebhook POSTs one event as JSON. This is the generic escape hatch --
// Feishu, DingTalk, Discord and self-hosted relays all take a JSON POST, and it
// is the only route left when Telegram is unreachable.
func sendWebhook(cfg Config, e Event) error {
	body, err := json.Marshal(webhookPayload{
		Kind:      string(e.Kind),
		Subject:   e.Subject,
		Text:      Render(e, cfg.Lang),
		Host:      Host(),
		Timestamp: e.At.Unix(),
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, cfg.Webhook.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient(cfg.Proxy).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return common.NewErrorf("webhook: http %d", resp.StatusCode)
	}
	return nil
}
