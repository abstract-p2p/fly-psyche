package main

import (
	"log"
	"net/http"

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

	mux := http.NewServeMux()
	mux.Handle("/", logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("here meet psychic beams"))
	})))
	mux.Handle("/psyche", logRequest(psyche.NewWebsocketHandler(node)))

	log.Println("listening on :8080")
	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		panic(err)
	}
}
