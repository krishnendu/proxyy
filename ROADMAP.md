# proxyy roadmap

Tracks what's shipped and what's planned. Each item carries a rough size
(**S** = a few hours, **M** = a day, **L** = a week+) and a priority based on
how often it shows up as a real-world need.

---

## Shipped

### v0.1 — local tunneling works
- HTTP tunneling with subdomain routing (`Host`-header based, `*.tun.proxyy.in`)
- Raw TCP tunneling with dynamic port allocation in a configurable range
- Single outbound yamux-multiplexed control channel per tunnel
- Auth via shared token
- Half-close-aware byte piping in both directions

### v0.2 — production-ready single-host deployment
- HTTPS termination via Let's Encrypt (`golang.org/x/crypto/acme/autocert`),
  per-subdomain certs issued on first request via HTTP-01 challenge
- `http.Server` + `http.Hijacker` based HTTP routing (clean handler model,
  works for plain HTTP and TLS-terminated HTTPS through the same code path)
- systemd unit with `CAP_NET_BIND_SERVICE`, restart-on-failure, hardening
  (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, etc.)
- Installer script (`deploy/install.sh`) that creates a dedicated `proxyy`
  system user, drops the binary in `/usr/local/bin`, lays out `/etc/proxyy/`
  and `/var/lib/proxyy/certs/`, reloads systemd

---

## Next up — v0.3

### TLS on the control channel **[M, high]**

The laptop ↔ VPS control channel is currently plain TCP. The auth token gates
registration, but anyone on the path could observe tunneled bytes. Wrap the
control listener in `crypto/tls`, reuse the autocert manager for the cert,
and update the client to dial TLS. The client config will accept a server-name
override so users behind weird NATs can still verify the cert.

### Live TCP smoke test on the deployed VPS **[S, high]**

HTTP was smoke-tested on the live VM. TCP code path is identical and tested
locally, but we should expose a real local service (e.g. `proxyy tcp 22` →
SSH from a friend's machine) on the production VM to confirm.

### Health endpoint + structured logging **[S, medium]**

`GET /healthz` on a non-public port (or under a magic path) so monitoring
can hit it. Switch to `log/slog` with JSON output so journald aggregates
cleanly.

---

## Mid term — v0.4

### Wildcard TLS via DNS-01 challenge **[L, medium]**

HTTP-01 issues one cert per subdomain, with a ~5–10 s delay on the first
hit. A single wildcard cert (`*.tun.proxyy.in`) would make every new
subdomain instant. Requires DNS provider API integration; easiest to start
with Cloudflare (most popular).

### UDP tunneling **[L, medium]**

Real demand: game servers, WireGuard, QUIC dev. UDP is connectionless, so
the server has to synthesize "sessions" by 5-tuple, time them out, and
multiplex them over yamux streams. Most home NATs drop idle UDP flows, which
also complicates keep-alive design.

### Per-tunnel rate limits and bandwidth caps **[M, low]**

Token-bucket per session, configurable defaults on the server, per-tenant
overrides once we have a tenant model. Prevents one noisy tunnel from
starving the others.

---

## Long term — v1.x

### Multi-tenant model **[L]**

Tokens become per-user (issued by a CLI command on the server), and tunnels
are namespaced under a username (`<user>-<sub>.tun.proxyy.in`). Lets us run
this as a small public service for friends without one user squatting all
the good subdomains.

### Web dashboard **[L]**

Read-only at first: list live tunnels, recent traffic counts, cert status.
Behind basic auth on a separate port. Eventually: a button to issue a new
client token.

### Replace yamux with QUIC **[L, exploratory]**

QUIC's stream multiplexing + 0-RTT reconnect + congestion control would
upgrade us from "good enough" to "best in class" for tunnel latency over
flaky networks. Significant rewrite of the control protocol.

### Connection inspector **[M, exploratory]**

Like ngrok's web UI but minimal: capture last N HTTP requests/responses per
tunnel and let you replay them. Useful for webhook debugging.

---

## Won't do (deliberately)

- **Hosted SaaS version.** This stays as something you self-host.
- **Windows server support.** Linux + systemd only. The client cross-compiles
  to Windows fine.
- **HTTP/2 between curl and the server.** We use `http.Hijacker`, which is
  HTTP/1.1-only. The trade-off is that hijacking lets us tunnel arbitrary
  byte streams (websockets, anything else); HTTP/2 doesn't allow that.
  Clients still get HTTP/2 between themselves and us if we add it later via
  ALPN, but it's not on the path.
