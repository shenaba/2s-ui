package notify

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

// sendTimeout caps every outbound call. Long enough for a slow Telegram edge,
// short enough that a subscriber worker cannot sit on a dead connection for
// minutes while its queue fills behind it.
const sendTimeout = 10 * time.Second

// uploadTimeout covers the scheduled backup, which is a multi-megabyte upload
// rather than a one-line message.
const uploadTimeout = 2 * time.Minute

// Config is the whole notification setup, read fresh for every event.
//
// It is supplied as a callback rather than captured at Start, so a settings
// change takes effect on the next event with no reload step to remember. The
// events are rare enough (minutes apart at worst) that the extra settings read
// does not matter.
type Config struct {
	Enable bool
	Proxy  string
	Lang   string
	Events map[Kind]bool

	Telegram TelegramConfig
	Webhook  WebhookConfig
	SMTP     SMTPConfig
}

// wants reports whether the operator asked to hear about this kind.
func (c Config) wants(k Kind) bool {
	return c.Enable && c.Events[k]
}

type TelegramConfig struct {
	Token   string
	ChatIDs []string
	// APIServer overrides https://api.telegram.org, for operators running their
	// own Bot API server. On a censored network that is a steadier route than
	// an HTTP proxy, which still has to reach Telegram's own edge.
	APIServer string
}

func (c TelegramConfig) enabled() bool { return c.Token != "" && len(c.ChatIDs) > 0 }

type WebhookConfig struct {
	URL string
}

func (c WebhookConfig) enabled() bool { return c.URL != "" }

type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
	To   []string
	// Security is none, starttls or tls.
	Security string
}

func (c SMTPConfig) enabled() bool {
	return c.Host != "" && c.Port > 0 && c.From != "" && len(c.To) > 0
}

// Notifier ties the suppressor and the bus to a config source.
type Notifier struct {
	bus *Bus
	sup *Suppressor
	cfg func() Config
}

var (
	stdMu sync.RWMutex
	std   *Notifier
)

// Start brings the notifier up. It follows service.StartHub's shape: a
// package-level singleton with no-op entry points once it is down, so callers
// never have to check whether notifications are running before publishing.
func Start(cfg func() Config) {
	if cfg == nil {
		return
	}
	stdMu.Lock()
	defer stdMu.Unlock()
	if std != nil {
		return
	}
	n := &Notifier{bus: NewBus(), sup: NewSuppressor(), cfg: cfg}
	n.bus.Subscribe("telegram", func(e Event) {
		if c := cfg(); c.Telegram.enabled() {
			sendTelegram(c, e)
		}
	})
	n.bus.Subscribe("webhook", func(e Event) {
		if c := cfg(); c.Webhook.enabled() {
			sendWebhook(c, e)
		}
	})
	n.bus.Subscribe("smtp", func(e Event) {
		if c := cfg(); c.SMTP.enabled() {
			sendSMTP(c, e)
		}
	})
	std = n
}

// Stop tears the notifier down. Safe to call when it was never started.
func Stop() {
	stdMu.Lock()
	n := std
	std = nil
	stdMu.Unlock()
	if n != nil {
		n.bus.Stop()
	}
}

// Publish is what every event source calls. It never blocks and never returns
// an error: a notification that cannot be delivered must not change what the
// caller does, and every caller here is a cron job or the login path.
func Publish(e Event) {
	stdMu.RLock()
	n := std
	stdMu.RUnlock()
	if n == nil {
		return
	}
	n.publish(e)
}

func (n *Notifier) publish(e Event) {
	cfg := n.cfg()
	if !cfg.wants(e.Kind) {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	d := n.sup.Decide(e, e.At)
	if !d.Send {
		return
	}
	// Fold the swallowed-attempt count into the payload the renderer sees.
	// Copied rather than written through: the caller owns that struct and may
	// well be reusing it across a loop.
	if d.Failures > 1 {
		if ld, ok := e.Data.(*LoginData); ok {
			cp := *ld
			cp.Failures = d.Failures
			e.Data = &cp
		}
	}
	n.bus.Publish(e)
}

// TestDeliver sends one event straight to every configured channel, skipping
// both the enabled-events filter and the suppressor.
//
// It backs the settings page's "send a test message" button, which has to
// reach the operator even when nothing is switched on yet -- that is the whole
// point of pressing it. Without this button a wrong chat id fails silently,
// which is the most common support question these panels get.
//
// It uses the saved settings, not whatever is in the form: the credentials are
// write-only, so the page has no token to submit even if it wanted to. Save
// first, then test.
func TestDeliver(cfg Config) error {
	return deliverAll(cfg, Event{Kind: CoreUp, Subject: Host(), At: time.Now()})
}

// DeliverNow sends a pre-composed body to every configured channel, bypassing
// both the enabled-events filter and the suppressor.
//
// The scheduled digests use it rather than Publish: they are not events. Each
// has its own schedule setting, which is already the operator's decision about
// how often to hear from it, and running them through the suppressor would mean
// a daily report silently swallowed by a cooldown.
func DeliverNow(text string) error {
	cfg, ok := currentConfig()
	if !ok || !cfg.Enable {
		return nil
	}
	return deliverAll(cfg, Event{Kind: CoreUp, Subject: Host(), Text: text, At: time.Now()})
}

// SendBackup uploads a database backup to the configured Telegram chats.
//
// Telegram only: a webhook receiver has nowhere to put a file, and mailing a
// database around is a different decision from alerting.
func SendBackup(filename string, data []byte, caption string) error {
	cfg, ok := currentConfig()
	if !ok || !cfg.Enable {
		return nil
	}
	if !cfg.Telegram.enabled() {
		return common.NewError("telegram is not configured")
	}
	var firstErr error
	for _, chatID := range cfg.Telegram.ChatIDs {
		if err := telegramSendDocument(cfg, chatID, filename, data, caption); err != nil {
			logger.Warning("notify: backup upload to ", chatID, " failed: ", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// currentConfig reads the live settings, reporting whether the notifier is up
// at all. Callers outside an event source need this because Publish's silent
// no-op is not appropriate for them -- a scheduled job should log that it could
// not run.
func currentConfig() (Config, bool) {
	stdMu.RLock()
	n := std
	stdMu.RUnlock()
	if n == nil {
		return Config{}, false
	}
	return n.cfg(), true
}

func deliverAll(cfg Config, e Event) error {
	var (
		attempted int
		failures  []string
	)
	try := func(name string, send func() error) {
		attempted++
		if err := send(); err != nil {
			failures = append(failures, name+": "+err.Error())
		}
	}

	if cfg.Telegram.enabled() {
		try("telegram", func() error { return sendTelegram(cfg, e) })
	}
	if cfg.Webhook.enabled() {
		try("webhook", func() error { return sendWebhook(cfg, e) })
	}
	if cfg.SMTP.enabled() {
		try("smtp", func() error { return sendSMTP(cfg, e) })
	}

	if attempted == 0 {
		return common.NewError("no channel is configured")
	}
	if len(failures) > 0 {
		return common.NewError(strings.Join(failures, "; "))
	}
	return nil
}

var (
	clientMu    sync.Mutex
	clientCache *http.Client
	clientProxy string
	clientSet   bool
)

// httpClient returns a client for the configured proxy, rebuilt only when that
// setting changes.
//
// Deliberately plain net/http rather than dialling through the panel's own
// core: the alert that matters most is the one saying the core failed to start,
// and routing it through that core makes it the one alert that can never be
// delivered.
func httpClient(proxy string) *http.Client {
	clientMu.Lock()
	defer clientMu.Unlock()
	if clientSet && clientProxy == proxy {
		return clientCache
	}
	transport := http.DefaultTransport
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			transport = &http.Transport{Proxy: http.ProxyURL(u)}
		} else {
			logger.Warning("notify: ignoring unparsable proxy ", proxy, ": ", err)
		}
	}
	clientCache = &http.Client{Timeout: sendTimeout, Transport: transport}
	clientProxy = proxy
	clientSet = true
	return clientCache
}
