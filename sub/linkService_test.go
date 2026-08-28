package sub

import (
	"encoding/json"
	"testing"
)

func rawLinks(t *testing.T, links []Link) *json.RawMessage {
	t.Helper()
	data, err := json.Marshal(links)
	if err != nil {
		t.Fatalf("marshal links: %v", err)
	}
	raw := json.RawMessage(data)
	return &raw
}

// GetLinks is now a projection of GetLinkList, and the two are read by
// different callers -- the subscription body and the bot's per-link buttons.
// They have to agree on which links exist and in what order, or a customer
// picks the second button and gets the third link's QR code.
func TestGetLinkListAgreesWithGetLinks(t *testing.T) {
	var s LinkService
	raw := rawLinks(t, []Link{
		{Type: "local", Remark: "tokyo", Uri: "vless://uuid@example.com:443?type=tcp#tokyo"},
		{Type: "external", Remark: "friend", Uri: "trojan://pass@other.example:443#friend"},
		{Type: "local", Remark: "osaka", Uri: "vmess://eyJwcyI6Im9zYWthIn0="},
	})

	list := s.GetLinkList(raw, "all", "")
	flat := s.GetLinks(raw, "all", "")

	if len(list) != 3 || len(flat) != 3 {
		t.Fatalf("got %d structured and %d flat links, want 3 of each", len(list), len(flat))
	}
	for i := range list {
		if list[i].Uri != flat[i] {
			t.Errorf("link %d: structured %q, flat %q", i, list[i].Uri, flat[i])
		}
	}
	// The remark is the whole reason the structured form exists: it is what a
	// per-link button says.
	for i, want := range []string{"tokyo", "friend", "osaka"} {
		if list[i].Remark != want {
			t.Errorf("link %d remark is %q, want %q", i, list[i].Remark, want)
		}
	}
}

// Anything other than "all" is the subscription-conversion path, which takes
// external entries only. Both functions have to drop local links there.
func TestGetLinkListHonoursTheTypeFilter(t *testing.T) {
	var s LinkService
	raw := rawLinks(t, []Link{
		{Type: "local", Remark: "tokyo", Uri: "vless://uuid@example.com:443#tokyo"},
		{Type: "external", Remark: "friend", Uri: "trojan://pass@other.example:443#friend"},
	})

	list := s.GetLinkList(raw, "external", "")
	if len(list) != 1 || list[0].Type != "external" {
		t.Fatalf("filtered list is %+v, want only the external link", list)
	}
	if flat := s.GetLinks(raw, "external", ""); len(flat) != 1 || flat[0] != list[0].Uri {
		t.Errorf("flat list is %v, want only %q", flat, list[0].Uri)
	}
}

// An empty clientInfo has to leave the URI exactly as stored. The bot relies on
// it: that argument appends the remaining traffic and days, which must not end
// up in a QR code handed to the customer.
func TestGetLinkListLeavesTheUriAloneWithoutClientInfo(t *testing.T) {
	var s LinkService
	const uri = "vless://uuid@example.com:443?type=tcp#tokyo"
	raw := rawLinks(t, []Link{{Type: "local", Remark: "tokyo", Uri: uri}})

	if got := s.GetLinkList(raw, "all", "")[0].Uri; got != uri {
		t.Errorf("uri changed with no client info: %q", got)
	}
	if got := s.GetLinkList(raw, "all", " 5.0 GB")[0].Uri; got == uri {
		t.Error("client info was not appended when one was given")
	}
}

// A client with no links at all is the normal state of a freshly created one,
// and it reaches here as an empty or absent array rather than as an error.
func TestGetLinkListToleratesEmptyInput(t *testing.T) {
	var s LinkService
	for _, body := range []string{`[]`, `null`, ``} {
		raw := json.RawMessage(body)
		if got := s.GetLinkList(&raw, "all", ""); len(got) != 0 {
			t.Errorf("input %q produced %+v, want nothing", body, got)
		}
	}
}
