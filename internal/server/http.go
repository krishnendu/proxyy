package server

import (
	"crypto/tls"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"proxyy/internal/protocol"
)

func (s *Server) runHTTP(handler http.Handler) {
	srv := &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Long timeouts because tunneled connections can be long-lived
		// (websockets, SSE, etc. — though hijack bypasses these anyway).
		WriteTimeout: 0,
		IdleTimeout:  0,
	}
	log.Printf("http listener on %s (domain=%s)", s.cfg.HTTPAddr, s.cfg.Domain)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("http listen: %v", err)
	}
}

func (s *Server) runHTTPS(mgr *autocert.Manager) {
	tlsCfg := &tls.Config{
		GetCertificate: mgr.GetCertificate,
		// Hijack doesn't work well over HTTP/2, so advertise only HTTP/1.1.
		NextProtos: []string{"http/1.1"},
		MinVersion: tls.VersionTLS12,
	}
	ln, err := tls.Listen("tcp", s.cfg.HTTPSAddr, tlsCfg)
	if err != nil {
		log.Fatalf("https listen: %v", err)
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(s.tunnelHTTP),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("https listener on %s (autocert cache=%s)", s.cfg.HTTPSAddr, s.cfg.CertCacheDir)
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("https serve: %v", err)
	}
}

// tunnelHTTP routes one HTTP request over the matching tunnel's yamux
// session. It hijacks the underlying connection so we can stream raw bytes
// in both directions (including websockets).
func (s *Server) tunnelHTTP(w http.ResponseWriter, r *http.Request) {
	sub := s.subdomainFromHost(r.Host)
	s.mu.RLock()
	t := s.bySub[sub]
	s.mu.RUnlock()

	if t == nil {
		http.Error(w, "tunnel not found: "+sub, http.StatusNotFound)
		return
	}

	stream, err := t.session.OpenStream()
	if err != nil {
		http.Error(w, "tunnel offline", http.StatusBadGateway)
		return
	}
	defer stream.Close()

	if err := protocol.WriteJSON(stream, protocol.StreamOpen{RemoteAddr: r.RemoteAddr}); err != nil {
		return
	}

	// Send the request to the client (it'll forward to the local backend).
	// http.Request.Write serializes in wire format, body and all.
	if err := r.Write(stream); err != nil {
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacker unsupported", http.StatusInternalServerError)
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	// Drain anything the http server already buffered from the client, in
	// case the wire-formatted request hasn't been fully sent yet — but
	// r.Write above already consumed r.Body, so brw.Reader should be empty.
	if n := brw.Reader.Buffered(); n > 0 {
		buf := make([]byte, n)
		_, _ = brw.Reader.Read(buf)
		_, _ = stream.Write(buf)
	}

	// Stream the response (and any subsequent request/response bytes for a
	// hijacked protocol like websockets) bidirectionally.
	pipe(conn, stream)
}

func (s *Server) subdomainFromHost(host string) string {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	suffix := "." + strings.ToLower(s.cfg.Domain)
	if !strings.HasSuffix(host, suffix) {
		return ""
	}
	return host[:len(host)-len(suffix)]
}

