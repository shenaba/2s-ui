package tgbot

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/service"
)

// reportLimit bounds every listing in this file. These are read on a phone, and
// a message long enough to need paging is one nobody reads to the end of.
const reportLimit = 10

// banListLimit is larger: a list of who is being refused right now is exactly
// the thing an operator wants in full during an attack.
const banListLimit = 20

// bansText lists the identities the login limiter is currently refusing.
//
// Both axes are shown with their scope, because they mean different things: an
// ip ban stops one source, a user ban stops one account from every source --
// including the operator's own, which is the case worth being able to see.
func bansText() string {
	var guard service.LoginGuardService
	bans, err := guard.ActiveBans(banListLimit)
	if err != nil {
		return t("err.read", p("detail", err.Error()))
	}
	if len(bans) == 0 {
		return t("bans.none", nil)
	}

	var b strings.Builder
	b.WriteString(t("bans.title", p("count", strconv.Itoa(len(bans)))))
	for _, ban := range bans {
		b.WriteString("\n" + t("bans.line", p(
			"scope", t("bans.scope."+ban.Scope, nil),
			"key", ban.Key,
			"minutes", strconv.Itoa(int((ban.Remaining+time.Minute-1)/time.Minute)),
		)))
	}
	return b.String()
}

// inboundsText lists the inbounds with the numbers that identify them in a
// config: the tag routing rules name, the port a client dials.
func inboundsText() string {
	var inboundService service.InboundService
	all, err := inboundService.GetAll()
	if err != nil {
		return t("err.read", p("detail", err.Error()))
	}
	if all == nil || len(*all) == 0 {
		return t("inbounds.none", nil)
	}

	var b strings.Builder
	b.WriteString(t("inbounds.title", p("count", strconv.Itoa(len(*all)))))
	for _, in := range *all {
		b.WriteString("\n" + inboundLine(in))
	}
	return b.String()
}

// inboundText is one inbound in full, looked up by tag. Tags are unique, and
// they are what every other surface -- routing rules, the share link, the stats
// table -- calls an inbound by.
func inboundText(tag string) string {
	var inboundService service.InboundService
	all, err := inboundService.GetAll()
	if err != nil {
		return t("err.read", p("detail", err.Error()))
	}
	if all == nil {
		return t("inbounds.notFound", p("tag", tag))
	}
	for _, in := range *all {
		if asString(in["tag"]) != tag {
			continue
		}
		var b strings.Builder
		b.WriteString(inboundLine(in))
		if users, ok := in["users"].([]string); ok {
			b.WriteString("\n" + t("inbounds.users", p("count", strconv.Itoa(len(users)))))
			sorted := append([]string(nil), users...)
			sort.Strings(sorted)
			for i, name := range sorted {
				if i == reportLimit {
					b.WriteString("\n" + t("inbounds.more", p("count", strconv.Itoa(len(sorted)-i))))
					break
				}
				b.WriteString("\n" + name)
			}
		}
		return b.String()
	}
	return t("inbounds.notFound", p("tag", tag))
}

func inboundLine(in map[string]interface{}) string {
	line := asString(in["tag"]) + " — " + asString(in["type"])
	if port := asString(in["listen_port"]); port != "" {
		line += ":" + port
	}
	// A replica of an inbound that runs on a managed node: it is in this
	// panel's database to build subscriptions from, but nothing here serves it.
	if _, ok := in["node_id"]; ok {
		line += " " + t("inbounds.onNode", nil)
	}
	return line
}

// trafficText ranks the busiest inbounds and clients of the last day.
//
// The window is fixed at a day on purpose: the useful question from a phone is
// "what is heavy right now", and offering a choice of window means another
// round of buttons for an answer nobody disagrees about.
func trafficText() string {
	var statsService service.StatsService
	since := time.Now().Add(-24 * time.Hour).Unix()

	var b strings.Builder
	b.WriteString(t("traffic.title", nil))

	section := func(titleKey, resource string) {
		rows, err := statsService.TopTags(resource, since, reportLimit)
		if err != nil {
			b.WriteString("\n\n" + t("err.read", p("detail", err.Error())))
			return
		}
		b.WriteString("\n\n" + t(titleKey, nil))
		if len(rows) == 0 {
			b.WriteString("\n" + t("traffic.none", nil))
			return
		}
		for _, row := range rows {
			b.WriteString("\n" + row.Tag + " — " + humanBytes(row.Traffic))
		}
	}
	section("traffic.inbounds", "inbound")
	section("traffic.clients", "user")

	return b.String()
}

// asString renders one of GetAll's map values for display. The promoted columns
// arrive as Go values and everything from the options blob as raw JSON, which
// prints as itself for a number and needs its quotes taken off for a string.
func asString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case json.RawMessage:
		var s string
		if err := json.Unmarshal(value, &s); err == nil {
			return s
		}
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}
