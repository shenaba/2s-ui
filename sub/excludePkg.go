package sub

import "regexp"

// mergeExcludePkgs adds the remote package list IDs (service/excludePkg.go) to
// every TUN inbound's exclude_package in an assembled JSON subscription.
//
// The operator's own exclude_package goes through untouched: same entries,
// same order, nothing validated or dropped. Only the remote IDs are filtered:
// one that any route or DNS rule names through package_name or matches through
// package_name_regex is left out, whatever the rule does -- a rule written for
// an app means the operator wants that app inside the tunnel, and an excluded
// app never reaches the rules at all. Only rules in the config itself are seen:
// a remote rule-set (type remote, fetched by the client) is opaque here, so an
// app it names can still be excluded. The settings page says so. A TUN with
// include_package is skipped:
// sing-box applies include_package first, so an exclusion there would mean
// nothing and could only confuse the reader.
func mergeExcludePkgs(config map[string]interface{}, ids []string) {
	if len(ids) == 0 {
		return
	}
	inbounds, _ := config["inbounds"].([]interface{})
	if len(inbounds) == 0 {
		return
	}
	exact := map[string]bool{}
	var patterns []*regexp.Regexp
	collectPkgRules(config["route"], exact, &patterns)
	collectPkgRules(config["dns"], exact, &patterns)

	var extra []string
	for _, id := range ids {
		if exact[id] || matchesAny(patterns, id) {
			continue
		}
		extra = append(extra, id)
	}
	if len(extra) == 0 {
		return
	}
	for _, v := range inbounds {
		tun, ok := v.(map[string]interface{})
		if !ok || tun["type"] != "tun" || len(listable(tun["include_package"])) > 0 {
			continue
		}
		cur := listable(tun["exclude_package"])
		have := make(map[string]bool, len(cur))
		out := make([]interface{}, 0, len(cur)+len(extra))
		for _, item := range cur {
			out = append(out, item)
			if s, ok := item.(string); ok {
				have[s] = true
			}
		}
		for _, id := range extra {
			if !have[id] {
				out = append(out, id)
			}
		}
		tun["exclude_package"] = out
	}
}

// listable reads a sing-box listable field: absent, a single value, or a list.
func listable(v interface{}) []interface{} {
	switch x := v.(type) {
	case nil:
		return nil
	case []interface{}:
		return x
	case []string:
		out := make([]interface{}, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	default:
		return []interface{}{x}
	}
}

// collectPkgRules walks a config subtree for package_name and
// package_name_regex, at any depth: logical rules nest, and inline rule-sets
// carry rules of their own. A regexp that does not compile is skipped; sing-box
// refuses that config on its own.
func collectPkgRules(v interface{}, exact map[string]bool, patterns *[]*regexp.Regexp) {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, item := range x {
			switch k {
			case "package_name":
				for _, s := range listable(item) {
					if name, ok := s.(string); ok {
						exact[name] = true
					}
				}
			case "package_name_regex":
				for _, s := range listable(item) {
					if expr, ok := s.(string); ok {
						if re, err := regexp.Compile(expr); err == nil {
							*patterns = append(*patterns, re)
						}
					}
				}
			default:
				collectPkgRules(item, exact, patterns)
			}
		}
	case []interface{}:
		for _, item := range x {
			collectPkgRules(item, exact, patterns)
		}
	}
}

func matchesAny(patterns []*regexp.Regexp, s string) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
