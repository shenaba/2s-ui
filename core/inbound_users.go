package core

import (
	suiAnytls "github.com/shenaba/2s-ui/core/protocol/anytls"
	suiHysteria "github.com/shenaba/2s-ui/core/protocol/hysteria"
	suiHysteria2 "github.com/shenaba/2s-ui/core/protocol/hysteria2"
	suiTrojan "github.com/shenaba/2s-ui/core/protocol/trojan"
	suiTuic "github.com/shenaba/2s-ui/core/protocol/tuic"
	suiVless "github.com/shenaba/2s-ui/core/protocol/vless"
	suiVmess "github.com/shenaba/2s-ui/core/protocol/vmess"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	sbCommon "github.com/sagernet/sing/common"
)

func (c *Core) UpdateInboundUsers(config []byte) (bool, error) {
	box, err := c.running()
	if err != nil {
		return false, err
	}
	var inboundConfig option.Inbound
	if err = inboundConfig.UnmarshalJSONContext(box.ctx, config); err != nil {
		return false, err
	}
	inb, found := box.Inbound().Get(inboundConfig.Tag)
	if !found {
		return false, nil
	}
	switch options := inboundConfig.Options.(type) {
	case *option.VLESSInboundOptions:
		if in, ok := inb.(*suiVless.Inbound); ok {
			return true, in.UpdateUsers(options.Users)
		}
	case *option.VMessInboundOptions:
		if in, ok := inb.(*suiVmess.Inbound); ok {
			return true, in.UpdateUsers(options.Users)
		}
	case *option.TrojanInboundOptions:
		if in, ok := inb.(*suiTrojan.Inbound); ok {
			return true, in.UpdateUsers(options.Users)
		}
	case *option.TUICInboundOptions:
		if in, ok := inb.(*suiTuic.Inbound); ok {
			return true, in.UpdateUsers(options.Users)
		}
	case *option.HysteriaInboundOptions:
		if in, ok := inb.(*suiHysteria.Inbound); ok {
			return true, in.UpdateUsers(options.Users)
		}
	case *option.Hysteria2InboundOptions:
		if in, ok := inb.(*suiHysteria2.Inbound); ok {
			return true, in.UpdateUsers(options.Users)
		}
	case *option.AnyTLSInboundOptions:
		if in, ok := inb.(*suiAnytls.Inbound); ok {
			return true, in.UpdateUsers(options.Users)
		}
	case *option.ShadowsocksInboundOptions:
		if options.Managed || len(options.Users) == 0 {
			return false, nil
		}
		if in, ok := inb.(*shadowsocks.MultiInbound); ok {
			return true, in.UpdateUsers(sbCommon.Map(options.Users, func(it option.ShadowsocksUser) string {
				return it.Name
			}), sbCommon.Map(options.Users, func(it option.ShadowsocksUser) string {
				return it.Password
			}))
		}
	}
	return false, nil
}
