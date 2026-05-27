# proxyy

> Self-hosted reverse tunnels. Like ngrok, but you run the server.

Expose any local TCP service — HTTP, HTTPS, SSH, or anything else — at a
public address on your own VPS, over a single outbound connection. HTTPS
certs are auto-provisioned by Let's Encrypt on first hit. Runs comfortably
on the free tier of every major cloud provider.

```
  laptop                       your VPS                                public internet
  ┌────────────────┐           ┌──────────────────────────────────┐    ┌──────────────┐
  │ proxyy (client) │ ──TCP──> │ :7000  control + yamux mux       │ <─ │ browser /    │
  │  forwards to    │          │ :80    HTTP routing + ACME       │    │ curl / ssh / │
  │  127.0.0.1:N    │          │ :443   HTTPS termination (LE)    │    │ webhook      │
  └────────────────┘           │ :10000-11000  raw TCP ports      │    └──────────────┘
                               └──────────────────────────────────┘
```

## Status

v0.2 is shipped and running on `tun.proxyy.in`. See [`ROADMAP.md`](./ROADMAP.md)
for what's planned next.

## How it works

1. The client dials the server's control port and opens a [yamux](https://github.com/hashicorp/yamux) session.
2. It registers a tunnel: `http` (gets a subdomain) or `tcp` (gets a port).
3. The server listens publicly. For each incoming connection it opens a new
   yamux stream over the tunnel; the client dials the local backend and pipes
   bytes in both directions.
4. For HTTPS, the server terminates TLS using certs auto-issued by Let's
   Encrypt on first request to each subdomain.

## Quick start (local, no server needed)

```bash
git clone https://github.com/krishnendu/proxyy && cd proxyy
go build -o bin/proxyy-server ./cmd/server
go build -o bin/proxyy        ./cmd/client

# Terminal 1 — start the server. localtest.me is a public DNS trick that
# resolves *.localtest.me to 127.0.0.1, so you don't need a real domain.
./bin/proxyy-server --control :7000 --http :8080 --domain localtest.me

# Terminal 2 — expose a local service
python3 -m http.server 3000
./bin/proxyy http 3000 --server localhost:7000 --subdomain demo

# Then hit it
curl http://demo.localtest.me:8080/
```

For raw TCP: `./bin/proxyy tcp 22 --server localhost:7000` — the server
assigns a port and prints the address.

## Deploy to your own VPS

The repo has everything you need under [`deploy/`](./deploy/): a systemd
unit, an `.env` template, and a one-shot installer.

**On your laptop:**

```bash
# Cross-compile for the VPS architecture (amd64 here; use arm64 for ARM)
GOOS=linux GOARCH=amd64 go build -o bin/proxyy-server-linux-amd64 ./cmd/server

scp -i ~/.ssh/<key> bin/proxyy-server-linux-amd64 ubuntu@<VPS_IP>:~/proxyy-server
scp -i ~/.ssh/<key> -r deploy ubuntu@<VPS_IP>:~/
```

**On the VPS:**

```bash
chmod +x ~/proxyy-server
sudo BINARY=~/proxyy-server bash ~/deploy/install.sh

# Set TUNNEL_AUTH_TOKEN and TUNNEL_ACME_EMAIL
sudo nano /etc/proxyy/proxyy.env

sudo systemctl enable --now proxyy
journalctl -u proxyy -f
```

Two more pieces of setup, both one-time:

**1. DNS** — point your wildcard subdomain at the VPS:

| Type | Name    | Value      |
|------|---------|------------|
| `A`  | `tun`   | `<VPS_IP>` |
| `A`  | `*.tun` | `<VPS_IP>` |

**2. Firewall** — open inbound ports `80`, `443`, `7000`, `10000-11000` in
both your cloud provider's firewall (Security List / NSG / Security Group)
**and** the VM's iptables (Oracle Cloud and AWS images ship with a default
REJECT rule that traps people):

```bash
sudo iptables -I INPUT 5 -p tcp -m multiport --dports 80,443,7000,10000:11000 -j ACCEPT
sudo netfilter-persistent save
```

Verify with `sudo iptables -L INPUT -n --line-numbers` — the ACCEPT line
must appear **above** the REJECT line.

## Client usage

```bash
export TUNNEL_SERVER=tun.proxyy.in:7000
export TUNNEL_AUTH_TOKEN=<token from /etc/proxyy/proxyy.env>
export TUNNEL_TLS=true   # the production server has --control-tls enabled

proxyy http 3000 --subdomain myapp     # http(s)://myapp.tun.proxyy.in
proxyy tcp 22                          # tcp://tun.proxyy.in:<assigned port>
```

The control channel is encrypted with TLS (the same Let's Encrypt cert that
serves your HTTPS subdomains). For local development against a plain server,
omit `TUNNEL_TLS` or pass `--tls=false`.

First HTTPS request to a new subdomain takes ~5–10 s while autocert
provisions a Let's Encrypt cert. Subsequent requests are instant. Certs are
cached on the VPS at `/var/lib/proxyy/certs/` and auto-renew before expiry.

## Project layout

```
cmd/server/         tunnel daemon (runs on the VPS)
cmd/client/         tunnel CLI (runs on the laptop)
internal/protocol/  wire protocol — register req/resp, stream-open header
internal/server/    accept loop, HTTP/HTTPS routing, TCP port allocator
internal/client/    dial server, accept streams, forward to local addr
deploy/             systemd unit, env template, install script
```

## License

[MIT](./LICENSE) © Krishnendu Chatterjee.
