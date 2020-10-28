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
	edge psyche.Interface

	gaugesMu sync.Mutex
	gauges   []*Gauge

	ctx       context.Context
	cancelCtx func()
}

func New(edge psyche.Interface) *Metrics {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Metrics{
		edge:      edge,
		ctx:       ctx,
		cancelCtx: cancel,
	}
	go m.servePollers()
	return m
}

func (m *Metrics) pubAll() {
	var b strings.Builder

	m.gaugesMu.Lock()
	for _, g := range m.gauges {
		val := g.Val()
		b.WriteString(g.StringWithVal(val))
		b.WriteByte('\n')
		g.oncePerDur.Reset()
		g.oncePerDelta.Reset(val)
	}
	m.gaugesMu.Unlock()

	m.edge.Pub(metricsSubject, []byte(b.String()))
}

func (m *Metrics) servePollers() error {
	defer m.edge.Close()

	m.edge.Sub(metricsSubject)

	var msg psyche.Message
	for m.edge.ReadMsg(m.ctx, &msg) {
		if bytes.Equal(bytes.ToUpper(msg.Payload), []byte("POLL")) {
			m.pubAll()
		}
	}

	return m.edge.Err()
}

func (m *Metrics) Close() {
	m.cancelCtx()
}

type Gauge struct {
	name         string
	val          int64
	oncePerDur   *OncePerDur
	oncePerDelta *OncePerDelta
	m            *Metrics
}

// NewGauge creates a new gauge.
//
// Safe to call from multiple goroutines.
func (m *Metrics) NewGauge(name string, oncePerDur time.Duration, oncePerDelta int64) *Gauge {
	g := &Gauge{
		name:         name,
		oncePerDur:   NewOncePerDur(oncePerDur),
		oncePerDelta: NewOncePerDelta(oncePerDelta),
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
	g.m.edge.Pub(metricsSubject, []byte(g.StringWithVal(v)))
}

// Add adds a value to the gauge.
//
// Safe to call from multiple goroutines.
func (g *Gauge) Add(n int64) {
	if n == 0 {
		return
	}
	val := atomic.AddInt64(&g.val, n)
	g.oncePerDur.Do(func() {
		g.oncePerDelta.Do(val, func() {
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
	sent,
	received *Gauge
}

func (mc *metricsConn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	mc.sent.Add(int64(n))
	return
}

func (mc *metricsConn) Read(b []byte) (n int, err error) {
	n, err = mc.Conn.Read(b)
	mc.received.Add(int64(n))
	return
}

type metricsListener struct {
	net.Listener
	sent,
	received *Gauge
}

func (mln *metricsListener) Accept() (net.Conn, error) {
	conn, err := mln.Listener.Accept()

	return &metricsConn{
		Conn:     conn,
		sent:     mln.sent,
		received: mln.received,
	}, err
}

func SentReceivedMiddleware(sent, received *Gauge, ln net.Listener) net.Listener {
	return &metricsListener{
		Listener: ln,
		sent:     sent,
		received: received,
	}
}
