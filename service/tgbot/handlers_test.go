package tgbot

import (
	"strings"
	"testing"

	"github.com/shenaba/2s-ui/service/notify"

	"github.com/go-telegram/bot/models"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in       string
		cmd, arg string
	}{
		{"/status", "status", ""},
		{"/clients bob", "clients", "bob"},
		{"/clients  bob smith  ", "clients", "bob smith"},
		// Group chats address a command to one bot by name.
		{"/status@panelbot", "status", ""},
		{"/inbound@panelbot vless-in", "inbound", "vless-in"},
		// Not a command at all: no verb, and no argument either -- the text is
		// not a search term somebody typed at a prompt.
		{"hello", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		cmd, arg := parseCommand(c.in)
		if cmd != c.cmd || arg != c.arg {
			t.Errorf("parseCommand(%q) = (%q, %q), want (%q, %q)", c.in, cmd, arg, c.cmd, c.arg)
		}
	}
}

// The id a client is bound by is the one a private message arrives under, which
// is the sender -- not the chat, which in a group belongs to the group.
func TestSenderIDPrefersTheSender(t *testing.T) {
	group := &models.Message{
		Chat: models.Chat{ID: -1001234567890},
		From: &models.User{ID: 42},
	}
	if got := senderID(group); got != 42 {
		t.Errorf("senderID in a group = %d, want the sender's 42", got)
	}
	// Channel posts and anonymous group admins carry no sender.
	anonymous := &models.Message{Chat: models.Chat{ID: 7}}
	if got := senderID(anonymous); got != 7 {
		t.Errorf("senderID with no sender = %d, want the chat's 7", got)
	}
}

// What a stranger can get out of the bot is the whole of its exposure, and the
// rule is that none of it identifies the panel: no product name, no hostname,
// and no figures. The bot answers now -- it used to stay silent -- so this is
// the check that replaces that silence.
func TestStrangerRepliesRevealNothing(t *testing.T) {
	// t() is the package's translate helper, which the *testing.T parameter
	// shadows; read the table directly instead of renaming either.
	unknown := botMessages[notify.DefaultLang]["unknownCmd"]
	admin := []string{"nodes", "clients", "online", "backup", "bans", "inbounds", "traffic", "id"}
	for _, cmd := range append(admin, "") {
		if got := strangerReply(cmd); got != unknown {
			t.Errorf("a stranger running /%s got %q, want the unknown-command answer", cmd, got)
		}
	}

	for _, cmd := range []string{"start", "help", "menu", "status", "nodes", ""} {
		got := strangerReply(cmd)
		if got == "" {
			t.Errorf("/%s answered with nothing", cmd)
		}
		for _, secret := range []string{"2S-UI", "2s-ui", notify.Host()} {
			if secret != "" && strings.Contains(got, secret) {
				t.Errorf("/%s leaks %q: %q", cmd, secret, got)
			}
		}
	}

	// The greeting has to point an unbound customer at the one command that
	// helps them, or the command is unreachable by the people who need it.
	if greet := strangerReply("start"); !strings.Contains(greet, "/id") {
		t.Errorf("the greeting does not mention /id: %q", greet)
	}
}

// The public menu is what every stranger sees listed in Telegram; the admin
// menu is published per chat. Mixing them up puts the management commands in
// front of everybody, which is a bigger disclosure than any reply.
func TestPublishedCommandMenusAreSeparate(t *testing.T) {
	public := make(map[string]bool, len(publicCommands))
	for _, name := range publicCommands {
		public[name] = true
	}
	for _, name := range []string{"nodes", "clients", "online", "backup", "bans", "inbounds", "traffic"} {
		if public[name] {
			t.Errorf("%q is published to everybody", name)
		}
	}

	// Every published command needs a description, in every language, or the
	// menu shows a bare key.
	for _, names := range [][]string{publicCommands, adminCommands} {
		for _, name := range names {
			for _, lang := range notify.Langs {
				if botMessages[lang]["cmd."+name] == "" {
					t.Errorf("cmd.%s has no %s description", name, lang)
				}
			}
		}
	}
}
