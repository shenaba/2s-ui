package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The bug this exists to prevent, seen in 3x-ui on 2026-08-20: their splitter
// breaks on blank lines only, so a long message containing none comes back
// unsplit and Telegram rejects the whole thing with "400: message is too long".
// The bot then looks like it silently did nothing. A depletion pass naming every
// disabled client is exactly that shape.
func TestPaginateHardSplitsUnbrokenText(t *testing.T) {
	const limit = 50
	msg := strings.Repeat("abcdefghij", 40) // 400 chars, no line breaks at all

	pages := Paginate(msg, limit)
	if len(pages) < 8 {
		t.Fatalf("expected the text to be split into at least 8 pages, got %d", len(pages))
	}
	for i, p := range pages {
		if n := countRunes(p); n > limit {
			t.Errorf("page %d is %d runes, over the %d limit", i, n, limit)
		}
	}
	if joined := strings.Join(pages, ""); joined != msg {
		t.Errorf("pages do not reassemble into the original message")
	}
}

// Splitting on bytes would tear a multi-byte character in half and Telegram
// would reject the result as malformed.
func TestPaginateKeepsUTF8Intact(t *testing.T) {
	const limit = 10
	// Chinese and an astral-plane emoji: 3 and 4 bytes per rune respectively.
	msg := strings.Repeat("节点已离线", 20) + strings.Repeat("\U0001F534", 20)

	pages := Paginate(msg, limit)
	for i, p := range pages {
		if !utf8.ValidString(p) {
			t.Errorf("page %d is not valid UTF-8", i)
		}
		if n := countRunes(p); n > limit {
			t.Errorf("page %d is %d runes, over the %d limit", i, n, limit)
		}
	}
	if joined := strings.Join(pages, ""); joined != msg {
		t.Error("pages do not reassemble into the original message")
	}
}

func TestPaginateLeavesShortMessagesAlone(t *testing.T) {
	msg := "\U0001F534 Node hk-01 is down"
	pages := Paginate(msg, pageLimit)
	if len(pages) != 1 || pages[0] != msg {
		t.Fatalf("a short message was altered: %q", pages)
	}
}

// Given the choice, a break should land at a paragraph or line boundary rather
// than mid-sentence.
func TestPaginatePrefersNaturalBreaks(t *testing.T) {
	const limit = 30
	msg := "first paragraph here\n\nsecond paragraph here\n\nthird paragraph here"

	pages := Paginate(msg, limit)
	for i, p := range pages {
		if n := countRunes(p); n > limit {
			t.Fatalf("page %d is %d runes, over the %d limit", i, n, limit)
		}
		if strings.HasPrefix(p, " ") || strings.HasSuffix(p, " ") {
			t.Errorf("page %d broke mid-word: %q", i, p)
		}
	}
	// No content lost, ignoring how the whitespace got redistributed.
	strip := func(s string) string { return strings.Join(strings.Fields(s), "") }
	if strip(strings.Join(pages, " ")) != strip(msg) {
		t.Error("content was lost while paginating")
	}
}

// A source that forgets to attach Data, or attaches the wrong type, should
// degrade to a plainer message -- never panic. Publish runs on the login path.
func TestRenderToleratesMissingData(t *testing.T) {
	for _, k := range AllKinds {
		for _, data := range []any{nil, "not the right type", &MetricData{}} {
			got := Render(Event{Kind: k, Subject: "x", Data: data}, "en")
			if got == "" {
				t.Errorf("%s with %T data rendered empty", k, data)
			}
		}
	}
}

func TestRenderFallsBackPerKey(t *testing.T) {
	e := Event{Kind: NodeDown, Subject: "hk-01"}

	// An unknown language falls back to English rather than to the raw key.
	if got := Render(e, "kl-GL"); !strings.Contains(got, "hk-01") || !strings.Contains(got, "down") {
		t.Errorf("unknown language did not fall back to English: %q", got)
	}
	// Every shipped language renders every key with the subject substituted.
	for _, lang := range Langs {
		got := Render(e, lang)
		if !strings.Contains(got, "hk-01") {
			t.Errorf("%s: subject not substituted: %q", lang, got)
		}
		if strings.Contains(got, "{") {
			t.Errorf("%s: unsubstituted placeholder left in: %q", lang, got)
		}
	}
}

// The client's copy of an alert is a different message from the operator's, and
// the difference is the point: it must not carry the panel's hostname, and it
// must exist in every shipped language.
func TestRenderClientSpeaksToTheClient(t *testing.T) {
	target := ClientTarget{Name: "alice", TgId: 42, DaysLeft: 3}
	volume := ClientTarget{Name: "alice", TgId: 42, BytesLeft: 5 << 30}

	cases := []struct {
		what   string
		event  Event
		target ClientTarget
		want   string
	}{
		{"days left", Event{Kind: ClientExpiring}, target, "3"},
		{"volume left", Event{Kind: ClientExpiring}, volume, "5.0 GiB"},
		{"depleted", Event{Kind: ClientDepleted}, ClientTarget{Name: "alice", TgId: 42}, "alice"},
	}
	for _, c := range cases {
		for _, lang := range Langs {
			got := RenderClient(c.event, c.target, lang)
			if got == "" {
				t.Errorf("%s in %s rendered empty", c.what, lang)
				continue
			}
			if strings.Contains(got, "{") {
				t.Errorf("%s in %s left a placeholder in: %q", c.what, lang, got)
			}
			if !strings.Contains(got, "alice") {
				t.Errorf("%s in %s does not name the client: %q", c.what, lang, got)
			}
			if strings.Contains(got, Host()) {
				t.Errorf("%s in %s leaks the panel hostname: %q", c.what, lang, got)
			}
		}
		if got := RenderClient(c.event, c.target, "en"); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q does not contain %q", c.what, got, c.want)
		}
	}
}

// Anything the client has no business hearing about renders empty, so adding a
// kind to the bus cannot silently start messaging customers.
func TestRenderClientIsSilentForOperatorKinds(t *testing.T) {
	target := ClientTarget{Name: "alice", TgId: 42, DaysLeft: 3}
	for _, k := range AllKinds {
		if k == ClientExpiring || k == ClientDepleted {
			continue
		}
		if got := RenderClient(Event{Kind: k}, target, "en"); got != "" {
			t.Errorf("%s rendered a client message: %q", k, got)
		}
	}
	// An expiring event that tripped neither margin has nothing to tell the
	// client, so it is not sent one.
	if got := RenderClient(Event{Kind: ClientExpiring}, ClientTarget{Name: "alice"}, "en"); got != "" {
		t.Errorf("an expiry with no margin rendered: %q", got)
	}
}

// Every key used by describe() must exist in English, or that event renders as
// a bare identifier like "client.expiring.volume" in every language.
func TestEveryMessageKeyIsTranslated(t *testing.T) {
	events := []Event{
		{Kind: NodeDown, Data: &NodeData{Err: "dial timeout"}},
		{Kind: NodeDown},
		{Kind: NodeUp, Data: &NodeData{LatencyMs: 42}},
		{Kind: NodeUp},
		{Kind: CoreCrash, Data: &CoreData{Err: "bad config"}},
		{Kind: CoreCrash},
		{Kind: CoreUp},
		{Kind: OutboundDown, Data: &OutboundData{Err: "dial timeout"}},
		{Kind: OutboundDown},
		{Kind: OutboundUp, Data: &OutboundData{LatencyMs: 42}},
		{Kind: OutboundUp},
		{Kind: ClientDepleted, Data: &ClientData{Names: []string{"a", "b"}}},
		{Kind: ClientExpiring, Data: &ClientData{DaysLeft: 3}},
		{Kind: ClientExpiring, Data: &ClientData{BytesLeft: 5 << 30}},
		{Kind: ClientExpiring},
		{Kind: CPUHigh, Data: &MetricData{Percent: 91.5, Threshold: 80}},
		{Kind: MemoryHigh, Data: &MetricData{Percent: 91.5, Threshold: 80}},
		{Kind: LoginSuccess, Data: &LoginData{Username: "admin", IP: "1.2.3.4"}},
		{Kind: LoginFailed, Data: &LoginData{Username: "admin", IP: "1.2.3.4", Failures: 1}},
		{Kind: LoginFailed, Data: &LoginData{Username: "admin", IP: "1.2.3.4", Failures: 7}},
		{Kind: LoginBanned, Data: &LoginData{IP: "1.2.3.4", BanMinutes: 15}},
	}
	for _, e := range events {
		key, _ := describe(e)
		if _, ok := messages[DefaultLang][key]; !ok {
			t.Errorf("message key %q has no English text", key)
		}
		for _, lang := range Langs {
			if _, ok := messages[lang][key]; !ok {
				t.Errorf("message key %q is missing from %q", key, lang)
			}
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:           "512 B",
		5 << 30:       "5.0 GiB",
		1536:          "1.5 KiB",
		3 * (1 << 40): "3.0 TiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// The scheduled digests compose their own body, so Render has to pass it
// through untouched -- including in a language whose message table would
// otherwise supply something for the Kind it is nominally tagged with.
func TestRenderPrefersPreComposedText(t *testing.T) {
	body := "panel-1\nCPU 12.3% · MEM 45%\nClients 42 (8 online)"
	e := Event{Kind: CoreUp, Subject: "panel-1", Text: body}

	for _, lang := range append([]string{"en"}, Langs...) {
		if got := Render(e, lang); got != body {
			t.Errorf("%s: pre-composed text was rewritten:\n got %q\nwant %q", lang, got, body)
		}
	}

	// Without Text it still renders the kind, so the field is an override and
	// not a required one.
	if got := Render(Event{Kind: CoreUp}, "en"); got == "" || got == body {
		t.Errorf("an event with no Text did not render its kind: %q", got)
	}
}
