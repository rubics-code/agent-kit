// Command proxy is a small HTTP CONNECT proxy for the HTTPS_PROXY test. It
// logs each tunnel host and copies bytes. It does not see the TLS content.
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8899", "listen address")
	flag.Parse()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		log.Printf("CONNECT %s", r.Host)
		up, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			up.Close()
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		down, _, err := hj.Hijack()
		if err != nil {
			up.Close()
			return
		}
		go func() { io.Copy(up, down); up.Close() }()
		io.Copy(down, up)
		down.Close()
	})
	log.Printf("proxy on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, h))
}
