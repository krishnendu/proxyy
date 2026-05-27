package client

import (
	"fmt"
	"io"
	"log"
	"net"

	"github.com/hashicorp/yamux"

	"proxyy/internal/protocol"
)

type Config struct {
	ServerAddr string
	Type       string
	LocalAddr  string
	Subdomain  string
	AuthToken  string
}

func Run(cfg Config) error {
	conn, err := net.Dial("tcp", cfg.ServerAddr)
	if err != nil {
		return fmt.Errorf("dial server: %w", err)
	}
	defer conn.Close()

	sess, err := yamux.Client(conn, nil)
	if err != nil {
		return fmt.Errorf("yamux client: %w", err)
	}
	defer sess.Close()

	ctrl, err := sess.OpenStream()
	if err != nil {
		return fmt.Errorf("open control stream: %w", err)
	}

	if err := protocol.WriteJSON(ctrl, protocol.RegisterReq{
		Type:      cfg.Type,
		Subdomain: cfg.Subdomain,
		LocalAddr: cfg.LocalAddr,
		AuthToken: cfg.AuthToken,
	}); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	var resp protocol.RegisterResp
	if err := protocol.ReadJSON(ctrl, &resp); err != nil {
		return fmt.Errorf("read register resp: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("server rejected: %s", resp.Error)
	}

	log.Printf("tunnel established: %s -> %s", resp.PublicURL, cfg.LocalAddr)
	fmt.Printf("\n  Forwarding  %s -> %s\n\n", resp.PublicURL, cfg.LocalAddr)

	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return fmt.Errorf("session closed: %w", err)
		}
		go handleStream(stream, cfg.LocalAddr)
	}
}

func handleStream(stream net.Conn, localAddr string) {
	defer stream.Close()

	var open protocol.StreamOpen
	if err := protocol.ReadJSON(stream, &open); err != nil {
		return
	}

	local, err := net.Dial("tcp", localAddr)
	if err != nil {
		log.Printf("dial local %s: %v", localAddr, err)
		return
	}
	defer local.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(local, stream); halfClose(local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(stream, local); halfClose(stream); done <- struct{}{} }()
	<-done
	<-done
}

type closeWriter interface{ CloseWrite() error }

func halfClose(c net.Conn) {
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}
