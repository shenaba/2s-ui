package util

// IsPlainDirectOutbound reports whether a decoded outbound carries nothing but
// its identity -- a `direct` outbound with no options of its own.
//
// sing-box refuses a detour to one of those: it would dial exactly what a
// dialer with no detour dials, and the flag that suppresses the check
// (DisableEmptyDirectCheck) has no JSON field, so the panel cannot set it. The
// two spellings mean the same thing, so wherever a detour is written out, such
// an outbound has to be written as "no detour" instead.
//
// The argument is an outbound in sing-box shape -- type and tag, then the
// options spread beside them -- which is what both callers hold. A row still in
// the database is answered without rendering it, by asking whether its options
// blob is empty; see database.plainDirectOutboundTags.
func IsPlainDirectOutbound(outbound map[string]interface{}) bool {
	if outboundType, _ := outbound["type"].(string); outboundType != "direct" {
		return false
	}
	for key := range outbound {
		switch key {
		case "type", "tag":
		default:
			return false
		}
	}
	return true
}
