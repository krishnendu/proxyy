package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"proxyy/internal/client"
	"proxyy/internal/protocol"
)

func usage() {
	fmt.Fprintln(os.Stderr, `proxyy - reverse tunnel client

Usage:
  proxyy http <local-port-or-addr> [--subdomain name] [flags]
  proxyy tcp  <local-port-or-addr> [flags]

Examples:
  proxyy http 3000
  proxyy http 3000 --subdomain myapp
  proxyy tcp 22

Flags:`)
	flag.PrintDefaults()
}

func main() {
	server := flag.String("server", envOr("TUNNEL_SERVER", "localhost:7000"), "tunnel server address (host:port)")
	subdomain := flag.String("subdomain", "", "requested subdomain (http only, random if blank)")
	auth := flag.String("auth", os.Getenv("TUNNEL_AUTH_TOKEN"), "auth token")

	flag.Usage = usage

	// Pull positional args before flag.Parse so subcommand + addr come first.
	args := os.Args[1:]
	if len(args) < 2 {
		usage()
		os.Exit(1)
	}
	kind := args[0]
	local := normalizeLocal(args[1])
	os.Args = append([]string{os.Args[0]}, args[2:]...)
	flag.Parse()

	switch kind {
	case protocol.TunnelHTTP, protocol.TunnelTCP:
	default:
		fmt.Fprintf(os.Stderr, "unknown tunnel type: %s\n\n", kind)
		usage()
		os.Exit(1)
	}

	if err := client.Run(client.Config{
		ServerAddr: *server,
		Type:       kind,
		LocalAddr:  local,
		Subdomain:  *subdomain,
		AuthToken:  *auth,
	}); err != nil {
		log.Fatal(err)
	}
}

func normalizeLocal(s string) string {
	// "3000" -> "127.0.0.1:3000"; "host:port" left as-is.
	for _, ch := range s {
		if ch == ':' {
			return s
		}
	}
	return "127.0.0.1:" + s
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
