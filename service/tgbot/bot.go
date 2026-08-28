// Package tgbot is the interactive half of the Telegram integration: it takes
// commands, where service/notify only sends alerts.
//
// It sits above service rather than inside it. Every write it performs goes
// through ConfigService.Save, the same path the HTTP API uses -- see exec.go
// for why that is not optional.
package tgbot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/service"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	// configPoll is how often the supervisor re-reads the settings. The bot has
	// no reload entry point on purpose: service cannot call into this package
	// without closing an import cycle, and a poll this slow costs one indexed
	// query per interval.
	configPoll = 20 * time.Second
	// retryDelay backs off after a failed connect (bad token, no network) so a
	// misconfigured bot does not spin.
	retryDelay = 30 * time.Second
	// httpTimeout has to exceed the long-poll timeout below, or every poll ends
	// as a client-side timeout.
	pollTimeout = 30
	httpTimeout = 60 * time.Second
)

var (
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
)

// Start brings the bot supervisor up. It returns immediately; the supervisor
// connects, reconnects on a credential change and stays down while the bot is
// switched off.
func Start() {
	mu.Lock()
	defer mu.Unlock()
	if running {
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	cancel = stop
	running = true
	go supervise(ctx)
}

// Stop tears the supervisor down. Safe when it was never started.
func Stop() {
	mu.Lock()
	stop := cancel
	cancel = nil
	running = false
	mu.Unlock()
	if stop != nil {
		stop()
	}
}

// supervise owns the connect/disconnect lifecycle.
//
// Structured as a loop around a blocking session rather than a long-lived bot
// with a reload method: the SDK's Start owns its polling loop and ends only
// when its context does, so "reconnect with a new token" is most honestly
// expressed as ending one session and starting the next.
func supervise(ctx context.Context) {
	var settingService service.SettingService
	for {
		if ctx.Err() != nil {
			return
		}
		cfg := settingService.GetBotConfig()
		if !cfg.Runnable() {
			if !sleep(ctx, configPoll) {
				return
			}
			continue
		}
		if err := runSession(ctx, cfg); err != nil {
			logger.Warning("tgbot: session ended: ", err)
			if !sleep(ctx, retryDelay) {
				return
			}
		}
	}
}

// runSession blocks for as long as one connection lasts: until the panel shuts
// down, until the bot is switched off, or until its credentials change.
func runSession(ctx context.Context, cfg service.BotConfig) error {
	opts := []bot.Option{
		bot.WithDefaultHandler(dispatch),
		bot.WithHTTPClient(httpTimeout, httpClient(cfg.Proxy)),
		// Only what is actually handled. Anything else is bandwidth spent on
		// updates that get dropped, and Telegram keeps re-sending until the
		// offset moves past them.
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message", "callback_query"}),
	}
	if cfg.APIServer != "" {
		opts = append(opts, bot.WithServerURL(cfg.APIServer))
	}

	b, err := bot.New(cfg.Token, opts...)
	if err != nil {
		return err
	}

	sessionCtx, stop := context.WithCancel(ctx)
	defer stop()
	go watchConfig(sessionCtx, stop, cfg.Connection())

	setCommands(sessionCtx, b, cfg)
	logger.Info("tgbot: connected")
	// Returns when sessionCtx is done -- panel shutdown, or watchConfig seeing
	// the credentials change.
	b.Start(sessionCtx)
	logger.Info("tgbot: disconnected")
	return nil
}

// watchConfig ends the session when the credentials change or the bot is turned
// off. Only the connection fields count: re-reading the admin list on every
// command means an edit there takes effect without dropping the connection.
func watchConfig(ctx context.Context, stop context.CancelFunc, connected string) {
	var settingService service.SettingService
	for {
		if !sleep(ctx, configPoll) {
			return
		}
		cfg := settingService.GetBotConfig()
		if !cfg.Runnable() || cfg.Connection() != connected {
			logger.Info("tgbot: settings changed, reconnecting")
			stop()
			return
		}
	}
}

// setCommands publishes the in-app command menu.
//
// Twice, at two scopes: the harmless commands to everyone, and the full list
// only to the chats that can run it. Telegram shows the narrowest matching
// scope, so an admin sees their own menu and everybody else sees the short one.
// Without the split a stranger opening the menu would read off /nodes, /bans
// and /backup, which says more about what is behind the bot than any of its
// answers do.
func setCommands(ctx context.Context, b *bot.Bot, cfg service.BotConfig) {
	publish := func(names []string, scope models.BotCommandScope) {
		cmds := make([]models.BotCommand, 0, len(names))
		for _, name := range names {
			cmds = append(cmds, models.BotCommand{Command: name, Description: t("cmd."+name, nil)})
		}
		if _, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: cmds, Scope: scope}); err != nil {
			// Cosmetic only -- it populates the in-app command menu. Every
			// command works regardless, so this must not fail the session.
			logger.Warning("tgbot: publishing the command list failed: ", err)
		}
	}

	publish(publicCommands, nil)
	for _, admin := range cfg.Admins {
		id, err := strconv.ParseInt(admin, 10, 64)
		if err != nil {
			logger.Warning("tgbot: ignoring an unparsable admin chat id ", admin)
			continue
		}
		publish(adminCommands, &models.BotCommandScopeChat{ChatID: id})
	}
}

func httpClient(proxy string) *http.Client {
	transport := http.DefaultTransport
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			transport = &http.Transport{Proxy: http.ProxyURL(u)}
		} else {
			logger.Warning("tgbot: ignoring unparsable proxy ", proxy, ": ", err)
		}
	}
	return &http.Client{Timeout: httpTimeout, Transport: transport}
}

// sleep waits for d, reporting false if the context ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
