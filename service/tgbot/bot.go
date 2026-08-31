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
	"slices"
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

	menus := setCommands(sessionCtx, b, cfg.Admins, menuState{})
	go watchConfig(sessionCtx, stop, b, cfg.Connection(), menus)

	logger.Info("tgbot: connected")
	// Returns when sessionCtx is done -- panel shutdown, or watchConfig seeing
	// the credentials change.
	b.Start(sessionCtx)
	logger.Info("tgbot: disconnected")
	return nil
}

// watchConfig ends the session when the credentials change or the bot is turned
// off, and keeps the per-admin command menus in step in between.
//
// Only the connection fields end the session: re-reading the admin list on
// every command means an edit there takes effect without dropping the
// connection. The menus are the one part of an admin-list edit that does not
// take care of itself, because they are state held on Telegram's side.
func watchConfig(ctx context.Context, stop context.CancelFunc, b *bot.Bot, connected string, menus menuState) {
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
		if menus.stale(cfg.Admins) {
			menus = setCommands(ctx, b, cfg.Admins, menus)
		}
	}
}

// menuState is what setCommands published last.
//
// Both halves are needed. admins is the setting exactly as it was read, and is
// what an edit is detected against: an unparsable entry never reaches
// published, so comparing the setting to that instead would look like a change
// on every single poll and republish every menu every twenty seconds forever.
// published is the chats actually reached, which is what a later call has to
// revoke.
type menuState struct {
	admins    []string
	published []string
}

// stale reports whether the admin setting has moved since these menus went out.
//
// Against admins, never against published -- that choice is the whole reason
// menuState has two fields, and swapping them turns an admin list with one
// unparsable entry into a republish of every menu every twenty seconds. The
// setting is an ordered list, so a reorder counts as a change and merely
// republishes.
func (m menuState) stale(admins []string) bool {
	return !slices.Equal(m.admins, admins)
}

// setCommands publishes the in-app command menus and takes back the ones that
// no longer apply. It returns the admin chats it published to, which is what a
// later call needs as its previous.
//
// Two scopes: the harmless commands to everyone, and the full list only to the
// chats that can run it. Telegram shows the narrowest matching scope, so an
// admin sees their own menu and everybody else sees the short one. Without the
// split a stranger opening the menu would read off /nodes, /bans and /backup,
// which says more about what is behind the bot than any of its answers do.
//
// A chat-scoped list lives on Telegram's side, so dropping someone from the
// admin setting does not by itself take their menu away -- it has to be
// deleted, or a removed admin goes on reading the panel's whole management
// surface off their command menu, which is the disclosure the split exists to
// prevent. previous is what this session published last, and watchConfig calls
// back in on every admin-list edit. The bookkeeping is in memory, so an admin
// removed while the panel is down keeps their menu; closing that would mean a
// settings row to remember a list of command names by.
func setCommands(ctx context.Context, b *bot.Bot, admins []string, previous menuState) menuState {
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

	current := make([]string, 0, len(admins))
	for _, admin := range admins {
		id, err := strconv.ParseInt(admin, 10, 64)
		if err != nil {
			logger.Warning("tgbot: ignoring an unparsable admin chat id ", admin)
			continue
		}
		publish(adminCommands, &models.BotCommandScopeChat{ChatID: id})
		current = append(current, admin)
	}

	for _, gone := range revoked(previous.published, current) {
		id, err := strconv.ParseInt(gone, 10, 64)
		if err != nil {
			continue
		}
		// Deleting the chat scope falls that chat back to the public menu,
		// which is what a non-admin should have been seeing all along.
		if _, err := b.DeleteMyCommands(ctx, &bot.DeleteMyCommandsParams{
			Scope: &models.BotCommandScopeChat{ChatID: id},
		}); err != nil {
			logger.Warning("tgbot: revoking the admin menu for ", gone, " failed: ", err)
		}
	}
	return menuState{admins: admins, published: current}
}

// revoked lists the chats that were published to and no longer are. Linear on
// purpose: this is the operator's own admin chat list, one entry on nearly
// every install and a handful on the largest.
func revoked(previous, current []string) []string {
	var gone []string
	for _, id := range previous {
		if !slices.Contains(current, id) {
			gone = append(gone, id)
		}
	}
	return gone
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
