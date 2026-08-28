package notify

import "time"

// Kind identifies what happened.
//
// The string form is what the notifyEvents setting stores, so these values are
// part of the panel's persisted configuration: renaming one silently turns that
// event off for every operator who had it enabled. Add new kinds, do not rename
// existing ones.
type Kind string

const (
	// State events. Delivery is gated on a transition -- see Suppressor.
	NodeDown     Kind = "node.down"
	NodeUp       Kind = "node.up"
	CoreCrash    Kind = "core.crash"
	CoreUp       Kind = "core.up"
	OutboundDown Kind = "outbound.down"
	OutboundUp   Kind = "outbound.up"

	// One-shot events, rate limited per subject.
	ClientDepleted Kind = "client.depleted"
	ClientExpiring Kind = "client.expiring"
	CPUHigh        Kind = "cpu.high"
	MemoryHigh     Kind = "memory.high"

	// Login events, each handled differently -- see Suppressor.Decide.
	LoginSuccess Kind = "login.success"
	LoginFailed  Kind = "login.failed"
	LoginBanned  Kind = "login.banned"
)

// AllKinds is the order the settings page lists the toggles in.
var AllKinds = []Kind{
	NodeDown, NodeUp, CoreCrash, CoreUp,
	OutboundDown, OutboundUp,
	ClientDepleted, ClientExpiring, CPUHigh, MemoryHigh,
	LoginSuccess, LoginFailed, LoginBanned,
}

// Event is one thing worth telling the operator about.
type Event struct {
	Kind Kind
	// Subject is what the event is about: a node name, a client name, a source
	// IP. It is half of the suppression key, so events about different subjects
	// never suppress one another.
	Subject string
	// Data carries the kind-specific payload, one of the types below. Renderers
	// type-assert it and must tolerate a nil or mismatched value rather than
	// panicking -- an event source that forgets to attach it should degrade to
	// a plainer message, not take the panel down.
	Data any
	// Text, when set, is delivered verbatim instead of rendering Kind. The
	// scheduled digests use it: their body is assembled from client and node
	// data this package cannot reach without depending on service, which would
	// close an import cycle.
	Text string
	At   time.Time
}

// NodeData accompanies NodeDown / NodeUp.
type NodeData struct {
	LatencyMs int64
	Err       string
}

// CoreData accompanies CoreCrash.
type CoreData struct {
	Err string
}

// OutboundData accompanies OutboundDown / OutboundUp. Separate from NodeData
// despite the identical shape: the two describe different things, and merging
// them would make a later field that only one of them has read as if it applied
// to both.
type OutboundData struct {
	LatencyMs uint16
	Err       string
}

// ClientData accompanies ClientDepleted / ClientExpiring. Names is plural
// because DepleteJob disables a whole batch in one pass and sends one event for
// the batch rather than one per client.
type ClientData struct {
	Names []string
	// DaysLeft / BytesLeft are only set for ClientExpiring, and only the one
	// that actually tripped is non-zero.
	DaysLeft  int
	BytesLeft int64
	// Targets are the clients this event is about that have a Telegram binding,
	// so they can be warned directly as well as the operator. Empty whenever
	// nobody involved has one, which is the common case -- the binding is
	// optional and set through the bot, not the panel's client form.
	//
	// The operator's message is still rendered from the fields above; this only
	// adds recipients, it does not change what the operator is told.
	Targets []ClientTarget
}

// ClientTarget is one client to warn on their own Telegram chat, carrying the
// figures that client's message needs. They are repeated here rather than read
// off ClientData because a batched event (ClientDepleted) stands for many
// clients at once, each with its own numbers.
type ClientTarget struct {
	Name      string
	TgId      int64
	DaysLeft  int
	BytesLeft int64
}

// LoginData accompanies the three login kinds. Failures is the number of
// attempts folded into this notification, which is 1 for the first alert in a
// window and higher when the limiter merged some -- see Suppressor.Decide.
type LoginData struct {
	Username   string
	IP         string
	Failures   int
	BanMinutes int
}

// MetricData accompanies CPUHigh / MemoryHigh.
type MetricData struct {
	Percent   float64
	Threshold int
}
