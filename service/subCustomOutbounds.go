package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sagernet/sing/common/json/badoption"
)

// SubOutboundExtensions 是 JSON 订阅模板中已经校验的额外出站和分组。
type SubOutboundExtensions struct {
	custom []map[string]any
	groups []subOutboundGroup
}

func ParseSubOutboundExtensions(template map[string]any) (SubOutboundExtensions, error) {
	var extensions SubOutboundExtensions
	var err error
	var customTags map[string]bool
	if value, ok := template["custom_outbounds"]; ok {
		extensions.custom, customTags, err = parseSubCustomOutbounds(value)
		if err != nil {
			return SubOutboundExtensions{}, err
		}
	}
	if value, ok := template["outbound_groups"]; ok {
		extensions.groups, err = parseSubOutboundGroups(value, customTags)
		if err != nil {
			return SubOutboundExtensions{}, err
		}
	}
	return extensions, nil
}

func parseSubCustomOutbounds(value any) ([]map[string]any, map[string]bool, error) {
	entries, ok := value.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("custom_outbounds 必须是数组")
	}
	outbounds := make([]map[string]any, 0, len(entries))
	tags := make(map[string]bool, len(entries))
	for _, entry := range entries {
		config, ok := entry.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("custom_outbounds 的每一项必须是对象")
		}
		var settings struct {
			Type           string              `json:"type"`
			Tag            string              `json:"tag"`
			ConnectTimeout *badoption.Duration `json:"connect_timeout"`
		}
		if err := decodeSubOutboundOptions(config, &settings); err != nil {
			return nil, nil, fmt.Errorf("无效的自定义订阅出站: %w", err)
		}
		if reservedSubOutboundTag(settings.Tag) || tags[settings.Tag] {
			return nil, nil, fmt.Errorf("无效、保留或重复的自定义订阅出站标签 %q", settings.Tag)
		}
		switch settings.Type {
		case "direct":
			if settings.ConnectTimeout != nil && *settings.ConnectTimeout < 0 {
				return nil, nil, fmt.Errorf("自定义直连出站 %q 的 connect_timeout 不能为负数", settings.Tag)
			}
		case "block":
			if _, exists := config["connect_timeout"]; exists || settings.ConnectTimeout != nil {
				return nil, nil, fmt.Errorf("自定义阻断出站 %q 不支持 connect_timeout", settings.Tag)
			}
		default:
			return nil, nil, fmt.Errorf("自定义订阅出站 %q 的类型必须是 direct 或 block", settings.Tag)
		}
		outbound := map[string]any{"type": settings.Type, "tag": settings.Tag}
		if settings.ConnectTimeout != nil {
			outbound["connect_timeout"] = *settings.ConnectTimeout
		}
		outbounds = append(outbounds, outbound)
		tags[settings.Tag] = true
	}
	return outbounds, tags, nil
}

func reservedSubOutboundTag(tag string) bool {
	switch tag {
	case subscriptionNodes, "proxy", "auto", "direct":
		return true
	default:
		return strings.TrimSpace(tag) == ""
	}
}

func decodeSubOutboundOptions(config map[string]any, options any) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(options)
}
