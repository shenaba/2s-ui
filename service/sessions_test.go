package service

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/shenaba/2s-ui/database/model"
)

// The resource is checked with or without a tag: a misspelt one and no tag must
// not fall through to "every user's connections".
func TestLocalSessionsResource(t *testing.T) {
	for _, tc := range []struct {
		resource, tag string
		ok            bool
	}{
		{"", "", true},
		{"user", "", true},
		{"user", "alice", true},
		{"inbound", "vless-in", true},
		{"outbound", "direct", true},
		{"users", "", false},
		{"users", "alice", false},
		{"", "alice", false},
	} {
		_, err := LocalSessions(tc.resource, tc.tag)
		if (err == nil) != tc.ok {
			t.Errorf("LocalSessions(%q, %q) err = %v, want ok=%v", tc.resource, tc.tag, err, tc.ok)
		}
	}
}

func TestEachNode(t *testing.T) {
	nodes := []*model.Node{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	var merged []string
	errs := eachNode(nodes,
		func(n *model.Node) (string, error) {
			if n.Name == "b" {
				return "", errors.New("unreachable")
			}
			return n.Name, nil
		},
		// No lock here on purpose: eachNode promises merge runs under its own.
		func(_ *model.Node, v string) { merged = append(merged, v) })

	sort.Strings(merged)
	if !reflect.DeepEqual(merged, []string{"a", "c"}) {
		t.Errorf("merged = %v, want [a c]", merged)
	}
	if !reflect.DeepEqual(errs, map[string]string{"b": "unreachable"}) {
		t.Errorf("errs = %v", errs)
	}

	// All answered: nil, so the omitempty Errors fields stay out of the JSON.
	if errs := eachNode(nodes[:1],
		func(n *model.Node) (int, error) { return 1, nil },
		func(*model.Node, int) {}); errs != nil {
		t.Errorf("errs = %v, want nil", errs)
	}
}
