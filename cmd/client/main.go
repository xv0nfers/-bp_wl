package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cacggghp/vk-turn-proxy/pkg/dpi_evasion"
	"github.com/cacggghp/vk-turn-proxy/pkg/probe"
	"github.com/cacggghp/vk-turn-proxy/pkg/turn_discovery"
	"github.com/cacggghp/vk-turn-proxy/pkg/v2ray_bridge"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

type options struct {
	Turn         string
	Port         string
	Listen       string
	Peer         string
	Link         string
	AutoTurn     bool
	MimicVK      bool
	PaddingMax   int
	Jitter       int
	N            int
	UDP          bool
	RotateTurn   bool
	NoDTLS       bool
	Hysteria     string
	Probe        bool
	DryRun       bool
	V2ClientConf string
	V2ServerConf string
}

type streamLogger struct {
	streamID int
}

func (l streamLogger) log(event string, fields map[string]any) {
	payload := map[string]any{"event": event, "stream_id": l.streamID, "ts": time.Now().UTC().Format(time.RFC3339Nano)}
	for k, v := range fields {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("event=%s stream_id=%d marshal_error=%v", event, l.streamID, err)
		return
	}
	log.Print(string(b))
}

type telemetry struct {
	reconnects atomic.Uint64
	failovers  atomic.Uint64
	mu         sync.Mutex
	latencyMs  map[string][]int64
}

func newTelemetry() *telemetry {
	return &telemetry{latencyMs: map[string][]int64{"lt100": {}, "lt300": {}, "lt1000": {}, "ge1000": {}}}
}

func (t *telemetry) ObserveLatency(_ string, latency time.Duration, err error) {
	if err != nil {
		return
	}
	ms := latency.Milliseconds()
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case ms < 100:
		t.latencyMs["lt100"] = append(t.latencyMs["lt100"], ms)
	case ms < 300:
		t.latencyMs["lt300"] = append(t.latencyMs["lt300"], ms)
	case ms < 1000:
		t.latencyMs["lt1000"] = append(t.latencyMs["lt1000"], ms)
	default:
		t.latencyMs["ge1000"] = append(t.latencyMs["ge1000"], ms)
	}
}

func (t *telemetry) snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]any{
		"reconnect_total": t.reconnects.Load(),
		"failover_total":  t.failovers.Load(),
		"latency_bins": map[string]int{
			"lt100":  len(t.latencyMs["lt100"]),
			"lt300":  len(t.latencyMs["lt300"]),
			"lt1000": len(t.latencyMs["lt1000"]),
			"ge1000": len(t.latencyMs["ge1000"]),
		},
	}
}

type turnNode struct {
	server      turn_discovery.Server
	healthScore float64
	fails       int
}

type turnPool struct {
	mu    sync.Mutex
	nodes []turnNode
	next  atomic.Uint64
}

func newTurnPool(servers []turn_discovery.Server) *turnPool {
	if len(servers) == 0 {
		servers = []turn_discovery.Server{{Host: "5.255.211.241", Port: "3478"}}
	}
	nodes := make([]turnNode, 0, len(servers))
	for _, s := range servers {
		nodes = append(nodes, turnNode{server: s, healthScore: 1.0})
	}
	return &turnPool{nodes: nodes}
}

func (p *turnPool) pick() turnNode {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.nodes) == 0 {
		return turnNode{server: turn_discovery.Server{Host: "5.255.211.241", Port: "3478"}, healthScore: 1.0}
	}
	start := int(p.next.Add(1)-1) % len(p.nodes)
	best := start
	bestScore := p.nodes[start].healthScore
	for i := 1; i < len(p.nodes); i++ {
		idx := (start + i) % len(p.nodes)
		if p.nodes[idx].healthScore > bestScore {
			best = idx
			bestScore = p.nodes[idx].healthScore
		}
	}
	return p.nodes[best]
}

func (p *turnPool) report(host string, success bool) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.nodes {
		if p.nodes[i].server.Host != host {
			continue
		}
		if success {
			p.nodes[i].fails = 0
			p.nodes[i].healthScore += 0.1
			if p.nodes[i].healthScore > 1.0 {
				p.nodes[i].healthScore = 1.0
			}
			return 0
		}
		p.nodes[i].fails++
		p.nodes[i].healthScore -= 0.25
		if p.nodes[i].healthScore < 0.1 {
			p.nodes[i].healthScore = 0.1
		}
		return p.nodes[i].fails
	}
	return 1
}

func main() {
	opt, err := parseFlagsFrom(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if opt.Hysteria != "" {
		log.Printf("hysteria inner tunnel enabled with config: %s", opt.Hysteria)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	trap(cancel)
	metrics := newTelemetry()

	discoverer := turn_discovery.New()
	res, err := discoverer.Discover(ctx, opt.Link, opt.AutoTurn)
	if err != nil {
		log.Printf("TURN auto-discovery failed, fallback enabled: %v", err)
	}
	if opt.Turn != "" {
		res.Servers = []turn_discovery.Server{{Host: opt.Turn, Port: choosePort(opt.Port)}}
	}

	if opt.Probe {
		pr := probe.RunWithHook(ctx, metrics)
		if pr.Detected {
			log.Printf("TSPU detected, enabling full evasion")
			opt.MimicVK = true
			if opt.PaddingMax < 512 {
				opt.PaddingMax = 512
			}
			if opt.Jitter < 50 {
				opt.Jitter = 50
			}
		}
	}

	if opt.DryRun {
		log.Printf("dry-run completed: provider=%s turn_servers=%d", res.Provider, len(res.Servers))
		b, _ := json.Marshal(metrics.snapshot())
		log.Printf("telemetry=%s", b)
		return
	}

	peer, err := net.ResolveUDPAddr("udp", opt.Peer)
	if err != nil {
		log.Fatalf("resolve peer: %v", err)
	}
	listenConn, err := net.ListenPacket("udp", opt.Listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer listenConn.Close()

	evader := dpi_evasion.New(dpi_evasion.Config{MimicVK: opt.MimicVK, PaddingMax: opt.PaddingMax, JitterMs: opt.Jitter})
	pool := newTurnPool(res.Servers)

	errCh := make(chan error, opt.N)
	for i := 0; i < opt.N; i++ {
		go func(streamID int) {
			errCh <- connectionLoop(ctx, opt, streamLogger{streamID: streamID}, listenConn, peer, pool, evader, metrics)
		}(i + 1)
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			log.Printf("worker ended: %v", err)
		}
	}
}

func parseFlagsFrom(args []string) (options, error) {
	opt := options{}
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	vk := fs.String("vk-link", "", "VK call link")
	ya := fs.String("yandex-link", "", "Yandex telemost link")
	fs.StringVar(&opt.Turn, "turn", "", "override turn ip")
	fs.StringVar(&opt.Port, "port", "", "override turn port")
	fs.StringVar(&opt.Listen, "listen", "127.0.0.1:9000", "local listen")
	fs.StringVar(&opt.Peer, "peer", "", "peer host:port")
	fs.BoolVar(&opt.AutoTurn, "auto-turn", false, "dynamic TURN discovery")
	fs.BoolVar(&opt.MimicVK, "mimic-vk", false, "mimic vk packet profile")
	fs.IntVar(&opt.PaddingMax, "padding-max", 512, "max random padding")
	fs.IntVar(&opt.Jitter, "jitter", 50, "max jitter ms")
	fs.IntVar(&opt.N, "n", 4, "parallel streams (1-32)")
	fs.BoolVar(&opt.UDP, "udp", false, "udp-only mode")
	fs.BoolVar(&opt.RotateTurn, "rotate-turn", false, "rotate turn servers")
	fs.BoolVar(&opt.NoDTLS, "no-dtls", false, "disable DTLS")
	fs.StringVar(&opt.Hysteria, "hysteria", "", "hysteria2 json config path")
	fs.BoolVar(&opt.Probe, "probe", true, "run TSPU probe")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "check discovery/probe and exit without tunnel")
	fs.StringVar(&opt.V2ClientConf, "gen-v2ray-client", "", "write v2ray client json")
	fs.StringVar(&opt.V2ServerConf, "gen-v2ray-server", "", "write v2ray server json")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	if *vk == "" && *ya == "" {
		return options{}, errors.New("need -vk-link or -yandex-link")
	}
	if opt.Peer == "" && !opt.DryRun {
		return options{}, errors.New("need -peer")
	}
	if *vk != "" {
		opt.Link = *vk
	} else {
		opt.Link = *ya
	}
	if opt.N < 1 {
		opt.N = 1
	}
	if opt.N > 32 {
		opt.N = 32
	}
	if opt.V2ClientConf != "" || opt.V2ServerConf != "" {
		generateV2(opt)
	}
	return opt, nil
}

func generateV2(opt options) {
	if opt.V2ClientConf != "" {
		if err := v2ray_bridge.GenerateClient(opt.V2ClientConf); err != nil {
			log.Printf("failed to generate client config: %v", err)
		}
	}
	if opt.V2ServerConf != "" {
		if err := v2ray_bridge.GenerateServer(opt.V2ServerConf); err != nil {
			log.Printf("failed to generate server config: %v", err)
		}
	}
}

func trap(cancel context.CancelFunc) {
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sc
		cancel()
	}()
}

func connectionLoop(ctx context.Context, opt options, logger streamLogger, listenConn net.PacketConn, peer *net.UDPAddr, pool *turnPool, evader *dpi_evasion.Evasion, metrics *telemetry) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		node := pool.pick()
		started := time.Now()
		err := oneConnection(ctx, opt, logger, listenConn, peer, node.server, evader)
		if err != nil {
			metrics.failovers.Add(1)
			fails := pool.report(node.server.Host, false)
			backoff := backoffDuration(fails)
			logger.log("failover", map[string]any{"turn_id": node.server.Host, "error": err.Error(), "backoff_ms": backoff.Milliseconds(), "health_score": fmt.Sprintf("%.2f", node.healthScore)})
			time.Sleep(backoff)
			continue
		}
		pool.report(node.server.Host, true)
		metrics.reconnects.Add(1)
		logger.log("reconnect", map[string]any{"turn_id": node.server.Host, "uptime_ms": time.Since(started).Milliseconds()})
	}
}

func backoffDuration(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	if fails > 6 {
		fails = 6
	}
	return time.Duration(150*(1<<(fails-1))) * time.Millisecond
}

func oneConnection(ctx context.Context, opt options, logger streamLogger, listenConn net.PacketConn, peer *net.UDPAddr, server turn_discovery.Server, evader *dpi_evasion.Evasion) error {
	addr := net.JoinHostPort(server.Host, choosePort(server.Port, opt.Port))
	conn, err := dialTURN(ctx, addr, server, opt.UDP, peer.IP.To4() != nil)
	if err != nil {
		if !opt.UDP {
			conn, err = dialFallbackTCP(ctx, addr, server, peer.IP.To4() != nil)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	defer conn.Close()

	proxyConn := conn
	if !opt.NoDTLS {
		proxyConn, err = wrapDTLS(ctx, conn, peer)
		if err != nil {
			return err
		}
		defer proxyConn.Close()
	}

	var packets uint64
	started := time.Now()
	buf := make([]byte, 1600)
	logger.log("turn_connected", map[string]any{"turn_id": server.Host, "turn_addr": addr})
	for {
		if opt.RotateTurn && (packets > 10000 || time.Since(started) > 5*time.Minute) {
			return fmt.Errorf("rotate-turn trigger")
		}
		listenConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, src, err := listenConn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		out := evader.Transform(buf[:n])
		evader.Delay()
		proxyConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err = proxyConn.WriteTo(out, peer); err != nil {
			return err
		}
		proxyConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		rn, _, err := proxyConn.ReadFrom(buf)
		if err != nil {
			return err
		}
		if _, err = listenConn.WriteTo(buf[:rn], src); err != nil {
			return err
		}
		packets++
	}
}

func dialTURN(ctx context.Context, addr string, s turn_discovery.Server, udp bool, ipv4 bool) (net.PacketConn, error) {
	var pc net.PacketConn
	if udp {
		u, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, err
		}
		c, err := net.DialUDP("udp", nil, u)
		if err != nil {
			return nil, err
		}
		pc = &udpConnected{c}
	} else {
		d := net.Dialer{Timeout: 5 * time.Second}
		c, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		pc = turn.NewSTUNConn(c)
	}
	family := turn.RequestedAddressFamilyIPv6
	if ipv4 {
		family = turn.RequestedAddressFamilyIPv4
	}
	cl, err := turn.NewClient(&turn.ClientConfig{STUNServerAddr: addr, TURNServerAddr: addr, Conn: pc, Username: s.Username, Password: s.Password, RequestedAddressFamily: family, LoggerFactory: logging.NewDefaultLoggerFactory()})
	if err != nil {
		return nil, err
	}
	if err = cl.Listen(); err != nil {
		return nil, err
	}
	r, err := cl.Allocate()
	if err != nil {
		return nil, err
	}
	log.Printf("using TURN %s relayed=%s", addr, r.LocalAddr())
	return r, nil
}

func dialFallbackTCP(ctx context.Context, addr string, s turn_discovery.Server, ipv4 bool) (net.PacketConn, error) {
	return dialTURN(ctx, addr, s, true, ipv4)
}

func wrapDTLS(ctx context.Context, conn net.PacketConn, peer *net.UDPAddr) (net.PacketConn, error) {
	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return nil, err
	}
	cfg := &dtls.Config{Certificates: []tls.Certificate{cert}, InsecureSkipVerify: true, ExtendedMasterSecret: dtls.RequireExtendedMasterSecret, CipherSuites: []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}, FlightInterval: 100 * time.Millisecond}
	dc, err := dtls.Client(conn, peer, cfg)
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = dc.HandshakeContext(hctx); err != nil {
		return nil, err
	}
	return &dtlsPacket{Conn: dc}, nil
}

type udpConnected struct{ *net.UDPConn }

func (u *udpConnected) WriteTo(p []byte, _ net.Addr) (int, error) { return u.Write(p) }

type dtlsPacket struct{ Conn net.Conn }

func (d *dtlsPacket) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := d.Conn.Read(p)
	return n, d.Conn.RemoteAddr(), err
}
func (d *dtlsPacket) WriteTo(p []byte, _ net.Addr) (int, error) { return d.Conn.Write(p) }
func (d *dtlsPacket) Close() error                              { return d.Conn.Close() }
func (d *dtlsPacket) LocalAddr() net.Addr                       { return d.Conn.LocalAddr() }
func (d *dtlsPacket) SetDeadline(t time.Time) error             { return d.Conn.SetDeadline(t) }
func (d *dtlsPacket) SetReadDeadline(t time.Time) error         { return d.Conn.SetReadDeadline(t) }
func (d *dtlsPacket) SetWriteDeadline(t time.Time) error        { return d.Conn.SetWriteDeadline(t) }

func choosePort(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return "3478"
}
