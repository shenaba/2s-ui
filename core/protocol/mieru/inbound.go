package mieru

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shenaba/2s-ui/core/usersession"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/listener"
	"github.com/sagernet/sing-box/common/uot"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	mierucommon "github.com/enfein/mieru/v3/apis/common"
	mieruconstant "github.com/enfein/mieru/v3/apis/constant"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
	mierutp "github.com/enfein/mieru/v3/apis/trafficpattern"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlcommon"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	mieruutil "github.com/enfein/mieru/v3/pkg/common"
	mierulog "github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"google.golang.org/protobuf/proto"
)

// The server is built on mieru's pkg/protocol Mux rather than on its
// apis/server wrapper. The wrapper takes the user list once, before Start, and
// refuses it afterwards; the Mux underneath swaps it atomically at any time.
// That is what lets UpdateUsers change who may connect without tearing the
// listener down and dropping every other client, the same in-place update the
// other user-carrying inbounds get. The price is depending on a package mieru
// does not promise to keep stable, which a version bump has to re-check.
//
// Start below repeats what apis/server's Start does with the config, minus the
// parts it does not use (port ranges, the listen address the Mux would bind --
// the listeners come from sing-box instead, see listenerFactory).

func RegisterInbound(registry *inbound.Registry) {
	inbound.Register[InboundOptions](registry, TypeMieru, NewInbound)
}

// mieru logs through a global logger of its own, at a volume meant for its
// standalone daemon. apis/server silences it the same way.
var silenceLog sync.Once

type Inbound struct {
	inbound.Adapter
	ctx       context.Context
	router    adapter.ConnectionRouterEx
	logger    log.ContextLogger
	listener  *listener.Listener
	transport string
	pattern   *mierupb.TrafficPattern
	hint      bool

	// users is the name set the Mux currently accepts, read on every new
	// session to close the gap between a removal and a Bind -- see
	// handleConnection.
	users    atomic.Pointer[map[string]struct{}]
	userList atomic.Pointer[[]User]
	// sessions holds the TCP underlays, the only mieru transport a removed
	// user can keep riding: a stream underlay authenticates once and then
	// carries any number of sessions. UDP authenticates every session on its
	// own, so a removal locks it out of new ones by itself.
	sessions *usersession.Registry

	access sync.Mutex
	mux    *protocol.Mux
	closed bool
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options InboundOptions) (adapter.Inbound, error) {
	transport, err := normalizeTransport(options.Transport)
	if err != nil {
		return nil, err
	}
	if options.ListenPort == 0 {
		return nil, E.New("mieru: listen_port must be set")
	}
	var pattern *mierupb.TrafficPattern
	if options.TrafficPattern != "" {
		pattern, err = mierutp.Decode(options.TrafficPattern)
		if err != nil {
			return nil, E.Cause(err, "mieru: decode traffic_pattern")
		}
		if err = mierutp.Validate(pattern); err != nil {
			return nil, E.Cause(err, "mieru: invalid traffic_pattern")
		}
	}
	silenceLog.Do(func() {
		mierulog.SetFormatter(&mierulog.NilFormatter{})
	})

	network := N.NetworkTCP
	if transport == TransportUDP {
		network = N.NetworkUDP
	}
	in := &Inbound{
		Adapter:   inbound.NewAdapter(TypeMieru, tag),
		ctx:       ctx,
		router:    uot.NewRouter(router, logger),
		logger:    logger,
		transport: transport,
		pattern:   pattern,
		hint:      options.UserHintIsMandatory,
		sessions:  usersession.NewRegistry(),
	}
	in.listener = listener.New(listener.Options{
		Context: ctx,
		Logger:  logger,
		Network: []string{network},
		Listen:  options.ListenOptions,
	})
	in.storeUsers(in.usable(options.Users))
	return in, nil
}

func normalizeTransport(transport string) (string, error) {
	switch transport {
	case "", TransportTCP, "tcp":
		return TransportTCP, nil
	case TransportUDP, "udp":
		return TransportUDP, nil
	}
	return "", E.New("mieru: transport must be TCP or UDP, not ", transport)
}

// usableUsers drops the users mieru cannot serve and says why for each.
//
// They are dropped rather than refused. The list is every client attached to
// the inbound, read straight from the database, and one bad row must not cost
// the others their listener -- or, at start, the whole core, which gives up on
// the first inbound that fails. The other inbounds take the same rows as they
// come; mieru alone is strict about them, in two ways:
//
//   - the name is half of the key, so it has to be there, to be unique, and
//     to fit the 64 bytes the protocol hashes (22 CJK characters is over);
//   - mieru itself skips such a user without a word -- its log is silenced
//     here -- and refuses to start once none is left.
//
// Old installs do hold clients sharing a name (see ensureNameAvailable in
// service/client.go). The first one keeps it.
func usableUsers(users []User) ([]User, []string) {
	kept := make([]User, 0, len(users))
	var skipped []string
	seen := make(map[string]struct{}, len(users))
	for _, user := range users {
		switch {
		case user.Name == "":
			skipped = append(skipped, "a user with no name")
			continue
		case len(user.Name) > mieruconstant.MaxUserNameLen:
			skipped = append(skipped, F.ToString("user ", user.Name, ": name is longer than ", mieruconstant.MaxUserNameLen, " bytes"))
			continue
		case user.Password == "":
			skipped = append(skipped, "user "+user.Name+": no password")
			continue
		case len(user.Password) > maxPasswordLen:
			skipped = append(skipped, F.ToString("user ", user.Name, ": password is longer than ", maxPasswordLen, " bytes"))
			continue
		}
		if _, dup := seen[user.Name]; dup {
			skipped = append(skipped, "user "+user.Name+": another user of this inbound has the same name")
			continue
		}
		seen[user.Name] = struct{}{}
		kept = append(kept, user)
	}
	return kept, skipped
}

// maxPasswordLen is what mieru's client accepts; it has no constant for it.
const maxPasswordLen = 64

// usable is usableUsers with every dropped user reported, since nothing else
// tells the operator why that client cannot connect.
func (h *Inbound) usable(users []User) []User {
	kept, skipped := usableUsers(users)
	for _, reason := range skipped {
		h.logger.Warn("mieru: skipping ", reason)
	}
	return kept
}

func (h *Inbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	h.access.Lock()
	defer h.access.Unlock()
	if h.closed {
		return E.New("mieru: inbound is closed")
	}

	mux := protocol.NewMux(false)
	mux.SetStreamListenerFactory(listenerFactory{h})
	mux.SetPacketListenerFactory(listenerFactory{h})
	pattern, err := mierutp.NewConfig(h.pattern)
	if err != nil {
		return E.Cause(err, "mieru: traffic pattern")
	}
	mux.SetTrafficPattern(pattern).
		SetServerUsers(serverUsers(h.currentUsers())).
		SetServerUserHintIsMandatory(h.hint)

	// The address only tells the Mux which transport to open; listenerFactory
	// ignores it and binds what the ListenOptions say.
	transport := mierupb.TransportProtocol_TCP
	if h.transport == TransportUDP {
		transport = mierupb.TransportProtocol_UDP
	}
	endpoints, err := appctlcommon.AddrPortToUnderlayProperties("", []*mierupb.PortBinding{{
		Port:     proto.Int32(int32(h.listener.ListenOptions().ListenPort)),
		Protocol: transport.Enum(),
	}}, mieruutil.DefaultMTU)
	if err != nil {
		return E.Cause(err, "mieru: listen endpoint")
	}
	mux.SetEndpoints(endpoints)
	if err = mux.Start(); err != nil {
		h.listener.Close()
		return E.Cause(err, "mieru: start server")
	}
	h.mux = mux
	scope.Add(h.Close)
	go h.acceptLoop(mux)
	return nil
}

func (h *Inbound) Close() error {
	h.access.Lock()
	defer h.access.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	// The sockets first, and here rather than left to the Mux. It closes a TCP
	// listener from a goroutine of its own, some time after its Close has
	// returned, and whatever replaces this inbound binds the same port at once.
	// Closing them after the Mux instead closed each a second time and
	// reported that as this inbound failing to close.
	err := h.listener.Close()
	if h.mux != nil {
		_ = h.mux.Close()
	}
	return err
}

func (h *Inbound) acceptLoop(mux *protocol.Mux) {
	for {
		conn, err := mux.Accept()
		if err != nil {
			return
		}
		go h.handleConnection(conn)
	}
}

// handleConnection reads the SOCKS5 request a mieru client sends at the start
// of every session -- apis/server does the same in Accept, where one slow
// client would hold up every other -- and routes it.
func (h *Inbound) handleConnection(conn net.Conn) {
	ctx := log.ContextWithNewID(h.ctx)
	mieruutil.SetReadTimeout(conn, 10*time.Second)
	request := &mierumodel.Request{}
	err := request.ReadFromSocks5(conn)
	mieruutil.SetReadTimeout(conn, 0)
	if err != nil {
		conn.Close()
		h.logger.DebugContext(ctx, "mieru: read request: ", err)
		return
	}

	var metadata adapter.InboundContext
	metadata.Inbound = h.Tag()
	metadata.InboundType = h.Type()
	//nolint:staticcheck
	metadata.InboundDetour = h.listener.ListenOptions().Detour
	metadata.UDPDisableDomainUnmapping = h.listener.ListenOptions().UDPDisableDomainUnmapping
	if remote := conn.RemoteAddr(); remote != nil {
		metadata.Source = M.SocksaddrFromNet(remote)
	}
	metadata.Destination = socksaddrOf(request.DstAddr)
	if userConn, ok := conn.(mierucommon.UserContext); ok {
		metadata.User = userConn.UserName()
	}

	if h.transport == TransportTCP && metadata.User != "" {
		source := metadata.Source.String()
		h.sessions.Bind(metadata.User, source)
		// A removal that ran between this session authenticating and the Bind
		// above saw no user on the underlay and left it open. Checking the
		// table again after the Bind closes that window: either the removal
		// comes later and finds the user bound, or it came first and the
		// check here sees the new table.
		if !h.allowed(metadata.User) {
			h.sessions.KickUserSessions(metadata.User)
			conn.Close()
			return
		}
	}

	response := &mierumodel.Response{
		Reply:    mieruconstant.Socks5ReplySuccess,
		BindAddr: mierumodel.AddrSpec{IP: net.IPv4zero, Port: 0},
	}
	if err = response.WriteToSocks5(conn); err != nil {
		conn.Close()
		h.logger.DebugContext(ctx, "mieru: write response: ", err)
		return
	}

	switch request.Command {
	case mieruconstant.Socks5ConnectCmd:
		h.logger.InfoContext(ctx, "inbound connection from ", metadata.Source, " to ", metadata.Destination)
		h.router.RouteConnectionEx(ctx, conn, metadata, nil)
	case mieruconstant.Socks5UDPAssociateCmd:
		// The request names no destination worth routing on: mieru's own
		// client sends 0.0.0.0:0, as SOCKS5 has it, and every datagram carries
		// its own. Route on the first one's, the way sing's socks inbound
		// does, or every rule written on a destination misses all UDP.
		packets := &packetConn{PacketConn: mierucommon.NewPacketOverStreamTunnel(conn)}
		first := buf.NewPacket()
		destination, err := packets.ReadPacket(first)
		if err != nil {
			first.Release()
			packets.Close()
			h.logger.DebugContext(ctx, "mieru: read first packet: ", err)
			return
		}
		metadata.Destination = destination
		h.logger.InfoContext(ctx, "inbound packet connection from ", metadata.Source, " to ", metadata.Destination)
		h.router.RoutePacketConnectionEx(ctx, bufio.NewCachedPacketConn(packets, first, destination), metadata, nil)
	default:
		conn.Close()
		h.logger.WarnContext(ctx, "mieru: unsupported command ", request.Command)
	}
}

func socksaddrOf(addr mierumodel.AddrSpec) M.Socksaddr {
	if addr.FQDN != "" {
		return M.Socksaddr{Fqdn: addr.FQDN, Port: uint16(addr.Port)}
	}
	if ip, ok := netip.AddrFromSlice(addr.IP); ok {
		return M.Socksaddr{Addr: ip.Unmap(), Port: uint16(addr.Port)}
	}
	return M.Socksaddr{Port: uint16(addr.Port)}
}

// packetConn carries UDP associate traffic. mieru tunnels each datagram over
// the session stream with a SOCKS5 UDP header in front of it.
type packetConn struct {
	net.PacketConn
}

var _ N.PacketConn = (*packetConn)(nil)

func (c *packetConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	n, _, err := c.PacketConn.ReadFrom(buffer.FreeBytes())
	if err != nil {
		return M.Socksaddr{}, err
	}
	buffer.Truncate(n)
	if buffer.Len() < 3 {
		return M.Socksaddr{}, io.ErrShortBuffer
	}
	// RSV (2 bytes) and FRAG (1 byte).
	buffer.Advance(3)
	var addr mierumodel.AddrSpec
	if err = addr.ReadFromSocks5(buffer); err != nil {
		return M.Socksaddr{}, err
	}
	return socksaddrOf(addr), nil
}

func (c *packetConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	defer buffer.Release()
	header := buf.NewSize(3 + M.MaxSocksaddrLength)
	defer header.Release()
	common.Must(header.WriteZeroN(3))
	var addr mierumodel.AddrSpec
	if destination.IsFqdn() {
		addr.FQDN = destination.Fqdn
	} else {
		addr.IP = destination.Addr.AsSlice()
	}
	addr.Port = int(destination.Port)
	if err := addr.WriteToSocks5(header); err != nil {
		return err
	}
	packet := buf.NewSize(header.Len() + buffer.Len())
	defer packet.Release()
	common.Must1(packet.Write(header.Bytes()))
	common.Must1(packet.Write(buffer.Bytes()))
	_, err := c.PacketConn.WriteTo(packet.Bytes(), nil)
	return err
}

// listenerFactory hands mieru the sockets sing-box opens from the inbound's
// ListenOptions, so listen, bind_interface, routing_mark, tcp_fast_open and the
// rest mean here what they mean on every other inbound. The address mieru
// passes in is ignored for the same reason.
type listenerFactory struct {
	inbound *Inbound
}

func (f listenerFactory) Listen(context.Context, string, string) (net.Listener, error) {
	tcpListener, err := f.inbound.listener.ListenTCP()
	if err != nil {
		return nil, err
	}
	return &trackingListener{Listener: tcpListener, sessions: f.inbound.sessions, logger: f.inbound.logger}, nil
}

func (f listenerFactory) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return f.inbound.listener.ListenUDP()
}

// trackingListener registers every TCP connection under its remote address
// before mieru wraps it into an underlay. A session's RemoteAddr is its
// underlay's, so the Bind in handleConnection finds the connection here and a
// removal or a kick can close the whole underlay, not just the session.
//
// It also keeps the listener alive. mieru leaves its accept loop on the first
// error of any kind and says so only to its own log, which is silenced here:
// one accept failing for want of a file descriptor would stop the inbound
// taking connections for good, with the port still bound and nothing logged.
// So a temporary error is retried here, as sing-box's own listener does, and
// one that does end the listener is reported.
type trackingListener struct {
	net.Listener
	sessions *usersession.Registry
	logger   log.ContextLogger
}

func (l *trackingListener) Accept() (net.Conn, error) {
	var delay time.Duration
	for {
		conn, err := l.accept()
		if err == nil {
			return conn, nil
		}
		if E.IsClosed(err) {
			return nil, err
		}
		//nolint:staticcheck
		if netError, isNetError := err.(net.Error); !isNetError || !netError.Temporary() {
			l.logger.Error("mieru: listener stopped accepting connections: ", err)
			return nil, err
		}
		if delay == 0 {
			delay = 5 * time.Millisecond
		} else {
			delay = min(2*delay, time.Second)
		}
		l.logger.Error("mieru: accept: ", err, ", retrying in ", delay)
		time.Sleep(delay)
	}
}

func (l *trackingListener) accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tracked := &trackedConn{Conn: conn, sessions: l.sessions, source: conn.RemoteAddr().String()}
	l.sessions.Track(tracked.source, tracked)
	return tracked, nil
}

type trackedConn struct {
	net.Conn
	sessions *usersession.Registry
	source   string
	once     sync.Once
}

func (c *trackedConn) Close() error {
	c.once.Do(func() { c.sessions.Untrack(c.source) })
	return c.Conn.Close()
}

func (c *trackedConn) Upstream() any {
	return c.Conn
}

// placeholderUser keeps an inbound with no clients running. mieru refuses to
// start without a user, while every other inbound starts empty and waits for
// clients to be attached. The name and password are random, so nothing can
// ever authenticate as it.
func placeholderUser() *mierupb.User {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	secret := hex.EncodeToString(raw[:])
	return &mierupb.User{
		Name:     proto.String("2s-ui-placeholder-" + secret[:8]),
		Password: proto.String(secret),
	}
}

func serverUsers(users []User) map[string]*mierupb.User {
	list := make([]*mierupb.User, 0, len(users)+1)
	for _, user := range users {
		list = append(list, &mierupb.User{
			Name:     proto.String(user.Name),
			Password: proto.String(user.Password),
		})
	}
	if len(list) == 0 {
		list = append(list, placeholderUser())
	}
	return appctlcommon.UserListToMap(list)
}
