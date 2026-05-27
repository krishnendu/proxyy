package main

import (
	"flag"
	"log"
	"os"

	"proxyy/internal/server"
)

func main() {
	controlAddr := flag.String("control", ":7000", "control listener address")
	httpAddr := flag.String("http", ":80", "public http listener address")
	httpsAddr := flag.String("https", envOr("TUNNEL_HTTPS_ADDR", ""), "public https listener address (e.g. :443); empty disables HTTPS")
	domain := flag.String("domain", envOr("TUNNEL_DOMAIN", "tun.proxyy.in"), "base domain for http tunnels (subdomains route here)")
	tcpMin := flag.Int("tcp-min", 10000, "min port for tcp tunnels")
	tcpMax := flag.Int("tcp-max", 11000, "max port for tcp tunnels")
	auth := flag.String("auth", os.Getenv("TUNNEL_AUTH_TOKEN"), "shared auth token (empty = no auth)")
	certCache := flag.String("cert-cache", envOr("TUNNEL_CERT_CACHE", "/var/lib/proxyy/certs"), "directory for Let's Encrypt cert cache (used when --https is set)")
	acmeEmail := flag.String("acme-email", os.Getenv("TUNNEL_ACME_EMAIL"), "email registered with Let's Encrypt for cert renewal notices")
	flag.Parse()

	srv := server.New(server.Config{
		ControlAddr:  *controlAddr,
		HTTPAddr:     *httpAddr,
		HTTPSAddr:    *httpsAddr,
		Domain:       *domain,
		TCPPortMin:   *tcpMin,
		TCPPortMax:   *tcpMax,
		AuthToken:    *auth,
		CertCacheDir: *certCache,
		ACMEEmail:    *acmeEmail,
	})
	log.Printf("proxyy server starting (domain=%s tcp=%d-%d https=%q)", *domain, *tcpMin, *tcpMax, *httpsAddr)
	if err := srv.Run(); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
