package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/yamux"
	"golang.org/x/crypto/acme/autocert"

	"proxyy/internal/protocol"
)

type Config struct {
	ControlAddr string
	HTTPAddr    string
	HTTPSAddr   string // empty disables HTTPS
	Domain      string
	TCPPortMin  int
	TCPPortMax  int
	AuthToken   string

	// Used only when HTTPSAddr is set.
	CertCacheDir string
	ACMEEmail    string
}

type tunnel struct {
	id        string
	kind      string
	subdomain string
	tcpPort   int
	session   *yamux.Session
}

type Server struct {
	cfg Config

	mu         sync.RWMutex
	bySub      map[string]*tunnel
	byTCPPort  map[int]*tunnel
	nextTCPPort int
}

func New(cfg Config) *Server {
	return &Server{
		cfg:         cfg,
		bySub:       make(map[string]*tunnel),
		byTCPPort:   make(map[int]*tunnel),
		nextTCPPort: cfg.TCPPortMin,
	}
}

func (s *Server) Run() error {
	httpHandler := http.HandlerFunc(s.tunnelHTTP)

	if s.cfg.HTTPSAddr != "" {
		mgr := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(s.cfg.CertCacheDir),
			HostPolicy: s.hostPolicy,
			Email:      s.cfg.ACMEEmail,
		}
		// autocert.HTTPHandler handles /.well-known/acme-challenge/* itself
		// and delegates everything else to our tunnel router.
		go s.runHTTP(mgr.HTTPHandler(httpHandler))
		go s.runHTTPS(mgr)
	} else {
		go s.runHTTP(httpHandler)
	}
	return s.runControl()
}

// hostPolicy restricts which subdomains autocert will provision certificates
// for. We only allow the configured base domain and its subdomains, so a
// random stranger can't trick our server into requesting a cert for some
// unrelated host.
func (s *Server) hostPolicy(_ context.Context, host string) error {
	host = strings.ToLower(host)
	domain := strings.ToLower(s.cfg.Domain)
	if host == domain || strings.HasSuffix(host, "."+domain) {
		return nil
	}
	return fmt.Errorf("acme: host %q not allowed", host)
}

func (s *Server) runControl() error {
	l, err := net.Listen("tcp", s.cfg.ControlAddr)
	if err != nil {
		return fmt.Errorf("control listen: %w", err)
	}
	log.Printf("control listener on %s", s.cfg.ControlAddr)
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handleControl(c)
	}
}

func (s *Server) handleControl(c net.Conn) {
	defer c.Close()
	sess, err := yamux.Server(c, nil)
	if err != nil {
		log.Printf("yamux server: %v", err)
		return
	}
	defer sess.Close()

	ctrl, err := sess.AcceptStream()
	if err != nil {
		log.Printf("accept control stream: %v", err)
		return
	}
	defer ctrl.Close()

	var req protocol.RegisterReq
	if err := protocol.ReadJSON(ctrl, &req); err != nil {
		log.Printf("read register: %v", err)
		return
	}

	if s.cfg.AuthToken != "" && req.AuthToken != s.cfg.AuthToken {
		_ = protocol.WriteJSON(ctrl, protocol.RegisterResp{Error: "invalid auth token"})
		return
	}

	t, resp := s.register(&req, sess)
	if err := protocol.WriteJSON(ctrl, resp); err != nil {
		log.Printf("write register resp: %v", err)
		return
	}
	if t == nil {
		return
	}
	defer s.unregister(t)

	log.Printf("tunnel up: %s -> %s (kind=%s)", resp.PublicURL, c.RemoteAddr(), t.kind)

	if t.kind == protocol.TunnelTCP {
		go s.acceptTCP(t)
	}

	<-sess.CloseChan()
	log.Printf("tunnel down: %s", resp.PublicURL)
}

func (s *Server) register(req *protocol.RegisterReq, sess *yamux.Session) (*tunnel, protocol.RegisterResp) {
	t := &tunnel{kind: req.Type, session: sess, id: randID()}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch req.Type {
	case protocol.TunnelHTTP:
		sub := req.Subdomain
		if sub == "" {
			sub = randID()
		}
		if _, ok := s.bySub[sub]; ok {
			return nil, protocol.RegisterResp{Error: "subdomain in use: " + sub}
		}
		t.subdomain = sub
		s.bySub[sub] = t
		return t, protocol.RegisterResp{OK: true, PublicURL: fmt.Sprintf("http://%s.%s", sub, s.cfg.Domain)}

	case protocol.TunnelTCP:
		port, err := s.allocTCPPortLocked()
		if err != nil {
			return nil, protocol.RegisterResp{Error: err.Error()}
		}
		t.tcpPort = port
		s.byTCPPort[port] = t
		return t, protocol.RegisterResp{OK: true, PublicURL: fmt.Sprintf("tcp://%s:%d", s.cfg.Domain, port)}

	default:
		return nil, protocol.RegisterResp{Error: "unknown tunnel type: " + req.Type}
	}
}

func (s *Server) unregister(t *tunnel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.subdomain != "" {
		delete(s.bySub, t.subdomain)
	}
	if t.tcpPort != 0 {
		delete(s.byTCPPort, t.tcpPort)
	}
}

func (s *Server) allocTCPPortLocked() (int, error) {
	for i := 0; i <= s.cfg.TCPPortMax-s.cfg.TCPPortMin; i++ {
		p := s.nextTCPPort
		s.nextTCPPort++
		if s.nextTCPPort > s.cfg.TCPPortMax {
			s.nextTCPPort = s.cfg.TCPPortMin
		}
		if _, taken := s.byTCPPort[p]; taken {
			continue
		}
		l, err := net.Listen("tcp", ":"+strconv.Itoa(p))
		if err != nil {
			continue
		}
		l.Close()
		return p, nil
	}
	return 0, fmt.Errorf("no free tcp port in range")
}

func (s *Server) acceptTCP(t *tunnel) {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(t.tcpPort))
	if err != nil {
		log.Printf("tcp listen :%d: %v", t.tcpPort, err)
		return
	}
	defer l.Close()

	go func() {
		<-t.session.CloseChan()
		l.Close()
	}()

	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go s.pipeToTunnel(t, c)
	}
}

func (s *Server) pipeToTunnel(t *tunnel, public net.Conn) {
	defer public.Close()
	stream, err := t.session.OpenStream()
	if err != nil {
		log.Printf("open stream: %v", err)
		return
	}
	defer stream.Close()
	if err := protocol.WriteJSON(stream, protocol.StreamOpen{RemoteAddr: public.RemoteAddr().String()}); err != nil {
		return
	}
	pipe(public, stream)
}

type closeWriter interface{ CloseWrite() error }

func halfClose(c net.Conn) {
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); halfClose(a); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); halfClose(b); done <- struct{}{} }()
	<-done
	<-done
}

func randID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
