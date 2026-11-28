package core

import (
	"slices"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
)

// Since 1.14.6 sing-box refuses to build a box whose rule actions name an
// outbound or DNS server it does not have: router.ValidateRuleActions for the
// route rules, and dnsRouter.Initialize (validateRuleActionServers) for the DNS
// rules, both from NewBox. 1.14.4 built such a box and failed only the
// connections or queries the rule matched.
//
// That matters more here than to sing-box, for three reasons, and each has its
// answer below:
//   - Upstream runs both at the end of NewBox, after every inbound and
//     endpoint has been built, and a wireguard endpoint builds its gVisor
//     stack in its constructor with no way to free it before Start. A config
//     failing them, retried by the watchdog, would leak one per attempt. So
//     Core.Start runs checkRuleActions on the parsed config before NewBox
//     builds anything, and box.go initializes the DNS router -- which can fail
//     on more than a missing server -- before the endpoints.
//   - The panel removes outbounds from a running core. One a rule names used to
//     go quietly, and now the next restart fails. So a removal or rename of one
//     is refused, like route.final (ruleNamed, refuseRuleNamed), and the service
//     layer refuses it as well while the core is down (RuleNamedOutbounds).
//   - A base config naming one is refused at save (CheckRuleActions).

// ruleTargets maps each outbound and DNS server a top-level route rule's action
// names to the first rule naming it -- exactly what ValidateRuleActions looks
// at: route and bypass name an outbound, resolve a DNS server. Rules nested in
// a logical rule carry no action of their own.
func ruleTargets(rules []option.Rule) (outbounds map[string]int, servers map[string]int) {
	outbounds = make(map[string]int)
	servers = make(map[string]int)
	for i, rule := range rules {
		action := rule.DefaultOptions.RuleAction
		if rule.Type == C.RuleTypeLogical {
			action = rule.LogicalOptions.RuleAction
		}
		var outbound, server string
		switch action.Action {
		case C.RuleActionTypeRoute:
			outbound = action.RouteOptions.Outbound
		case C.RuleActionTypeBypass:
			outbound = action.BypassOptions.Outbound
		case C.RuleActionTypeResolve:
			server = action.ResolveOptions.Server
		}
		if _, seen := outbounds[outbound]; outbound != "" && !seen {
			outbounds[outbound] = i
		}
		if _, seen := servers[server]; server != "" && !seen {
			servers[server] = i
		}
	}
	return outbounds, servers
}

// dnsRuleServer is upstream's dnsRuleActionType/dnsRuleActionServer
// (dns/router.go): the server a top-level DNS rule's action names, and whether
// that action is evaluate, which may not use a fakeip server.
func dnsRuleServer(rule option.DNSRule) (server string, evaluate bool) {
	var action option.DNSRuleAction
	switch rule.Type {
	case "", C.RuleTypeDefault:
		action = rule.DefaultOptions.DNSRuleAction
	case C.RuleTypeLogical:
		action = rule.LogicalOptions.DNSRuleAction
	default:
		return "", false
	}
	if action.Action == C.RuleActionTypeEvaluate {
		return action.EvaluateOptions.Server, true
	}
	return action.RouteOptions.Server, false
}

// RuleNumber is how the panel numbers a rule in its messages: from 1, as the
// Rules and DNS views do (index + 1). sing-box counts from 0, so a message
// passing its "rule[i]" on would send an operator to the rule before.
func RuleNumber(index int) string {
	return F.ToString("#", index+1)
}

// refuseRuleNamed is the removal refusal for a tag a route rule names.
func refuseRuleNamed(kind string, tag string, named map[string]int) error {
	if i, found := named[tag]; found {
		return E.New(kind, "[", tag, "] is named by route rule ", RuleNumber(i), "; change the rule first")
	}
	return nil
}

// componentTags is the tag each of a config's components gets in the box: its
// own, or its index (box.go).
func componentTags[T any](items []T, tagOf func(T) string) []string {
	tags := make([]string, len(items))
	for i, it := range items {
		if tag := tagOf(it); tag != "" {
			tags[i] = tag
		} else {
			tags[i] = F.ToString(i)
		}
	}
	return tags
}

// checkRuleActions is what ValidateRuleActions and validateRuleActionServers
// refuse, judged on the options alone. outbounds and endpoints are the tags the
// box will hold; a rule may name either, since the router looks an outbound up
// through the endpoints too. The fallbacks are mirrored: a box with no
// outbounds and no route.final gets a "direct" one (endpoints do not count: one
// only becomes the default through route.final), and one with no DNS servers a
// "local" one.
func checkRuleActions(opt option.Options, outbounds []string, endpoints []string) error {
	routeOptions := common.PtrValueOrDefault(opt.Route)
	dnsOptions := common.PtrValueOrDefault(opt.DNS)
	knownOutbounds := make(map[string]bool, len(outbounds)+len(endpoints)+1)
	for _, tag := range slices.Concat(outbounds, endpoints) {
		knownOutbounds[tag] = true
	}
	if len(outbounds) == 0 && routeOptions.Final == "" {
		knownOutbounds["direct"] = true
	}
	serverTypes := make(map[string]string, len(dnsOptions.Servers)+1)
	for i, tag := range componentTags(dnsOptions.Servers, func(it option.DNSServerOptions) string { return it.Tag }) {
		serverTypes[tag] = dnsOptions.Servers[i].Type
	}
	if len(dnsOptions.Servers) == 0 {
		serverTypes["local"] = C.DNSTypeLocal
	}

	type problem struct {
		rule    int
		message string
	}
	var problems []problem
	namedOutbounds, namedServers := ruleTargets(routeOptions.Rules)
	for tag, i := range namedOutbounds {
		if !knownOutbounds[tag] {
			problems = append(problems, problem{i, F.ToString("route rule ", RuleNumber(i), ": outbound not found: ", tag)})
		}
	}
	for tag, i := range namedServers {
		if _, known := serverTypes[tag]; !known {
			problems = append(problems, problem{i, F.ToString("route rule ", RuleNumber(i), ": DNS server not found: ", tag)})
		}
	}
	// In rule order, so the message reads the way the config does; the DNS
	// rules after the route rules.
	slices.SortFunc(problems, func(a, b problem) int { return a.rule - b.rule })
	for i, rule := range dnsOptions.Rules {
		server, evaluate := dnsRuleServer(rule)
		if server == "" {
			continue
		}
		serverType, known := serverTypes[server]
		if !known {
			problems = append(problems, problem{i, F.ToString("dns rule ", RuleNumber(i), ": server not found: ", server)})
		} else if evaluate && serverType == C.DNSTypeFakeIP {
			problems = append(problems, problem{i, F.ToString("dns rule ", RuleNumber(i), ": evaluate action cannot use fakeip server: ", server)})
		}
	}
	if len(problems) == 0 {
		return nil
	}
	messages := make([]string, len(problems))
	for i, it := range problems {
		messages[i] = it.message
	}
	return E.New(strings.Join(messages, "; "))
}

// checkBuiltRuleActions runs checkRuleActions on a whole config, before NewBox
// builds anything from it.
func checkBuiltRuleActions(opt option.Options) error {
	return checkRuleActions(opt,
		componentTags(opt.Outbounds, func(it option.Outbound) string { return it.Tag }),
		componentTags(opt.Endpoints, func(it option.Endpoint) string { return it.Tag }))
}

// CheckRuleActions refuses a base config whose rules name an outbound or DNS
// server the core would not have, which NewBox would refuse on the restart the
// save leads to. outbounds and endpoints are the tags the tables add to the
// config -- the base carries none of its own -- and the DNS servers are the
// base's. Parsing goes through parseConfig, so only a config ValidateConfig
// would accept gets this far.
func (c *Core) CheckRuleActions(sbConfig []byte, outbounds []string, endpoints []string) error {
	opt, err := c.parseConfig(sbConfig)
	if err != nil {
		return err
	}
	return checkRuleActions(opt, outbounds, endpoints)
}

// RuleNamedOutbounds maps each outbound (or endpoint) tag a route rule of this
// base config names to the first rule naming it. The service layer refuses to
// delete or rename one while the core is down, which the core cannot do for it.
func (c *Core) RuleNamedOutbounds(sbConfig []byte) (map[string]int, error) {
	opt, err := c.parseConfig(sbConfig)
	if err != nil {
		return nil, err
	}
	named, _ := ruleTargets(common.PtrValueOrDefault(opt.Route).Rules)
	return named, nil
}

// CheckConfigRuleActions runs the check Start runs first on a whole config,
// so the panel can say at startup why the core will not come up.
func (c *Core) CheckConfigRuleActions(sbConfig []byte) error {
	opt, err := c.parseConfig(sbConfig)
	if err != nil {
		return err
	}
	return checkBuiltRuleActions(opt)
}
