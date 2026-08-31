package tgbot

import "testing"

// A chat-scoped command list is state on Telegram's side, so what setCommands
// has to delete is whatever it published and no longer would. Getting this
// wrong either leaves a removed admin reading the management menu or deletes
// the menu of an admin who is still one.
func TestRevokedListsOnlyDroppedChats(t *testing.T) {
	cases := []struct {
		name     string
		previous []string
		current  []string
		want     []string
	}{
		{"first publish has nothing to revoke", nil, []string{"111", "222"}, nil},
		{"unchanged list revokes nothing", []string{"111"}, []string{"111"}, nil},
		{"a removed admin is revoked", []string{"111", "222"}, []string{"111"}, []string{"222"}},
		{"an added admin revokes nothing", []string{"111"}, []string{"111", "222"}, nil},
		{"clearing the list revokes everyone", []string{"111", "222"}, nil, []string{"111", "222"}},
		{"a reorder is not a removal", []string{"111", "222"}, []string{"222", "111"}, nil},
		{"swapping one out revokes only that one", []string{"111", "222"}, []string{"222", "333"}, []string{"111"}},
	}

	for _, c := range cases {
		got := revoked(c.previous, c.current)
		if len(got) != len(c.want) {
			t.Errorf("%s: revoked(%v, %v) = %v, want %v", c.name, c.previous, c.current, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: revoked(%v, %v) = %v, want %v", c.name, c.previous, c.current, got, c.want)
				break
			}
		}
	}
}

// stale decides whether the menus are republished at all, and the field it
// reads is the whole point: the setting as it was read, not the chats that were
// actually reached.
func TestMenuStateStale(t *testing.T) {
	cases := []struct {
		name    string
		menus   menuState
		setting []string
		want    bool
	}{
		{"nothing published, nothing configured", menuState{}, nil, false},
		{"unchanged", menuState{admins: []string{"111"}, published: []string{"111"}},
			[]string{"111"}, false},
		{"an admin added", menuState{admins: []string{"111"}, published: []string{"111"}},
			[]string{"111", "222"}, true},
		{"an admin removed", menuState{admins: []string{"111", "222"}, published: []string{"111", "222"}},
			[]string{"111"}, true},
		{"reordered", menuState{admins: []string{"111", "222"}, published: []string{"111", "222"}},
			[]string{"222", "111"}, true},
		// The case the two fields exist for. setCommands could not parse
		// "not-an-id", so it never reached published -- comparing the setting
		// against published would call this stale on every poll and republish
		// every menu every twenty seconds forever.
		{"an entry that could not be published is not a change",
			menuState{admins: []string{"111", "not-an-id"}, published: []string{"111"}},
			[]string{"111", "not-an-id"}, false},
		// A publish that failed leaves retry set, so the next poll tries again
		// even though the setting itself has not moved.
		{"a failed publish is retried",
			menuState{admins: []string{"111"}, published: nil, retry: true},
			[]string{"111"}, true},
	}

	for _, c := range cases {
		if got := c.menus.stale(c.setting); got != c.want {
			t.Errorf("%s: stale(%v) on %+v = %v, want %v", c.name, c.setting, c.menus, got, c.want)
		}
	}
}
