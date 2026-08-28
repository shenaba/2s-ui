package notify

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

const defaultTelegramAPI = "https://api.telegram.org"

// sendTelegram delivers one event to every configured chat.
//
// Each chat is attempted independently: one bad chat id (a chat the bot was
// removed from, a typo) must not stop the alert reaching the others.
func sendTelegram(cfg Config, e Event) error {
	text := Host() + "\n" + Render(e, cfg.Lang)
	pages := Paginate(text, pageLimit)

	var firstErr error
	for _, chatID := range cfg.Telegram.ChatIDs {
		for _, page := range pages {
			if err := telegramSendMessage(cfg, chatID, page); err != nil {
				logger.Warning("notify: telegram send to ", chatID, " failed: ", err)
				if firstErr == nil {
					firstErr = err
				}
				// Skip this chat's remaining pages -- a half-delivered multi
				// page message is worse than one that failed outright.
				break
			}
		}
	}
	return firstErr
}

// sendClientAlerts warns each affected client on their own Telegram chat.
//
// Gated by the caller on the token alone rather than on TelegramConfig.enabled(),
// which also demands an admin chat id: telling customers about their own quota
// while the operator hears nothing is a legitimate setup, and the only thing
// this needs is the bot's credentials. It does not need the interactive bot to
// be switched on either -- this is an outgoing call, not a polling session.
//
// A target RenderClient has no wording for is skipped rather than sent an empty
// message, so adding a kind to the bus does not silently start messaging
// customers about it.
func sendClientAlerts(cfg Config, e Event) error {
	d, ok := e.Data.(*ClientData)
	if !ok || len(d.Targets) == 0 {
		return nil
	}

	var firstErr error
	for _, target := range d.Targets {
		if target.TgId == 0 {
			continue
		}
		text := RenderClient(e, target, cfg.Lang)
		if text == "" {
			continue
		}
		if err := telegramSendMessage(cfg, strconv.FormatInt(target.TgId, 10), text); err != nil {
			logger.Warning("notify: client alert to ", target.TgId, " failed: ", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func telegramSendMessage(cfg Config, chatID, text string) error {
	base := strings.TrimSuffix(cfg.Telegram.APIServer, "/")
	if base == "" {
		base = defaultTelegramAPI
	}
	endpoint := base + "/bot" + cfg.Telegram.Token + "/sendMessage"

	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	// No parse_mode on purpose. The messages carry no markup, and asking
	// Telegram to parse them as HTML would make any client name or error string
	// containing < or & a 400 -- which is to say the alerts most worth sending
	// would be the ones that fail.
	form.Set("disable_web_page_preview", "true")

	resp, err := httpClient(cfg.Proxy).PostForm(endpoint, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Telegram reports its own failures in the body with HTTP 200 in some
	// cases, so the status code alone is not enough.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var result struct {
		Ok          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode != http.StatusOK {
			return common.NewErrorf("telegram: http %d", resp.StatusCode)
		}
		return common.NewErrorf("telegram: unreadable response: %v", err)
	}
	if !result.Ok {
		return common.NewErrorf("telegram: %s", result.Description)
	}
	return nil
}

// telegramSendDocument uploads a file. Used for the scheduled database backup,
// which is the one notification that is not a message.
func telegramSendDocument(cfg Config, chatID, filename string, data []byte, caption string) error {
	base := strings.TrimSuffix(cfg.Telegram.APIServer, "/")
	if base == "" {
		base = defaultTelegramAPI
	}
	endpoint := base + "/bot" + cfg.Telegram.Token + "/sendDocument"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if caption != "" {
		// Telegram caps captions at 1024, well under a message's 4096, and
		// rejects the whole upload when it is exceeded -- which would lose the
		// backup over a caption.
		if err := w.WriteField("caption", firstRunes(caption, 1000)); err != nil {
			return err
		}
	}
	part, err := w.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	// Uploads are slower than messages and a database can be a few MB, so this
	// one gets its own client rather than the shared 10s one.
	client := *httpClient(cfg.Proxy)
	client.Timeout = uploadTimeout
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var result struct {
		Ok          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		if resp.StatusCode != http.StatusOK {
			return common.NewErrorf("telegram: http %d", resp.StatusCode)
		}
		return common.NewErrorf("telegram: unreadable response: %v", err)
	}
	if !result.Ok {
		return common.NewErrorf("telegram: %s", result.Description)
	}
	return nil
}

// firstRunes truncates on a rune boundary, so a cut never produces invalid
// UTF-8 that Telegram would reject outright.
func firstRunes(s string, limit int) string {
	n := 0
	for i := range s {
		if n == limit {
			return s[:i]
		}
		n++
	}
	return s
}
