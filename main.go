package main

import (
	"log"
	"net"
	"net/http"
	"time"

	"github.com/Gaboose/fly-psyche/metrics"
	"github.com/abstract-p2p/go-psyche"
)

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("HTTP URL=%s RemoteAddr=%s ", r.URL.String(), r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func main() {
	node := psyche.NewNode(map[string]interface{}{
		"gateway": []interface{}{
			map[string]interface{}{
				"name":    "fly-psyche",
				"version": "0.0.1",
			},
		},
	})

	mtr := metrics.New()
	defer mtr.Close()

	mux := http.NewServeMux()

	mux.Handle("/", logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("here meet psychic beams"))
	})))

	wsHandler := psyche.NewWebsocketHandler(node)

	mux.Handle("/psyche", metrics.InFlightMiddleware(
		mtr.NewGauge("requests_in_flight", 0, 0, ""),
		logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := wsHandler.Accept(w, r)

			// Publish metrics on the connection's local node
			edge := c.LocalNode().NewEdge()
			defer edge.Close()
			mtr.Attach(edge, c.RemoteAddr())

			c.Serve()
		})),
	))

	log.Println("listening on :8080")

	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}

	if err := http.Serve(metrics.SentReceivedMiddleware(
		// Server-wide metrics
		mtr.NewGauge("bytes_sent", time.Second, 128, ""),
		mtr.NewGauge("bytes_received", time.Second, 0, ""),

		// Metrics specific to each connection
		func(raddr string) (sent, received *metrics.Gauge) {
			sent = mtr.NewGauge("bytes_sent{conn=this}", time.Second, 128, raddr)
			received = mtr.NewGauge("bytes_received{conn=this}", time.Second, 0, raddr)
			return
		},

		ln,
	), mux); err != nil {
		panic(err)
	}
}
