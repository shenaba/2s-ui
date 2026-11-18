package service

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/sagernet/sing-box/option"
)

// SubClientOptions 是 JSON 订阅模板中由客户端 sing-box 使用的选项。
type SubClientOptions struct {
	Services              []json.RawMessage
	DefaultDomainResolver any
}

func ParseSubClientOptions(template map[string]any) (SubClientOptions, error) {
	var options SubClientOptions
	if value := template["services"]; value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return SubClientOptions{}, err
		}
		if err := json.Unmarshal(raw, &options.Services); err != nil {
			return SubClientOptions{}, fmt.Errorf("订阅 services 必须是数组: %w", err)
		}
		tags := make(map[string]bool, len(options.Services))
		for i, item := range options.Services {
			var settings struct {
				Type string `json:"type"`
				Tag  string `json:"tag"`
				option.APIServiceOptions
			}
			decoder := json.NewDecoder(bytes.NewReader(item))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&settings); err != nil {
				return SubClientOptions{}, fmt.Errorf("无效的订阅 services[%d]: %w", i, err)
			}
			if settings.Type != "api" {
				return SubClientOptions{}, fmt.Errorf("订阅 services[%d] 的类型必须是 api", i)
			}
			if settings.Tag != "" {
				if tags[settings.Tag] {
					return SubClientOptions{}, fmt.Errorf("重复的订阅服务标签 %q", settings.Tag)
				}
				tags[settings.Tag] = true
			}
		}
	}

	switch resolver := template["default_domain_resolver"].(type) {
	case nil:
	case string:
		if resolver != "" {
			options.DefaultDomainResolver = resolver
		}
	case map[string]any:
		raw, err := json.Marshal(resolver)
		if err != nil {
			return SubClientOptions{}, err
		}
		var settings option.DomainResolveOptions
		if err := json.Unmarshal(raw, &settings); err != nil {
			return SubClientOptions{}, fmt.Errorf("无效的订阅 default_domain_resolver: %w", err)
		}
		options.DefaultDomainResolver = resolver
	default:
		return SubClientOptions{}, fmt.Errorf("订阅 default_domain_resolver 必须是字符串或对象")
	}
	return options, nil
}
