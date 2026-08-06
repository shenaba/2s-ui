package sub

import (
	"encoding/json"
	"strings"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util"
)

type Link struct {
	Type   string `json:"type"`
	Remark string `json:"remark"`
	Uri    string `json:"uri"`
}

type LinkService struct {
}

func (s *LinkService) GetLinks(linkJson *json.RawMessage, types string, clientInfo string) []string {
	links := []Link{}
	var result []string
	err := json.Unmarshal(*linkJson, &links)
	if err != nil {
		return nil
	}
	for _, link := range links {
		switch link.Type {
		case "external":
			result = append(result, link.Uri)
		case "sub":
			subLinks := util.GetExternalLink(link.Uri)
			result = append(result, strings.Split(subLinks, "\n")...)
		case "local":
			if types == "all" {
				result = append(result, s.addClientInfo(link.Uri, clientInfo))
			}
		}
	}
	return result
}

func (s *LinkService) GetExternalOutbounds(linkJson *json.RawMessage) ([]map[string]interface{}, []string) {
	links := []Link{}
	err := json.Unmarshal(*linkJson, &links)
	if err != nil {
		return nil, nil
	}

	var outbounds []map[string]interface{}
	var tags []string

	for _, link := range links {
		switch link.Type {
		case "external":
			outbound, tag, err := util.GetOutbound(link.Uri, 0)
			if err == nil && outbound != nil && len(tag) > 0 {
				outbounds = append(outbounds, *outbound)
				tags = append(tags, tag)
			}
		case "sub":
			subOutbounds, err := util.GetExternalSub(link.Uri)
			if err != nil {
				logger.Warning("sub: Error getting external sub:", err)
				continue
			}
			for _, outbound := range subOutbounds {
				if tag, _ := outbound["tag"].(string); len(tag) > 0 {
					outbounds = append(outbounds, outbound)
					tags = append(tags, tag)
				}
			}
		}
	}

	// Tag uniqueness is settled by uniqueOutboundTags once every source has been
	// merged: deduping only within the links here left collisions against the
	// inbound-derived tags unnoticed.
	return outbounds, tags
}

func (s *LinkService) addClientInfo(uri string, clientInfo string) string {
	if len(clientInfo) == 0 {
		return uri
	}
	protocol := strings.Split(uri, "://")
	if len(protocol) < 2 {
		return uri
	}
	switch protocol[0] {
	case "vmess":
		var vmessJson map[string]interface{}
		config, err := util.B64StrToByte(protocol[1])
		if err != nil {
			logger.Warning("sub: Error decoding vmess content:", err)
			return uri
		}
		err = json.Unmarshal(config, &vmessJson)
		if err != nil {
			logger.Warning("sub: Error decoding vmess content:", err)
			return uri
		}
		vmessJson["ps"] = vmessJson["ps"].(string) + clientInfo
		result, err := json.MarshalIndent(vmessJson, "", "  ")
		if err != nil {
			logger.Warning("sub: Error decoding vmess + clientInfo content:", err)
			return uri
		}
		return "vmess://" + util.ByteToB64Str(result)
	default:
		return uri + clientInfo
	}
}
