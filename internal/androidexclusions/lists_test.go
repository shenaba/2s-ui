package androidexclusions

import (
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	a, e := Parse([]byte("\ufeff# com.comment.hidden\r\ncom.a, 'ru.bank'\r\ncom.a\ncom.b # comment\nhttps://example.com/com.bad\n<html>com.bad</html>\ntext here\n1234\n"))
	if e != nil || !reflect.DeepEqual(a, []string{"com.a", "com.b", "ru.bank"}) {
		t.Fatalf("%v %v", a, e)
	}
	b, e := Parse([]byte("ru.bank\ncom.b\n# new comment\ncom.a\n"))
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal(b, e)
	}
	for _, s := range []string{"", "# comment\nhttps://example.com", strings.Repeat("x", MaxBytes+1)} {
		if _, e := Parse([]byte(s)); e == nil {
			t.Fatal("unexpected successful parse")
		}
	}
	a, e = Parse([]byte(`["com.a","junk", 123, null, {"not":"com.fake"}, "ru.bank", "com.a"]`))
	if e != nil || !reflect.DeepEqual(a, []string{"com.a", "ru.bank"}) {
		t.Fatal(a, e)
	}
}
func TestFilter(t *testing.T) {
	r := map[string]any{"rules": []any{map[string]any{"package_name": []any{"com.a"}, "action": "reject"}, map[string]any{"type": "logical", "rules": []any{map[string]any{"package_name_regex": "^com\\.video\\."}}}, map[string]any{"package_name": "com.a", "outbound": "direct"}}}
	a, n, e := Filter([]string{"com.a", "com.a", "com.video.app", "ru.bank"}, r)
	if e != nil || n != 2 || !reflect.DeepEqual(a, []string{"ru.bank"}) {
		t.Fatal(a, n, e)
	}
	if _, _, e := Filter([]string{"com.a"}, map[string]any{"package_name_regex": "["}); e == nil {
		t.Fatal("invalid regexp accepted")
	}
}
func TestAddressPolicy(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "192.168.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::7f00:1", "198.18.0.1", "192.0.2.1"} {
		if publicIP(netip.MustParseAddr(s)) {
			t.Fatal("accepted", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(s)) {
			t.Fatal("rejected", s)
		}
	}
	for _, s := range []string{"http://example.com", "https://localhost", "https://127.0.0.1", "https://169.254.169.254", "https://user:pass@example.com", "https://example.com:8080", "https://example.com/#x"} {
		u, e := url.Parse(s)
		if e == nil && validateURL(u) == nil {
			t.Fatal("accepted", s)
		}
	}
	u, _ := url.Parse("https://raw.githubusercontent.com/owner/repo/main/list.txt")
	if e := validateURL(u); e != nil {
		t.Fatal(e)
	}
}

func TestParseMixedRecords(t *testing.T) {
	raw := "\ufeffСбербанк — ru.sberbankmobile\r\ncom.a, 'com.b' com.c\ncom.a\ncom.d\t# com.comment\ncom.e // com.comment\nhttps://example.com/com.fake\n/path/com.fake\nmail@com.fake\ncom.bad.\ncom..bad\n1com.bad\ncom.1bad\n<html>com.fake</html>\n"
	got, err := Parse([]byte(raw))
	want := []string{"com.a", "com.b", "com.c", "com.d", "com.e", "ru.sberbankmobile"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
}

func TestParseAdjacentURL(t *testing.T) {
	got, err := Parse([]byte("com.first,https://example.com/com.fake,com.last\nhttps://example.com,com.other\ncom.x,mail@com.fake,com.y\ncom.z,# com.comment\n"))
	want := []string{"com.first", "com.last", "com.other", "com.x", "com.y", "com.z"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
}
