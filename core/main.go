package core

import (
	"context"
	"sync"

	"github.com/shenaba/2s-ui/util/common"

	sb "github.com/sagernet/sing-box"
	_ "github.com/sagernet/sing-box/experimental/clashapi"
	_ "github.com/sagernet/sing-box/experimental/v2rayapi"
	"github.com/sagernet/sing-box/option"
	_ "github.com/sagernet/sing-box/transport/v2rayquic"
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

	// ctx carries the protocol registries. Built once in NewCore and never
	// reassigned, so it needs no lock -- unlike the managers this type used to
	// keep beside it in package-level vars, which Start wrote unsynchronised
	// while the endpoint methods read them from gin handlers. Those were
	// copies of fields the Box already holds, so a hot-reload could apply an
	// add to the manager of a box a restart had already closed. They are read
	// off the live Box now; see running.
	ctx context.Context
}

// running returns the live box, or an error naming why there is none.
//
// One lock, so the answer and the box come from the same moment. Callers
// that asked IsRunning and then GetInstance were reading two of them, and a
// Stop in between handed them a nil box they went on to dereference.
func (c *Core) running() (*Box, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.isRunning || c.instance == nil {
		return nil, common.NewError("sing-box is not running")
	}
	return c.instance, nil
}

func NewCore() *Core {
	return &Core{
		ctx: sb.Context(context.Background(), InboundRegistry(), OutboundRegistry(),
			EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(),
			CertificateProviderRegistry()),
	}
}

func (c *Core) GetCtx() context.Context {
	return c.ctx
}

func (c *Core) GetInstance() *Box {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.instance
}

// SetStateForTest puts the core into a state Start and Stop only ever pass
// through, so a caller that reads the two fields separately can be tested
// against it.
//
// Named for tests because nothing else may use it: Start and Stop write both
// fields under one lock precisely so this combination is never observable, and
// a caller reaching for it would be writing the bug back in.
func (c *Core) SetStateForTest(isRunning bool, instance *Box) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.isRunning = isRunning
	c.instance = instance
}

// ValidateConfig parses a whole sing-box config the way Start does, without
// building or starting anything, so that a save can refuse a config the next
// start could not even read: a malformed value, an unknown field, a removed
// option. That is all it checks. What only NewBox or Start find -- a tag that
// names nothing, a port another socket holds -- still passes and fails the
// restart. Parsing needs no running core: the protocol registries live on c.ctx,
// not on the box.
func (c *Core) ValidateConfig(sbConfig []byte) error {
	_, err := c.parseConfig(sbConfig)
	return err
}

// parseConfig is the one definition of a config the core can read, shared by
// Start and ValidateConfig so that a save cannot accept what a start refuses.
// Anything else NewBox checks before it builds belongs here too.
func (c *Core) parseConfig(sbConfig []byte) (option.Options, error) {
	var opt option.Options
	if err := opt.UnmarshalJSONContext(c.ctx, sbConfig); err != nil {
		return opt, err
	}
	if opt.Experimental != nil && opt.Experimental.Debug != nil {
		if err := checkDebugOptions(*opt.Experimental.Debug); err != nil {
			return opt, err
		}
	}
	return opt, nil
}

func (c *Core) Start(sbConfig []byte) error {
	// Returned, not just logged. A config sing-box cannot parse left opt at
	// its zero value and carried on: NewBox happily builds a box with no
	// inbounds at all, Start succeeds and IsRunning reports true -- so the
	// five-second watchdog sees a healthy core and never retries, and the
	// panel serves nobody while looking fine. Failing here routes it through
	// the start cooldown and the CoreCrash notification instead.
	opt, err := c.parseConfig(sbConfig)
	if err != nil {
		return common.NewErrorf("unmarshal sing-box config: %v", err)
	}

	// Built into a local and published at the end: assigning c.instance first
	// exposed a box that had not started yet, and the write itself raced every
	// GetInstance reader.
	instance, err := NewBox(Options{
		Context: c.ctx,
		Options: opt,
	})
	if err != nil {
		// Its inbounds may have been built, switching the PROXY dial guard
		// on, and are now gone without a box to say so.
		refreshProxyHeaders()
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
