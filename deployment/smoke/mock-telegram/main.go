// Command mock-telegram serves the kind stack a Telegram Bot API for any
// number of bots, so the Portal browser suite can drive the chat gateway and
// Space Assistant bots end to end without Telegram.
//
// It is a packaging of internal/testsupport/mocktelegram, whose unit tests
// drive it with the real adapter. See docs/deploy/local-kind.md.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/icloudbb/buildmax/internal/testsupport/mocktelegram"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		healthcheck()
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/", mocktelegram.New())
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func healthcheck() {
	resp, err := http.Get("http://127.0.0.1:8080/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	_ = resp.Body.Close()
}
