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

// AsStringList reads a JSON array that should hold strings, skipping anything
// that is not one rather than asserting element by element.
func AsStringList(v interface{}) []string {
	items, ok := v.([]interface{})
	if !ok {
		return nil
	}
	list := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			list = append(list, s)
		}
	}
	return list
}
