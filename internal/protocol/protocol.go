package protocol

import (
	"encoding/json"
	"fmt"
	"io"
)

const (
	ControlPort = 7000

	TunnelHTTP = "http"
	TunnelTCP  = "tcp"
)

type RegisterReq struct {
	Type      string `json:"type"`
	Subdomain string `json:"subdomain,omitempty"`
	LocalAddr string `json:"local_addr"`
	AuthToken string `json:"auth_token,omitempty"`
}

type RegisterResp struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	PublicURL string `json:"public_url,omitempty"`
}

type StreamOpen struct {
	RemoteAddr string `json:"remote_addr"`
}

func WriteJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("message too large")
	}
	var hdr [4]byte
	hdr[0] = byte(len(b) >> 24)
	hdr[1] = byte(len(b) >> 16)
	hdr[2] = byte(len(b) >> 8)
	hdr[3] = byte(len(b))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func ReadJSON(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := int(hdr[0])<<24 | int(hdr[1])<<16 | int(hdr[2])<<8 | int(hdr[3])
	if n <= 0 || n > 1<<20 {
		return fmt.Errorf("invalid message length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, v)
}
