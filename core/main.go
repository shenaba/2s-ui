package core

import (
	"context"
	"sync"

	"github.com/shenaba/2s-ui/logger"

	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	_ "github.com/sagernet/sing-box/experimental/clashapi"
	_ "github.com/sagernet/sing-box/experimental/v2rayapi"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	_ "github.com/sagernet/sing-box/transport/v2rayquic"
	"github.com/sagernet/sing/service"
)

var (
	globalCtx        context.Context
	inbound_manager  adapter.InboundManager
	outbound_manager adapter.OutboundManager
	service_manager  adapter.ServiceManager
	endpoint_manager adapter.EndpointManager
	router           adapter.Router
	factory          log.Factory
)

type Core struct {
	// Guards isRunning and instance. Both are written by Start/Stop (the app
	// lifecycle and ConfigService) while ~18 call sites across service/ read
	// them from unrelated goroutines — gin handlers, the @every 5s checkCore
	// and stats cron jobs, and the websocket read pump. cron.Stop does not wait
	// for in-flight jobs, so a shutdown reliably overlaps a checkCore run; the
	// race detector flags it on every restart.
	mu        sync.RWMutex
	isRunning bool
	instance  *Box
}

func NewCore() *Core {
	globalCtx = context.Background()
	globalCtx = sb.Context(globalCtx, InboundRegistry(), OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry())
	return &Core{
		isRunning: false,
		instance:  nil,
	}
}

func (c *Core) GetCtx() context.Context {
	return globalCtx
}

func (c *Core) GetInstance() *Box {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.instance
}

func (c *Core) Start(sbConfig []byte) error {
	var opt option.Options
	err := opt.UnmarshalJSONContext(globalCtx, sbConfig)
	if err != nil {
		logger.Error("Unmarshal config err:", err.Error())
	}

	// Built into a local and published at the end: assigning c.instance first
	// exposed a box that had not started yet, and the write itself raced every
	// GetInstance reader.
	instance, err := NewBox(Options{
		Context: globalCtx,
		Options: opt,
	})
	if err != nil {
		return err
	}

	err = instance.Start()
	if err != nil {
		_ = instance.Close()
		c.mu.Lock()
		c.instance = nil
		c.isRunning = false
		c.mu.Unlock()
		return err
	}

	globalCtx = service.ContextWith(globalCtx, c)
	inbound_manager = service.FromContext[adapter.InboundManager](globalCtx)
	outbound_manager = service.FromContext[adapter.OutboundManager](globalCtx)
	service_manager = service.FromContext[adapter.ServiceManager](globalCtx)
	endpoint_manager = service.FromContext[adapter.EndpointManager](globalCtx)
	router = service.FromContext[adapter.Router](globalCtx)

	c.mu.Lock()
	c.instance = instance
	c.isRunning = true
	c.mu.Unlock()
	return nil
}

func (c *Core) Stop() error {
	// Publish "stopped" first, then close outside the lock: readers see the
	// core as down immediately instead of blocking behind a slow teardown.
	c.mu.Lock()
	instance := c.instance
	c.isRunning = false
	c.instance = nil
	c.mu.Unlock()
	if instance == nil {
		return nil
	}
	return instance.Close()
}

func (c *Core) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isRunning
}
