package core

import (
	"github.com/shenaba/2s-ui/logger"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

// Every method here takes the live box once, through running(), and then works
// from that one reference.
//
// The managers come off the box rather than from package-level vars, so a
// hot-reload cannot apply an add to a manager belonging to a box that has since
// been closed by a restart -- and reading c.isRunning directly, which is what
// these did, was a data race against Start and Stop besides.

func (c *Core) AddInbound(config []byte) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	var inbound_config option.Inbound
	if err = inbound_config.UnmarshalJSONContext(box.ctx, config); err != nil {
		return err
	}

	return box.Inbound().Create(
		box.ctx,
		box.Router(),
		box.LogFactory().NewLogger("inbound/"+inbound_config.Type+"["+inbound_config.Tag+"]"),
		inbound_config.Tag,
		inbound_config.Type,
		inbound_config.Options)
}

func (c *Core) RemoveInbound(tag string) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	logger.Info("remove inbound: ", tag)
	return box.Inbound().Remove(tag)
}

func (c *Core) AddOutbound(config []byte) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	var outbound_config option.Outbound
	if err = outbound_config.UnmarshalJSONContext(box.ctx, config); err != nil {
		return err
	}

	outboundCtx := adapter.WithContext(box.ctx, &adapter.InboundContext{
		Outbound: outbound_config.Tag,
	})

	return box.Outbound().Create(
		outboundCtx,
		box.Router(),
		box.LogFactory().NewLogger("outbound/"+outbound_config.Type+"["+outbound_config.Tag+"]"),
		outbound_config.Tag,
		outbound_config.Type,
		outbound_config.Options)
}

func (c *Core) RemoveOutbound(tag string) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	logger.Info("remove outbound: ", tag)
	return box.Outbound().Remove(tag)
}

func (c *Core) AddEndpoint(config []byte) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	var endpoint_config option.Endpoint
	if err = endpoint_config.UnmarshalJSONContext(box.ctx, config); err != nil {
		return err
	}

	return box.Endpoint().Create(
		box.ctx,
		box.Router(),
		box.LogFactory().NewLogger("endpoint/"+endpoint_config.Type+"["+endpoint_config.Tag+"]"),
		endpoint_config.Tag,
		endpoint_config.Type,
		endpoint_config.Options)
}

func (c *Core) RemoveEndpoint(tag string) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	logger.Info("remove endpoint: ", tag)
	return box.Endpoint().Remove(tag)
}

func (c *Core) AddService(config []byte) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	var srv_config option.Service
	if err = srv_config.UnmarshalJSONContext(box.ctx, config); err != nil {
		return err
	}

	return box.Service().Create(
		box.ctx,
		box.LogFactory().NewLogger("service/"+srv_config.Type+"["+srv_config.Tag+"]"),
		srv_config.Tag,
		srv_config.Type,
		srv_config.Options)
}

func (c *Core) RemoveService(tag string) error {
	box, err := c.running()
	if err != nil {
		return err
	}
	logger.Info("remove service: ", tag)
	return box.Service().Remove(tag)
}
