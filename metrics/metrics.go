package metrics

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abstract-p2p/go-psyche"
)

var (
	metricsSubject = ".metrics"
)

type Metrics struct {
	edgesMu sync.Mutex
	edges   map[string]psyche.Interface

	gaugesMu sync.Mutex
	gauges   []*Gauge

	ctx       context.Context
	cancelCtx func()
}

func New() *Metrics {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Metrics{
		edges:     map[string]psyche.Interface{},
		ctx:       ctx,
		cancelCtx: cancel,
	}
	return m
}

// pub publishes a payload to the edge with the given remote address.
// if no raddr is provided, payload is publishes to all edges.
func (m *Metrics) pub(raddr string, payload []byte) {
	m.edgesMu.Lock()
	defer m.edgesMu.Unlock()

	if raddr != "" {
		if e, ok := m.edges[raddr]; ok {
			e.Pub(metricsSubject, payload)
		}
	} else {
		for _, e := range m.edges {
			e.Pub(metricsSubject, payload)
		}
	}
}

func (m *Metrics) pubMetrics(raddr string) {
	b := strings.Builder{}

	m.gaugesMu.Lock()
	for _, g := range m.gauges {
		val := g.Val()

		if g.raddr == "" || g.raddr == raddr {
			b.WriteString(g.StringWithVal(val))
			b.WriteByte('\n')
		}

		g.oncePerDur.Reset()
		g.oncePerDelta.Reset(val)
	}
	m.gaugesMu.Unlock()

	m.edgesMu.Lock()
	m.edges[raddr].Pub(metricsSubject, []byte(b.String()))
	m.edgesMu.Unlock()
}

func (m *Metrics) servePoller(edge psyche.Interface, raddr string) error {
	defer edge.Close()
	defer m.detach(edge, raddr)

	var msg psyche.Message
	for edge.ReadMsg(m.ctx, &msg) {
		if bytes.Equal(bytes.ToUpper(msg.Payload), []byte("POLL")) {
			m.pubMetrics(raddr)
		}
	}

	return edge.Err()
}

// Attach will subscribe to the metrics subject on the given
// edge and publish metrics updates on it. Resources associated
// with Attach will be freed when the given edge is closed.
func (m *Metrics) Attach(edge psyche.Interface, remoteAddr string) {
	m.edgesMu.Lock()
	m.edges[remoteAddr] = edge
	m.edgesMu.Unlock()

	edge.Sub(metricsSubject)
	go m.servePoller(edge, remoteAddr)
}

func (m *Metrics) detach(edge psyche.Interface, raddr string) {
	m.edgesMu.Lock()
	defer m.edgesMu.Unlock()
	if e, ok := m.edges[raddr]; ok && edge == e {
		delete(m.edges, raddr)
	}
}

func (m *Metrics) Close() {
	m.cancelCtx()
}

type Gauge struct {
	name         string
	val          int64
	oncePerDur   *OncePerDur
	oncePerDelta *OncePerDelta
	raddr        string
	m            *Metrics
}

// NewGauge creates a new gauge.
//
// Safe to call from multiple goroutines.
func (m *Metrics) NewGauge(name string, oncePerDur time.Duration, oncePerDelta int64, raddr string) *Gauge {
	g := &Gauge{
		name:         name,
		oncePerDur:   NewOncePerDur(oncePerDur),
		oncePerDelta: NewOncePerDelta(oncePerDelta),
		raddr:        raddr,
		m:            m,
	}

	m.gaugesMu.Lock()
	m.gauges = append(m.gauges, g)
	m.gaugesMu.Unlock()

	return g
}

func (g *Gauge) Val() int64 {
	return atomic.LoadInt64(&g.val)
}

func (g *Gauge) StringWithVal(val int64) string {
	return fmt.Sprintf("%s=%d", g.name, val)
}

func (g *Gauge) pub(v int64) {
	g.m.pub(g.raddr, []byte(g.StringWithVal(v)))
}

// Add adds a value to the gauge.
//
// Safe to call from multiple goroutines.
func (g *Gauge) Add(n int64) {
	if n == 0 {
		return
	}
	val := atomic.AddInt64(&g.val, n)
	g.oncePerDelta.Do(val, func() {
		g.oncePerDur.Do(func() {
			g.pub(val)
		})
	})
}

func (g *Gauge) Inc() {
	g.Add(1)
}

func (g *Gauge) Dec() {
	g.Add(-1)
}

func InFlightMiddleware(gauge *Gauge, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gauge.Inc()
		defer gauge.Dec()
		next.ServeHTTP(w, r)
	})
}

type metricsConn struct {
	net.Conn
	sentTotal,
	receivedTotal,
	sentThis,
	receivedThis *Gauge
}

func (mc *metricsConn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	mc.sentTotal.Add(int64(n))
	mc.sentThis.Add(int64(n))
	return
}

func (mc *metricsConn) Read(b []byte) (n int, err error) {
	n, err = mc.Conn.Read(b)
	mc.receivedTotal.Add(int64(n))
	mc.receivedThis.Add(int64(n))
	return
}

type metricsListener struct {
	net.Listener
	sentTotal,
	receivedTotal *Gauge
	connGauges func(raddr string) (sent, received *Gauge)
}

func (mln *metricsListener) Accept() (net.Conn, error) {
	conn, err := mln.Listener.Accept()

	raddr := conn.RemoteAddr()

	sent, received := mln.connGauges(raddr.String())

	return &metricsConn{
		Conn:          conn,
		sentTotal:     mln.sentTotal,
		receivedTotal: mln.receivedTotal,
		sentThis:      sent,
		receivedThis:  received,
	}, err
}

func SentReceivedMiddleware(
	sentTotal, receivedTotal *Gauge,
	connGauges func(raddr string) (sent, received *Gauge),
	ln net.Listener,
) net.Listener {
	return &metricsListener{
		Listener:      ln,
		sentTotal:     sentTotal,
		receivedTotal: receivedTotal,
		connGauges:    connGauges,
	}
}
