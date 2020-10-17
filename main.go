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
		"server": "fly-psyche",
	})

	mtr := metrics.New(node.NewEdge())
	defer mtr.Close()

	mux := http.NewServeMux()

	mux.Handle("/", logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("here meet psychic beams"))
	})))

	mux.Handle("/psyche", metrics.InFlightMiddleware(
		mtr.NewGauge("requests_in_flight", 0),
		logRequest(psyche.NewWebsocketHandler(node)),
	))

	log.Println("listening on :8080")

	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}

	if err := http.Serve(metrics.SentReceivedMiddleware(
		mtr.NewGauge("bytes_sent", 2*time.Second),
		mtr.NewGauge("bytes_received", 2*time.Second),
		ln,
	), mux); err != nil {
		panic(err)
	}
}
