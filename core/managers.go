package core

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/service"
)

// The four managers below stand in for sing-box's adapter/inbound, outbound,
// endpoint and service managers, which 1.14.3 rewrote around adapter.Scope and
// left with no way to change a running box: no Remove, and a Create that
// neither starts what it creates nor replaces a tag that is already taken. The
// panel changes all four in a running core -- every save in service/ goes
// through core.AddInbound / AddOutbound / ... -- so it keeps its own.
//
// Each one follows its 1.14.3 counterpart for everything the box does at start
// (the stages, the outbound dependency order, the default outbound, refusing a
// duplicate tag while the box is being built), and once the box is running
// adds back what 1.14.2 had:
//
//   - Remove(tag), returning os.ErrInvalid for an unknown tag, which is what
//     the callers in service/ test for;
//   - a Create that brings the new component through every stage the box has
//     already passed, and replaces a component that already holds the tag.
//
// A replacement takes the old component's place in the list rather than going
// to the end. Order is what makes the first outbound the default when there is
// no route.final, and an edit -- the same tag, new options -- has to leave that
// as a restart would. (An outbound rename starts the new one and then removes
// the old, so a renamed implicit default does hand the role on until the next
// restart.)
//
// Replacing closes the old component before starting the new one: a listener
// cannot bind a port the old one still holds. So a replacement that fails to
// start would leave nothing; each tag's creation arguments are kept, and the
// old component is rebuilt from them when that happens. Until the new one is
// in, the old one stays in the tables: a lookup -- or the default, which the
// router reads on every connection -- finds a closed component for that moment
// rather than none.
//
// An outbound or endpoint that others depend on -- a selector's or urltest's
// member, a detour -- can be neither removed nor replaced. They resolved it once,
// at Start, and would go on dialing through the closed instance.
//
// Create and Remove are serialized per manager (mutate) and hold that lock
// while the component starts, so two changes to one manager run in turn.
// Lookups take a separate lock (access) that is never held across a Start,
// because a component's Start may look others up through its own manager: a
// selector resolves its members through Outbound().
//
// UPGRADING sing-box: these are not copies, so the line diff in
// scripts/check-protocol-copies.sh cannot cover them; it pins a checksum of the
// upstream managers instead and fails when one changes.

// recipe is what a component was created from, kept so that it can be rebuilt
// when a replacement for it fails to start.
type recipe struct {
	ctx           context.Context
	router        adapter.Router
	logger        log.ContextLogger
	tag           string
	componentType string
	options       any
}

// managerLifecycle is the part every manager shares: the scopes of the
// components it started, and how far the box has got, so that a component
// created later can be brought to the same stage.
type managerLifecycle struct {
	logger log.ContextLogger
	access sync.Mutex
	ctx    context.Context
	scopes *scopeSet
	stage  adapter.StartStage
}

// enter records that the box reached stage. On the first stage it takes the
// manager's own scope as the parent of every component's and has that scope
// close them all, so the box still tears everything down as one.
func (l *managerLifecycle) enter(stage adapter.StartStage, scope *adapter.Scope) *scopeSet {
	l.access.Lock()
	defer l.access.Unlock()
	if l.scopes == nil {
		l.ctx = scope.Context()
		l.scopes = newScopeSet(l.ctx, l.logger)
		scope.Add(l.scopes.Close)
	}
	l.stage = stage
	return l.scopes
}

// current returns the scopes and the stage the box reached, or nil scopes when
// the box has not started yet -- a component created then is started with the
// rest of the box.
func (l *managerLifecycle) current() (*scopeSet, adapter.StartStage) {
	l.access.Lock()
	defer l.access.Unlock()
	return l.scopes, l.stage
}

func (l *managerLifecycle) started() bool {
	scopes, _ := l.current()
	return scopes != nil
}

// catchUp runs component through every stage the box has already passed. On
// failure whatever it did start is closed again, so a refused hot add leaves
// nothing listening.
func (l *managerLifecycle) catchUp(name string, component adapter.Lifecycle) error {
	scopes, reached := l.current()
	if scopes == nil {
		return nil
	}
	for _, stage := range adapter.ListStartStages {
		if stage > reached {
			break
		}
		err := scopes.Start(name, component, stage)
		if err != nil {
			_ = scopes.Remove(component)
			return err
		}
	}
	return nil
}

// close closes component. A close error is logged rather than returned: the
// component is going either way, and failing the save over it would roll the
// database back to a row whose component is already closed.
func (l *managerLifecycle) close(name string, component adapter.Lifecycle) {
	scopes, _ := l.current()
	if scopes == nil {
		return
	}
	if err := scopes.Remove(component); err != nil {
		l.logger.Warn("close ", name, ": ", err)
	}
}

// referencesChanged makes the reference manager recompute which outbounds are
// referenced. It does that on its own only for url-test and clash-mode events,
// from options read once when the box was built, so without this a hot-removed
// outbound stayed pinned in its keep-idle table for the life of the box and a
// hot-added one -- or a Reality inbound's handshake detour -- was never looked
// at. NotifyUpdated is the url-test history's own "something changed" signal,
// which is exactly what it subscribes to.
func (l *managerLifecycle) referencesChanged() {
	l.access.Lock()
	ctx := l.ctx
	l.access.Unlock()
	if ctx == nil {
		return
	}
	if history := service.PtrFromContext[urltest.HistoryStorage](ctx); history != nil {
		history.NotifyUpdated()
	}
}

// component is what every managed kind has in common. An outbound need not
// have a lifecycle at all, so that is asserted per component instead.
type component interface {
	comparable
	Type() string
	Tag() string
}

// componentTable is a manager minus what each kind has of its own: a list in
// start order, an index by tag, the recipe of each, and the scopes of the ones
// started. The hooks are how the outbound and endpoint managers keep their own
// state in step; each may be nil.
type componentTable[T component] struct {
	kind      string
	build     func(recipe) (T, error)
	lifecycle managerLifecycle
	mutate    sync.Mutex
	// access is read on every connection the router handles (Outbound,
	// Default, Get), so readers share it.
	access  sync.RWMutex
	list    []T
	byTag   map[string]T
	recipes map[string]recipe

	// refuse vetoes replacing (removing false) or removing tag. Called
	// holding mutate only.
	refuse func(tag string, removing bool) error
	// standIn may name a component, already started, to take the place of one
	// being removed. Called holding mutate only.
	standIn func(it T) (T, bool, error)
	// added, replaced and dropped see the table change. Called holding
	// access, with the table already updated.
	added    func(it T)
	replaced func(old, new T)
	dropped  func(it T)
}

func newComponentTable[T component](kind string, logger log.ContextLogger, build func(recipe) (T, error)) componentTable[T] {
	return componentTable[T]{
		kind:      kind,
		build:     build,
		lifecycle: managerLifecycle{logger: logger},
		byTag:     make(map[string]T),
		recipes:   make(map[string]recipe),
	}
}

func (t *componentTable[T]) name(it T) string {
	return t.kind + "/" + it.Type() + "[" + it.Tag() + "]"
}

func (t *componentTable[T]) start(stage adapter.StartStage, scope *adapter.Scope) error {
	scopes := t.lifecycle.enter(stage, scope)
	for _, it := range t.all() {
		lifecycle, isLifecycle := any(it).(adapter.Lifecycle)
		if !isLifecycle {
			continue
		}
		err := scopes.Start(t.name(it), lifecycle, stage)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *componentTable[T]) all() []T {
	t.access.RLock()
	defer t.access.RUnlock()
	return t.list
}

// settled is all, once no create, rename or remove is under way: one being
// created can already be started -- accepting, for an inbound -- before it is
// listed.
func (t *componentTable[T]) settled() []T {
	t.mutate.Lock()
	defer t.mutate.Unlock()
	return t.all()
}

func (t *componentTable[T]) get(tag string) (T, bool) {
	t.access.RLock()
	defer t.access.RUnlock()
	it, found := t.byTag[tag]
	return it, found
}

// options is what tag was created with, for asking about its configuration as
// it runs -- the database row may already say something else. Nil for a tag
// the table does not hold.
func (t *componentTable[T]) options(tag string) any {
	t.access.RLock()
	defer t.access.RUnlock()
	return t.recipes[tag].options
}

func (t *componentTable[T]) create(r recipe) error {
	t.mutate.Lock()
	defer t.mutate.Unlock()
	t.access.RLock()
	existing, loaded := t.byTag[r.tag]
	previous, hasPrevious := t.recipes[r.tag]
	t.access.RUnlock()
	if loaded {
		if !t.lifecycle.started() {
			return E.New("duplicate ", t.kind, " tag: ", r.tag)
		}
		if t.refuse != nil {
			if err := t.refuse(r.tag, false); err != nil {
				return err
			}
		}
	}
	// Built only once nothing above refuses it. A component can hold
	// resources from its constructor on that only its Start registers for
	// cleanup -- a wireguard endpoint builds its gVisor stack there -- so one
	// built and then refused would leak them.
	it, err := t.build(r)
	if err != nil {
		return err
	}
	if loaded {
		t.close(existing)
	}
	err = t.catchUp(it)
	if err != nil {
		if loaded {
			err = E.Errors(err, t.restore(existing, previous, hasPrevious))
			t.lifecycle.referencesChanged()
		}
		return err
	}
	if loaded {
		t.swap(existing, it, &r)
	} else {
		t.add(it, &r)
	}
	t.lifecycle.referencesChanged()
	return nil
}

// restore rebuilds the component a failed replacement had already closed, in
// its place. Failing that, it is dropped: it is closed and not coming back.
func (t *componentTable[T]) restore(old T, r recipe, hasRecipe bool) error {
	if !hasRecipe {
		t.drop(old)
		return E.New("no recipe to restore previous ", t.kind, "[", old.Tag(), "] from")
	}
	restored, err := t.build(r)
	if err == nil {
		err = t.catchUp(restored)
	}
	if err != nil {
		t.drop(old)
		return E.Cause(err, "restore previous ", t.kind, "[", r.tag, "]")
	}
	t.swap(old, restored, &r)
	return nil
}

func (t *componentTable[T]) remove(tag string) error {
	t.mutate.Lock()
	defer t.mutate.Unlock()
	it, found := t.get(tag)
	if !found {
		return os.ErrInvalid
	}
	if t.refuse != nil {
		if err := t.refuse(tag, true); err != nil {
			return err
		}
	}
	replaced := false
	if t.standIn != nil {
		in, ok, err := t.standIn(it)
		if err != nil {
			return err
		}
		if ok {
			t.swap(it, in, nil)
			replaced = true
		}
	}
	if !replaced {
		t.drop(it)
	}
	t.close(it)
	t.lifecycle.referencesChanged()
	return nil
}

// rename replaces the component holding oldTag with one built from r, which
// names another tag, in the old one's place in the list. The old one is closed
// first -- the new one may want the port it holds -- and, if the new one does
// not start, rebuilt from its recipe: a failed rename leaves the core as it
// was, as a failed replacement does. Remove followed by Create, which is what
// the callers did, left nothing running on that failure while the save rolled
// the row back to the old tag.
//
// Outbounds do not come through here: they hold no port, so OutboundService
// starts the new one before it removes the old.
func (t *componentTable[T]) rename(oldTag string, r recipe) error {
	if oldTag == r.tag {
		return t.create(r)
	}
	t.mutate.Lock()
	old, found := t.get(oldTag)
	if !found {
		// Nothing to rename: an edit of one the core lost puts it back.
		t.mutate.Unlock()
		return t.create(r)
	}
	defer t.mutate.Unlock()
	if _, taken := t.get(r.tag); taken {
		return E.New("duplicate ", t.kind, " tag: ", r.tag)
	}
	if t.refuse != nil {
		if err := t.refuse(oldTag, true); err != nil {
			return err
		}
	}
	t.access.RLock()
	previous, hasPrevious := t.recipes[oldTag]
	t.access.RUnlock()
	// Built before the old one is closed, and only once nothing above
	// refused: see create.
	it, err := t.build(r)
	if err != nil {
		return err
	}
	t.close(old)
	err = t.catchUp(it)
	if err != nil {
		err = E.Errors(err, t.restore(old, previous, hasPrevious))
		t.lifecycle.referencesChanged()
		return err
	}
	t.swap(old, it, &r)
	t.lifecycle.referencesChanged()
	return nil
}

func (t *componentTable[T]) catchUp(it T) error {
	if lifecycle, isLifecycle := any(it).(adapter.Lifecycle); isLifecycle {
		return t.lifecycle.catchUp(t.name(it), lifecycle)
	}
	return nil
}

func (t *componentTable[T]) close(it T) {
	if lifecycle, isLifecycle := any(it).(adapter.Lifecycle); isLifecycle {
		t.lifecycle.close(t.name(it), lifecycle)
	}
}

// add, swap and drop change the table. The list is replaced rather than edited
// in place: all() hands it out and the reference manager walks it from its own
// goroutine, so a reader must never see an element shift or vanish under it.

// add appends it. r is nil for a component with nothing to rebuild it from.
func (t *componentTable[T]) add(it T, r *recipe) {
	t.access.Lock()
	defer t.access.Unlock()
	t.list = append(slices.Clip(t.list), it)
	t.byTag[it.Tag()] = it
	if r != nil {
		t.recipes[r.tag] = *r
	}
	if t.added != nil {
		t.added(it)
	}
}

// swap puts new where old was. new usually has old's tag; a stand-in need not.
// r is nil for a component with nothing to rebuild it from.
func (t *componentTable[T]) swap(old, new T, r *recipe) {
	t.access.Lock()
	defer t.access.Unlock()
	list := slices.Clone(t.list)
	if index := slices.Index(list, old); index >= 0 {
		list[index] = new
	} else {
		list = append(list, new)
	}
	t.list = list
	if oldTag := old.Tag(); t.byTag[oldTag] == old {
		delete(t.byTag, oldTag)
		delete(t.recipes, oldTag)
	}
	t.byTag[new.Tag()] = new
	if r != nil {
		t.recipes[r.tag] = *r
	}
	if t.replaced != nil {
		t.replaced(old, new)
	}
}

func (t *componentTable[T]) drop(it T) {
	t.access.Lock()
	defer t.access.Unlock()
	tag := it.Tag()
	if t.byTag[tag] == it {
		delete(t.byTag, tag)
		delete(t.recipes, tag)
	}
	t.list = slices.DeleteFunc(slices.Clone(t.list), func(other T) bool { return other == it })
	if t.dropped != nil {
		t.dropped(it)
	}
}

var _ adapter.InboundManager = (*inboundManager)(nil)

type inboundManager struct {
	componentTable[adapter.Inbound]
	endpoint adapter.EndpointManager
}

func newInboundManager(logger log.ContextLogger, registry adapter.InboundRegistry, endpoint adapter.EndpointManager) *inboundManager {
	return &inboundManager{
		componentTable: newComponentTable("inbound", logger, func(r recipe) (adapter.Inbound, error) {
			return registry.Create(r.ctx, r.router, r.logger, r.tag, r.componentType, r.options)
		}),
		endpoint: endpoint,
	}
}

func (m *inboundManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return m.start(stage, scope)
}

func (m *inboundManager) Inbounds() []adapter.Inbound {
	return m.all()
}

func (m *inboundManager) Get(tag string) (adapter.Inbound, bool) {
	if inbound, found := m.get(tag); found {
		return inbound, true
	}
	return m.endpoint.Get(tag)
}

func (m *inboundManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, inboundType string, options any) error {
	return m.create(recipe{ctx: ctx, router: router, logger: logger, tag: tag, componentType: inboundType, options: options})
}

func (m *inboundManager) Remove(tag string) error {
	return m.remove(tag)
}

var _ adapter.EndpointManager = (*endpointManager)(nil)

type endpointManager struct {
	componentTable[adapter.Endpoint]
	// dependents names the outbounds and endpoints that depend on a tag. It
	// comes from the outbound manager, which is built after this one: see
	// refuseReferenced.
	dependents func(tag string) []string
}

func newEndpointManager(logger log.ContextLogger, registry adapter.EndpointRegistry) *endpointManager {
	return &endpointManager{
		componentTable: newComponentTable("endpoint", logger, func(r recipe) (adapter.Endpoint, error) {
			return registry.Create(r.ctx, r.router, r.logger, r.tag, r.componentType, r.options)
		}),
	}
}

// refuseReferenced makes the endpoint manager keep any endpoint the outbounds
// still depend on -- a selector naming a wireguard endpoint resolved it at
// Start just as it does an outbound -- and refuse to remove the one route.final
// names, which a restart would then fail on.
func (m *endpointManager) refuseReferenced(dependents func(tag string) []string, final string) {
	m.dependents = dependents
	m.refuse = func(tag string, removing bool) error {
		if dependBy := dependents(tag); len(dependBy) > 0 {
			return E.New("endpoint[", tag, "] is depended by ", strings.Join(dependBy, ", "))
		}
		if removing && tag == final {
			return E.New("endpoint[", tag, "] is route.final; change route.final first")
		}
		return nil
	}
}

// Dependents names the outbounds and endpoints that depend on tag: what keeps
// an endpoint from being replaced in place.
func (m *endpointManager) Dependents(tag string) []string {
	if m.dependents == nil {
		return nil
	}
	return m.dependents(tag)
}

func (m *endpointManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateStart {
		// Started by the outbound manager, in dependency order, through
		// StartEndpoint. The stage is still recorded, so that a later Create
		// catches up through it.
		m.lifecycle.enter(stage, scope)
		return nil
	}
	return m.start(stage, scope)
}

func (m *endpointManager) StartEndpoint(endpoint adapter.Endpoint) error {
	scopes, _ := m.lifecycle.current()
	if scopes == nil {
		return E.New("start ", m.name(endpoint), ": endpoint manager not initialized")
	}
	return scopes.Start(m.name(endpoint), endpoint, adapter.StartStateStart)
}

func (m *endpointManager) Endpoints() []adapter.Endpoint {
	return m.all()
}

func (m *endpointManager) Get(tag string) (adapter.Endpoint, bool) {
	return m.get(tag)
}

func (m *endpointManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, endpointType string, options any) error {
	return m.create(recipe{ctx: ctx, router: router, logger: logger, tag: tag, componentType: endpointType, options: options})
}

func (m *endpointManager) Remove(tag string) error {
	return m.remove(tag)
}

var _ adapter.ServiceManager = (*serviceManager)(nil)

type serviceManager struct {
	componentTable[adapter.Service]
}

func newServiceManager(logger log.ContextLogger, registry adapter.ServiceRegistry) *serviceManager {
	return &serviceManager{
		componentTable: newComponentTable("service", logger, func(r recipe) (adapter.Service, error) {
			return registry.Create(r.ctx, r.logger, r.tag, r.componentType, r.options)
		}),
	}
}

func (m *serviceManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	return m.start(stage, scope)
}

func (m *serviceManager) Services() []adapter.Service {
	return m.all()
}

func (m *serviceManager) Get(tag string) (adapter.Service, bool) {
	return m.get(tag)
}

func (m *serviceManager) Create(ctx context.Context, logger log.ContextLogger, tag string, serviceType string, options any) error {
	return m.create(recipe{ctx: ctx, logger: logger, tag: tag, componentType: serviceType, options: options})
}

func (m *serviceManager) Remove(tag string) error {
	return m.remove(tag)
}

var _ adapter.OutboundManager = (*outboundManager)(nil)

// outboundManager adds to the table what only outbounds have: a default, and
// 1.14.3's dependency-ordered start. defaultOutbound is guarded by the table's
// access lock and kept in step by its hooks; with route.final set, Default
// looks the tag up instead.
type outboundManager struct {
	componentTable[adapter.Outbound]
	logger                  log.ContextLogger
	endpoint                adapter.EndpointManager
	defaultTag              string
	defaultOutbound         adapter.Outbound
	defaultOutboundFallback func() (adapter.Outbound, error)
}

func newOutboundManager(logger log.ContextLogger, registry adapter.OutboundRegistry, endpoint adapter.EndpointManager, defaultTag string) *outboundManager {
	m := &outboundManager{
		componentTable: newComponentTable("outbound", logger, func(r recipe) (adapter.Outbound, error) {
			return registry.CreateOutbound(r.ctx, r.router, r.logger, r.tag, r.componentType, r.options)
		}),
		logger:     logger,
		endpoint:   endpoint,
		defaultTag: defaultTag,
	}
	m.refuse = func(tag string, removing bool) error {
		if dependBy := m.dependents(tag); len(dependBy) > 0 {
			return E.New("outbound[", tag, "] is depended by ", strings.Join(dependBy, ", "))
		}
		// A replacement is fine: Default looks route.final up by tag. A removal
		// would leave the core on some other outbound now and failing to start
		// on the next restart.
		if removing && tag == m.defaultTag {
			return E.New("outbound[", tag, "] is route.final; change route.final first")
		}
		return nil
	}
	m.standIn = m.fallbackForLast
	m.added = func(it adapter.Outbound) {
		if it.Tag() == m.defaultTag || (m.defaultTag == "" && m.defaultOutbound == nil) {
			m.setDefault(it)
		}
	}
	m.replaced = func(old, new adapter.Outbound) {
		if m.defaultOutbound == old {
			m.setDefault(new)
		}
	}
	m.dropped = func(it adapter.Outbound) {
		if m.defaultOutbound != it {
			return
		}
		// As 1.14.2 did: the first one left.
		if len(m.list) > 0 {
			m.setDefault(m.list[0])
		} else {
			m.defaultOutbound = nil
		}
	}
	return m
}

// fallbackForLast keeps the box with a default outbound. With no route.final
// the default is the first outbound, and the router calls it on every
// unmatched connection without a nil check (route/route.go), so removing the
// last one would panic that connection's goroutine and take the panel down.
// A box built with no outbounds gets a direct one for this (Start, below); one
// that loses its last outbound gets the same, started before the old one goes.
func (m *outboundManager) fallbackForLast(it adapter.Outbound) (adapter.Outbound, bool, error) {
	if m.defaultTag != "" || m.defaultOutboundFallback == nil || !m.lifecycle.started() {
		return nil, false, nil
	}
	if list := m.all(); len(list) != 1 || list[0] != it {
		return nil, false, nil
	}
	fallback, err := m.defaultOutboundFallback()
	if err != nil {
		return nil, false, E.Cause(err, "create direct outbound for fallback")
	}
	err = m.catchUp(fallback)
	if err != nil {
		return nil, false, err
	}
	return fallback, true, nil
}

// dependents names the outbounds and endpoints that depend on tag. It scans
// rather than keeping an index, as upstream s-ui does: an index has to be kept
// in step on every add, replace and drop, and the one this used to keep did
// not count endpoints at all. A panel has tens of outbounds, not thousands.
func (m *outboundManager) dependents(tag string) []string {
	var dependBy []string
	for _, it := range m.all() {
		if common.Contains(it.Dependencies(), tag) {
			dependBy = append(dependBy, it.Tag())
		}
	}
	for _, it := range m.endpoint.Endpoints() {
		if common.Contains(it.Dependencies(), tag) {
			dependBy = append(dependBy, it.Tag())
		}
	}
	return dependBy
}

// checkDependencies is the check 1.14.3 runs at start (startOutbounds), for one
// outbound or endpoint that is not in the box yet: each of dependencies must
// exist, and none may lead back to tag.
func (m *outboundManager) checkDependencies(tag string, dependencies []string) error {
	seen := make(map[string]bool)
	var walk func(path []string, dependencies []string) error
	walk = func(path []string, dependencies []string) error {
		for _, dependency := range dependencies {
			if dependency == tag {
				return E.New("circular outbound dependency: ", strings.Join(path, " -> "), " -> ", dependency)
			}
			if seen[dependency] {
				continue
			}
			seen[dependency] = true
			outbound, found := m.Outbound(dependency)
			if !found {
				return E.New("dependency[", dependency, "] not found for outbound[", path[len(path)-1], "]")
			}
			if err := walk(append(slices.Clip(path), dependency), outbound.Dependencies()); err != nil {
				return err
			}
		}
		return nil
	}
	return walk([]string{tag}, dependencies)
}

func (m *outboundManager) setDefault(it adapter.Outbound) {
	m.defaultOutbound = it
	if m.lifecycle.started() {
		m.logger.Info("updated default outbound to ", it.Tag())
	}
}

func (m *outboundManager) Initialize(defaultOutboundFallback func() (adapter.Outbound, error)) {
	m.defaultOutboundFallback = defaultOutboundFallback
}

func (m *outboundManager) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	scopes := m.lifecycle.enter(stage, scope)
	if stage == adapter.StartStateInitialize {
		m.access.Lock()
		if m.defaultTag != "" && m.defaultOutbound == nil {
			defaultEndpoint, loaded := m.endpoint.Get(m.defaultTag)
			if !loaded {
				m.access.Unlock()
				return E.New("default outbound not found: ", m.defaultTag)
			}
			m.defaultOutbound = defaultEndpoint
		}
		hasDefault := m.defaultOutbound != nil
		m.access.Unlock()
		if !hasDefault {
			directOutbound, err := m.defaultOutboundFallback()
			if err != nil {
				return E.Cause(err, "create direct outbound for fallback")
			}
			m.add(directOutbound, nil)
			m.access.Lock()
			m.defaultOutbound = directOutbound
			m.access.Unlock()
		}
	}
	outbounds := m.all()
	if stage == adapter.StartStateStart {
		return m.startOutbounds(scopes, append(slices.Clone(outbounds), common.Map(m.endpoint.Endpoints(), func(it adapter.Endpoint) adapter.Outbound { return it })...))
	}
	for _, outbound := range outbounds {
		lifecycle, isLifecycle := outbound.(adapter.Lifecycle)
		if !isLifecycle {
			continue
		}
		err := scopes.Start(m.name(outbound), lifecycle, stage)
		if err != nil {
			return err
		}
	}
	return nil
}

// startOutbounds is 1.14.3's, on the manager's own scopes: dependencies first,
// endpoints handed to the endpoint manager, a cycle or a missing dependency
// named in the error.
func (m *outboundManager) startOutbounds(scopes *scopeSet, outbounds []adapter.Outbound) error {
	started := make(map[string]bool)
	for {
		canContinue := false
	startOne:
		for _, outboundToStart := range outbounds {
			outboundTag := outboundToStart.Tag()
			if started[outboundTag] {
				continue
			}
			dependencies := outboundToStart.Dependencies()
			for _, dependency := range dependencies {
				if !started[dependency] {
					continue startOne
				}
			}
			started[outboundTag] = true
			canContinue = true
			if endpoint, isEndpoint := outboundToStart.(adapter.Endpoint); isEndpoint {
				err := m.endpoint.StartEndpoint(endpoint)
				if err != nil {
					return err
				}
				continue
			}
			lifecycle, isLifecycle := outboundToStart.(adapter.Lifecycle)
			if !isLifecycle {
				continue
			}
			err := scopes.Start(m.name(outboundToStart), lifecycle, adapter.StartStateStart)
			if err != nil {
				return err
			}
		}
		if len(started) == len(outbounds) {
			break
		}
		if canContinue {
			continue
		}
		currentOutbound := common.Find(outbounds, func(it adapter.Outbound) bool {
			return !started[it.Tag()]
		})
		var lintOutbound func(oTree []string, oCurrent adapter.Outbound) error
		lintOutbound = func(oTree []string, oCurrent adapter.Outbound) error {
			problemOutboundTag := common.Find(oCurrent.Dependencies(), func(it string) bool {
				return !started[it]
			})
			if common.Contains(oTree, problemOutboundTag) {
				return E.New("circular outbound dependency: ", strings.Join(oTree, " -> "), " -> ", problemOutboundTag)
			}
			problemOutbound := common.Find(outbounds, func(it adapter.Outbound) bool {
				return it.Tag() == problemOutboundTag
			})
			if problemOutbound == nil {
				return E.New("dependency[", problemOutboundTag, "] not found for outbound[", oCurrent.Tag(), "]")
			}
			return lintOutbound(append(oTree, problemOutboundTag), problemOutbound)
		}
		return lintOutbound([]string{currentOutbound.Tag()}, currentOutbound)
	}
	return nil
}

func (m *outboundManager) Outbounds() []adapter.Outbound {
	return m.all()
}

func (m *outboundManager) Outbound(tag string) (adapter.Outbound, bool) {
	if outbound, found := m.get(tag); found {
		return outbound, true
	}
	return m.endpoint.Get(tag)
}

// Default looks route.final up by tag rather than keeping the object it found
// at start: an endpoint named there lives in the endpoint manager, and
// replacing it -- an edit -- would otherwise leave the default on the closed
// instance, failing every unmatched connection until a restart.
func (m *outboundManager) Default() adapter.Outbound {
	if m.defaultTag != "" {
		if outbound, found := m.Outbound(m.defaultTag); found {
			return outbound
		}
	}
	m.access.RLock()
	defer m.access.RUnlock()
	return m.defaultOutbound
}

func (m *outboundManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, outboundType string, options any) error {
	if tag == "" {
		return os.ErrInvalid
	}
	return m.create(recipe{ctx: ctx, router: router, logger: logger, tag: tag, componentType: outboundType, options: options})
}

func (m *outboundManager) Remove(tag string) error {
	return m.remove(tag)
}
