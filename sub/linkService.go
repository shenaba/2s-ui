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
	expanded := s.GetLinkList(linkJson, types, clientInfo)
	var result []string
	for _, link := range expanded {
		result = append(result, link.Uri)
	}
	return result
}

// GetLinkList is GetLinks with the remarks kept.
//
// The remark is what a link is called on screen -- it names the inbound the
// link belongs to -- and any caller offering one link at a time needs it to
// label the choice. A remote subscription expands into several links that share
// the stored entry's remark, since the remote gives no per-link name here.
func (s *LinkService) GetLinkList(linkJson *json.RawMessage, types string, clientInfo string) []Link {
	links := []Link{}
	var result []Link
	err := json.Unmarshal(*linkJson, &links)
	if err != nil {
		return nil
	}
	for _, link := range links {
		switch link.Type {
		case "external":
			result = append(result, link)
		case "sub":
			subLinks := util.GetExternalLink(link.Uri)
			for _, uri := range strings.Split(subLinks, "\n") {
				result = append(result, Link{Type: link.Type, Remark: link.Remark, Uri: uri})
			}
		case "local":
			if types == "all" {
				result = append(result, Link{
					Type:   link.Type,
					Remark: link.Remark,
					Uri:    s.addClientInfo(link.Uri, clientInfo),
				})
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
