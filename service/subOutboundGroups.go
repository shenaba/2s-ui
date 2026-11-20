package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sagernet/sing-box/option"
)

const subscriptionNodes = "$nodes"

type subOutboundGroup struct {
	tag        string
	members    []string
	defaultTag string
	config     map[string]any
}

// SubOutboundGroups 是 JSON 订阅模板里已完成静态校验的自定义分组。
type SubOutboundGroups []subOutboundGroup

func ParseSubOutboundGroups(value any) (SubOutboundGroups, error) {
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("outbound_groups 必须是数组")
	}
	groups := make(SubOutboundGroups, 0, len(entries))
	dependencies := make(map[string][]string, len(entries))
	for _, entry := range entries {
		config, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("outbound_groups 的每一项必须是对象")
		}
		tag, _ := config["tag"].(string)
		switch tag {
		case "", subscriptionNodes, "proxy", "auto", "direct":
			return nil, fmt.Errorf("无效或保留的订阅分组标签 %q", tag)
		}
		if strings.TrimSpace(tag) == "" {
			return nil, fmt.Errorf("订阅分组标签不能为空白")
		}
		if _, exists := dependencies[tag]; exists {
			return nil, fmt.Errorf("重复的订阅分组标签 %q", tag)
		}
		raw, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		group := subOutboundGroup{tag: tag, config: config}
		switch config["type"] {
		case "selector":
			var settings struct {
				Type string `json:"type"`
				Tag  string `json:"tag"`
				option.SelectorOutboundOptions
			}
			err = decoder.Decode(&settings)
			group.members = settings.Outbounds
			group.defaultTag = settings.Default
		case "urltest":
			var settings struct {
				Type string `json:"type"`
				Tag  string `json:"tag"`
				option.URLTestOutboundOptions
			}
			err = decoder.Decode(&settings)
			group.members = settings.Outbounds
		default:
			return nil, fmt.Errorf("订阅分组 %q 的类型必须是 selector 或 urltest", tag)
		}
		if err != nil {
			return nil, fmt.Errorf("无效的订阅分组 %q: %w", tag, err)
		}
		if len(group.members) == 0 || slices.Contains(group.members, "") {
			return nil, fmt.Errorf("订阅分组 %q 必须指定有效的组成员", tag)
		}
		if group.defaultTag != "" && !slices.Contains(group.members, group.defaultTag) && !slices.Contains(group.members, subscriptionNodes) {
			return nil, fmt.Errorf("订阅分组 %q 的默认出站 %q 不在组内", tag, group.defaultTag)
		}
		dependencies[tag] = group.members
		groups = append(groups, group)
	}

	visiting := make(map[string]bool)
	var visit func(string) error
	visit = func(tag string) error {
		if visiting[tag] {
			return fmt.Errorf("订阅分组 %q 存在循环引用", tag)
		}
		members, exists := dependencies[tag]
		if !exists {
			return nil
		}
		visiting[tag] = true
		for _, member := range members {
			if err := visit(member); err != nil {
				return err
			}
		}
		delete(visiting, tag)
		delete(dependencies, tag)
		return nil
	}
	for _, group := range groups {
		if err := visit(group.tag); err != nil {
			return nil, err
		}
	}
	return groups, nil
}

// AppendTo 用当前订阅的可选节点标签展开分组，并校验最终出站引用。
func (groups SubOutboundGroups) AppendTo(outbounds []map[string]any, nodeTags []string) ([]map[string]any, error) {
	known := make(map[string]bool, len(outbounds)+len(groups))
	for _, outbound := range outbounds {
		tag, _ := outbound["tag"].(string)
		known[tag] = true
	}
	for _, group := range groups {
		if known[group.tag] {
			return nil, fmt.Errorf("订阅分组标签 %q 与现有出站冲突", group.tag)
		}
		known[group.tag] = true
	}
	for _, group := range groups {
		var members []string
		for _, member := range group.members {
			if member == subscriptionNodes {
				members = append(members, nodeTags...)
			} else if known[member] {
				members = append(members, member)
			} else {
				return nil, fmt.Errorf("订阅分组 %q 引用了不存在的出站 %q", group.tag, member)
			}
		}
		config := maps.Clone(group.config)
		if len(members) == 0 {
			// 保留组标签，避免依赖它的路由失效。没有节点可供测速时降级为直连选择组。
			config = map[string]any{"type": "selector", "tag": group.tag}
			members = []string{"direct"}
		}
		seen := make(map[string]bool, len(members))
		unique := members[:0]
		for _, member := range members {
			if !seen[member] {
				seen[member] = true
				unique = append(unique, member)
			}
		}
		config["outbounds"] = unique
		defaultTag := group.defaultTag
		if defaultTag == subscriptionNodes {
			defaultTag = unique[0]
			if len(nodeTags) > 0 {
				defaultTag = nodeTags[0]
			}
		}
		if defaultTag != "" {
			if !seen[defaultTag] {
				return nil, fmt.Errorf("订阅分组 %q 的默认出站 %q 不在组内", group.tag, defaultTag)
			}
			config["default"] = defaultTag
		}
		outbounds = append(outbounds, config)
	}
	return outbounds, nil
}
