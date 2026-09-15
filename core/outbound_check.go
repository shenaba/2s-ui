package core

import (
	"context"
	"time"

	urltest "github.com/sagernet/sing-box/common/urltest"
)

const checkTimeout = 15 * time.Second

type CheckOutboundResult struct {
	OK    bool
	Delay uint16
	Error string
}

// CheckOutbound is a method rather than a package function taking a context:
// it needs the outbound manager of the box that is running right now, and the
// caller used to supply the context separately from a nil check on a package
// var, which is two reads of a state that changes on every config save.
func (c *Core) CheckOutbound(tag string, link string) (result CheckOutboundResult) {
	box, err := c.running()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	ob, ok := box.Outbound().Outbound(tag)
	if !ok {
		result.Error = "outbound not found"
		return result
	}

	ctx, cancel := context.WithTimeout(box.ctx, checkTimeout)
	defer cancel()

	delay, err := urltest.URLTest(ctx, link, ob)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.Delay = delay
	return result
}
