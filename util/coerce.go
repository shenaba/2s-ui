package util

// Readers for values that arrive as interface{} from JSON.
//
// Inbound options, TLS configs and address rows reach the link and subscription
// builders as map[string]interface{} straight out of the database, so a value
// the schema calls a string can be absent, null, or a number -- a row written
// by an older panel, restored from a backup, edited by hand, or pushed by a
// managed node. A bare type assertion on any of those panics inside the
// subscription handler, where one bad row costs every client on the inbound its
// links.
//
// Exported rather than kept private to genLink.go because sub/ reads the same
// maps: the unexported versions were why the Clash converter had to hand-roll
// the same loops again, and why two halves of the same fix drifted apart.

// AsString reads a value the schema says is a string, or "".
func AsString(v interface{}) string {
	s, _ := v.(string)
	return s
}

// AsBool reads a value the schema says is a bool, or false.
func AsBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

// The Go-native shapes matter as much as the JSON ones. GetOutbound assembles
// an outbound in Go rather than unmarshalling one -- getTls fills alpn with a
// strings.Split, hy2 does the same for server_ports, hy runs the bandwidths
// through strconv.Atoi -- and GetExternalOutbounds hands those maps straight to
// the Clash converter with no round trip in between. So a value the schema
// calls a list arrives as []string where a stored row gives []interface{}, and
// a number arrives as int where a stored row gives float64. Every reader here
// takes both; asserting only the JSON shape is what silently dropped the alpn,
// the port-hopping range and the transport host of every external and
// node-replica link.

// AsStringList reads a value the schema says is a list of strings, skipping
// anything that is not one rather than asserting element by element.
func AsStringList(v interface{}) []string {
	switch items := v.(type) {
	case []string:
		return items
	case []interface{}:
		list := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok {
				list = append(list, s)
			}
		}
		return list
	}
	return nil
}

// AsInt64 reads a value the schema says is a number, and reports whether it was
// one -- callers use that to tell an absent field from a zero.
func AsInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}
