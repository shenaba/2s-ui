package tgbot

import (
	"path/filepath"
	"testing"

	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/database/model"
	"github.com/shenaba/2s-ui/logger"

	"github.com/go-telegram/bot/models"
	"github.com/op/go-logging"
)

// The query strings here have to match what sub.SubHandler switches on. They
// are the whole difference between handing someone a Clash config and handing
// them the base64 list again, and nothing else connects the two files.
func TestSubscriptionLinksCoverEveryServedFormat(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "links.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	if err := database.GetDB().Create(&model.Setting{
		Key: "subURI", Value: "https://example.com/sub/",
	}).Error; err != nil {
		t.Fatalf("seed subURI: %v", err)
	}

	got := subscriptionLinks("alice")
	want := []string{
		"https://example.com/sub/alice",
		"https://example.com/sub/alice?format=clash",
		"https://example.com/sub/alice?format=json",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d subscription entries, want %d: %+v", len(got), len(want), got)
	}
	for i, uri := range want {
		if got[i].Uri != uri {
			t.Errorf("entry %d is %q, want %q", i, got[i].Uri, uri)
		}
		if got[i].Remark == "" {
			t.Errorf("entry %d has no label to put on its button", i)
		}
	}
	// The bare URL comes first: it is what most client apps take, and the two
	// format variants are for the ones that cannot.
	if got[0].Uri != "https://example.com/sub/alice" {
		t.Errorf("the plain subscription is not first: %q", got[0].Uri)
	}
}

// With no subscription URI configured there is nothing to hand out, and the
// individual links have to remain reachable rather than the whole listing
// failing.
func TestSubscriptionLinksAreSkippedWhenUnconfigured(t *testing.T) {
	logger.InitLogger(logging.CRITICAL)

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "nosub.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.CloseDBForTest(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	// No subURI row, and no cert or domain either, so GetFinalSubURI derives
	// nothing useful -- whatever it returns, an empty one must not become a
	// button pointing at a bare host.
	for _, link := range subscriptionLinks("alice") {
		if link.Uri == "" || link.Uri == "alice" {
			t.Errorf("produced an unusable subscription link: %q", link.Uri)
		}
	}
}

// buttonGrid is what keeps a page of clients from being a page of scrolling.
func TestButtonGridColumns(t *testing.T) {
	cases := []struct {
		count, wantRows, wantFirstRow int
	}{
		{0, 0, 0},
		{1, 1, 1},
		{3, 1, 3},
		{5, 2, 3},
		// Six or more drops to two columns, so longer names still fit.
		{6, 3, 2},
		{20, 10, 2},
	}
	for _, c := range cases {
		buttons := make([]models.InlineKeyboardButton, c.count)
		rows := buttonGrid(buttons)
		if len(rows) != c.wantRows {
			t.Errorf("%d buttons produced %d rows, want %d", c.count, len(rows), c.wantRows)
			continue
		}
		if c.count > 0 && len(rows[0]) != c.wantFirstRow {
			t.Errorf("%d buttons put %d in the first row, want %d", c.count, len(rows[0]), c.wantFirstRow)
		}
		total := 0
		for _, row := range rows {
			total += len(row)
		}
		if total != c.count {
			t.Errorf("%d buttons came back as %d -- the grid dropped some", c.count, total)
		}
	}
}
