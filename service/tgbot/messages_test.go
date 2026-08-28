package tgbot

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/service/notify"
)

var placeholder = regexp.MustCompile(`\{[a-zA-Z]+\}`)

// Every language must carry every key. A missing one falls back to English,
// which is survivable, but it shows up as one English button in an otherwise
// translated menu -- exactly the half-translated look this table exists to fix.
func TestBotMessagesCoverEveryLanguage(t *testing.T) {
	en, ok := botMessages[notify.DefaultLang]
	if !ok {
		t.Fatal("no English table")
	}
	if len(en) < 50 {
		t.Fatalf("the English table has only %d keys; that cannot be the whole bot", len(en))
	}

	for _, lang := range notify.Langs {
		table, ok := botMessages[lang]
		if !ok {
			t.Errorf("%s has no table at all", lang)
			continue
		}
		var missing, extra []string
		for k := range en {
			if table[k] == "" {
				missing = append(missing, k)
			}
		}
		for k := range table {
			if en[k] == "" {
				extra = append(extra, k)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 {
			t.Errorf("%s is missing %d keys: %v", lang, len(missing), missing)
		}
		if len(extra) > 0 {
			t.Errorf("%s has %d keys English does not: %v", lang, len(extra), extra)
		}
	}
}

// A translation that drops a placeholder silently drops the information it
// carried -- "已启用。" instead of "alice：已启用。" -- and nothing at runtime
// notices, because substitution on a missing placeholder is a no-op.
func TestBotMessagesKeepTheirPlaceholders(t *testing.T) {
	en := botMessages[notify.DefaultLang]

	for _, lang := range notify.Langs {
		if lang == notify.DefaultLang {
			continue
		}
		for key, enText := range en {
			text, ok := botMessages[lang][key]
			if !ok || text == "" {
				continue // reported by the coverage test above
			}
			want := placeholder.FindAllString(enText, -1)
			got := placeholder.FindAllString(text, -1)
			sort.Strings(want)
			sort.Strings(got)
			if strings.Join(want, ",") != strings.Join(got, ",") {
				t.Errorf("%s/%s: placeholders %v, English has %v", lang, key, got, want)
			}
		}
	}
}

// The keys the code asks for have to exist, or the operator sees a bare
// identifier like "client.doneReset" where a sentence should be.
func TestKeysUsedByTheCodeExist(t *testing.T) {
	used := []string{
		"cmd.start", "cmd.status", "cmd.nodes", "cmd.clients", "cmd.online", "cmd.backup",
		"cmd.help", "cmd.id", "cmd.inbounds", "cmd.traffic", "cmd.bans",
		"btn.status", "btn.nodes", "btn.clients", "btn.online", "btn.backup", "btn.restart",
		"btn.new", "btn.confirm", "btn.cancel", "btn.back", "btn.menu", "btn.open",
		"btn.refresh", "btn.enable", "btn.disable", "btn.reset", "btn.bind", "btn.rebind",
		"btn.inbounds", "btn.traffic", "btn.bans", "btn.bansClear", "btn.search",
		"btn.prev", "btn.next", "btn.links",
		"unknownCmd", "expired", "cancelled", "err.read", "err.save",
		"greet", "alive", "id.reply",
		"nodes.none", "nodes.title", "nodes.online", "nodes.offline", "nodes.stopped",
		"nodes.disabled", "nodes.unknown",
		"online.none", "online.title", "backup.failed",
		"bans.none", "bans.title", "bans.line", "bans.scope.ip", "bans.scope.user",
		"bans.scope.prompt", "bans.confirmClear", "bans.cleared",
		"inbounds.none", "inbounds.title", "inbounds.notFound", "inbounds.users",
		"inbounds.more", "inbounds.onNode",
		"traffic.title", "traffic.inbounds", "traffic.clients", "traffic.none",
		"core.confirm", "core.restarted", "core.failed",
		"client.notFound", "client.listEmpty", "client.listTitle",
		"client.range", "client.searchTitle", "client.searchEmpty", "client.searchPrompt",
		"client.on", "client.off", "client.unlimited", "client.expires", "client.never",
		"client.upDown", "client.group", "client.telegram", "client.desc",
		"client.confirmToggleOn", "client.confirmToggleOff", "client.confirmReset",
		"client.doneEnabled", "client.doneDisabled", "client.doneReset",
		"bind.prompt", "bind.invalid", "bind.removed", "bind.done",
		"bind.pick", "bind.pickEmpty", "bind.pickStale",
		"form.name", "form.nameEmpty", "form.nameTaken", "form.volume", "form.volumeBad",
		"form.expiry", "form.expiryBad", "form.summary", "form.sumName", "form.sumTraffic",
		"form.sumUnlim", "form.sumExpiry", "form.sumNever", "form.sumInbound",
		"form.expiredMsg", "form.created",
		"self.unavailable", "self.left", "self.exhausted", "self.unlimited",
		"self.disabled", "self.sub",
		"links.none", "links.title", "links.more", "links.unnamed", "links.noQr",
		"links.subBase", "links.subClash", "links.subJson",
	}
	en := botMessages[notify.DefaultLang]
	for _, k := range used {
		if en[k] == "" {
			t.Errorf("the code uses %q but the table has no English text for it", k)
		}
	}
}

// nodeStateText covers the states NodeStatus actually reports; an unmapped one
// must still read as something, not as an empty string.
func TestNodeStateTextHandlesEveryState(t *testing.T) {
	for _, state := range []string{"online", "offline", "core-stopped", "", "something-new"} {
		if got := nodeStateText(state); got == "" {
			t.Errorf("state %q rendered empty", state)
		}
	}
}
