package notify

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// pageLimit is how much text goes into one Telegram message.
//
// Telegram's own cap is 4096, counted in UTF-16 code units rather than bytes or
// runes. Splitting on runes at 2000 keeps the worst case -- text made entirely
// of astral-plane characters, each two UTF-16 units -- at 4000, still under the
// cap, while ordinary text is nowhere near either bound.
const pageLimit = 2000

var (
	hostnameOnce sync.Once
	hostname     string
)

// Host is the name that goes at the top of every message. Without it, an
// operator running more than one panel cannot tell which one is reporting.
func Host() string {
	hostnameOnce.Do(func() {
		h, err := os.Hostname()
		if err != nil || h == "" {
			h = "unknown"
		}
		hostname = h
	})
	return hostname
}

// Render turns an event into the message body for the given language.
func Render(e Event, lang string) string {
	// A pre-composed body wins: the scheduled digests build their own from data
	// this package has no access to, and there is nothing to translate.
	if e.Text != "" {
		return e.Text
	}
	key, params := describe(e)
	return translate(lang, key, params)
}

// describe maps an event onto a message key and its placeholders. It tolerates
// a missing or mistyped Data: the event still renders, just without detail.
func describe(e Event) (string, map[string]string) {
	p := map[string]string{"subject": e.Subject}

	switch e.Kind {
	case NodeDown:
		if d, ok := e.Data.(*NodeData); ok && d.Err != "" {
			p["error"] = d.Err
			return "node.down.err", p
		}
		return "node.down", p
	case NodeUp:
		if d, ok := e.Data.(*NodeData); ok && d.LatencyMs > 0 {
			p["latency"] = strconv.FormatInt(d.LatencyMs, 10)
			return "node.up.latency", p
		}
		return "node.up", p

	case CoreCrash:
		if d, ok := e.Data.(*CoreData); ok && d.Err != "" {
			p["error"] = d.Err
			return "core.crash.err", p
		}
		return "core.crash", p
	case CoreUp:
		return "core.up", p

	case ClientDepleted:
		if d, ok := e.Data.(*ClientData); ok && len(d.Names) > 0 {
			p["count"] = strconv.Itoa(len(d.Names))
			p["names"] = strings.Join(d.Names, ", ")
			return "client.depleted", p
		}
		p["count"] = "0"
		p["names"] = ""
		return "client.depleted", p

	case ClientExpiring:
		if d, ok := e.Data.(*ClientData); ok {
			if d.DaysLeft > 0 {
				p["days"] = strconv.Itoa(d.DaysLeft)
				return "client.expiring.days", p
			}
			if d.BytesLeft > 0 {
				p["volume"] = humanBytes(d.BytesLeft)
				return "client.expiring.volume", p
			}
		}
		return "client.expiring", p

	case CPUHigh, MemoryHigh:
		key := "cpu.high"
		if e.Kind == MemoryHigh {
			key = "memory.high"
		}
		if d, ok := e.Data.(*MetricData); ok {
			p["percent"] = strconv.FormatFloat(d.Percent, 'f', 1, 64)
			p["threshold"] = strconv.Itoa(d.Threshold)
			return key, p
		}
		return key, p

	case LoginSuccess:
		fillLogin(p, e.Data)
		return "login.success", p
	case LoginFailed:
		fillLogin(p, e.Data)
		if p["count"] == "" || p["count"] == "1" {
			return "login.failed.one", p
		}
		return "login.failed", p
	case LoginBanned:
		fillLogin(p, e.Data)
		return "login.banned", p
	}

	return string(e.Kind), p
}

func fillLogin(p map[string]string, data any) {
	d, ok := data.(*LoginData)
	if !ok {
		return
	}
	p["user"] = d.Username
	p["ip"] = d.IP
	if d.Failures > 0 {
		p["count"] = strconv.Itoa(d.Failures)
	}
	if d.BanMinutes > 0 {
		p["minutes"] = strconv.Itoa(d.BanMinutes)
	}
}

// HumanBytes renders a byte count the way both halves of the Telegram
// integration show one.
//
// Exported because service/tgbot needs the same rendering: the operator reads
// a client card from the bot and an expiry alert about that same client, and
// two copies of this would eventually disagree about the figure. Alongside
// Translate, Label, Paginate and Host, which are exported for the same reason.
func HumanBytes(b int64) string { return humanBytes(b) }

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTP"[exp])
}

// Paginate splits a message into chunks no longer than limit runes, preferring
// to break at blank lines, then at line ends, and cutting mid-line only when a
// single line is itself too long.
//
// That last fallback is the point of this function. 3x-ui splits on blank lines
// alone, so a long message that happens to contain none comes back unsplit and
// Telegram rejects the whole thing with "400: message is too long" -- which
// reads as the bot silently doing nothing. Any batch message here (a depletion
// pass naming every disabled client) is exactly that shape.
func Paginate(msg string, limit int) []string {
	if limit <= 0 || countRunes(msg) <= limit {
		return []string{msg}
	}

	var pages []string
	appendChunk := func(sep, chunk string) {
		if n := len(pages) - 1; n >= 0 && countRunes(pages[n])+countRunes(sep)+countRunes(chunk) <= limit {
			pages[n] += sep + chunk
			return
		}
		pages = append(pages, chunk)
	}

	for _, para := range strings.Split(msg, "\n\n") {
		first := true
		for _, line := range strings.Split(para, "\n") {
			for _, chunk := range hardSplit(line, limit) {
				sep := "\n"
				if first {
					sep = "\n\n"
					first = false
				}
				appendChunk(sep, chunk)
			}
		}
	}
	if len(pages) == 0 {
		return []string{msg}
	}
	return pages
}

// hardSplit cuts s into runs of at most limit runes. Ranging over the string
// yields whole runes, so a multi-byte character is never torn in half -- doing
// this on bytes would emit invalid UTF-8 and Telegram would reject it.
func hardSplit(s string, limit int) []string {
	if countRunes(s) <= limit {
		return []string{s}
	}
	var (
		out   []string
		start int
		n     int
	)
	for i := range s {
		if n == limit {
			out = append(out, s[start:i])
			start = i
			n = 0
		}
		n++
	}
	return append(out, s[start:])
}

func countRunes(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
