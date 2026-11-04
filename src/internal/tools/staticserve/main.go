// Command staticserve serves a directory over HTTP on a loopback address.
//
// The web UI accessibility job uses it as the fixture's upstream, the
// version_check_url its pack config points at:
//
//	go -C src build -o "$work/staticserve" ./internal/tools/staticserve
//	"$work/staticserve" -addr 127.0.0.1:8765 -dir src/internal/web/testdata/a11y/upstream
//
// It refuses a non-loopback address: it is test scaffolding, not a server.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "loopback host:port to listen on")
	dir := flag.String("dir", ".", "directory to serve")
	flag.Parse()

	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "staticserve: %v\n", err)
		os.Exit(2)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		fmt.Fprintf(os.Stderr, "staticserve: %s is not a loopback address\n", host)
		os.Exit(2)
	}
	if info, err := os.Stat(*dir); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "staticserve: %s is not a directory\n", *dir)
		os.Exit(2)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(http.FileServer(http.Dir(*dir))),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("serving %s on http://%s", *dir, *addr)
	log.Fatal(srv.ListenAndServe())
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
