# Minecraft Bedrock Broadcast Relay

A lightweight relay that lets Minecraft Bedrock Edition servers running in isolated environments (Kubernetes clusters, Docker networks, remote VMs) show up in the **"Friends / LAN Games"** tab of local Bedrock clients (Xbox, PlayStation, Nintendo Switch, iOS, Android, Windows).

Bedrock clients find LAN games by **broadcast**, which does not cross subnets or reach pods. A relay on the clients' LAN answers the broadcast on behalf of the real server.

This relay supports the **NetherNet** transport (WebRTC), the default for Bedrock dedicated servers since 26.50. RakNet was removed in 26.60, and an earlier RakNet relay that lived in this repository was removed because it was never verified to work.

---

## How it works

NetherNet LAN discovery and signaling both run over UDP port `7551`. The actual game data then flows over a WebRTC connection (ICE/DTLS/SCTP) **directly between the client and the server**. The relay never carries game traffic.

```
  LAN                                                         Remote network / Kubernetes
  [Bedrock Client] --1. broadcast request (7551)--> [Relay] --2. unicast request (7551)--> [Server / LB]
  [Bedrock Client] <--3. cached response ---------- [Relay] <-- response (polled, cached)--
  [Bedrock Client] <==4. signaling (offer/answer/ICE) via Relay <==> Server (7551) ==>
  [Bedrock Client] <================ 5. WebRTC game traffic (direct UDP) ================> [Server / LB]
```

1. **Poller:** the relay periodically sends a unicast discovery request to the backend (`-remote`, port `7551`) and caches the response (server name, player count, version, nonce, ...). The cached data is relayed verbatim.
2. **Responder:** when a client broadcasts a discovery request on the LAN, the relay replies from its own address, using the **backend's network ID** so the client addresses later signaling to the ID it discovered. It only answers while the backend has answered recently (`-ttl`).
3. **Signaling forwarder:** when a client joins, its WebRTC offer and ICE candidates (`CONNECTREQUEST`, `CANDIDATEADD`) are forwarded to the backend. The backend's answer is routed back to the right client by network ID.
4. **ICE candidate rewriting (optional):** with `-public-ip`, the IP in ICE candidates sent by the backend is replaced (see below).

Discovery packets are encrypted (AES-ECB, key = SHA-256 of `0xdeadbeef`) and carry an HMAC-SHA256 checksum. The relay implements this codec itself; the `ServerData` payload is never modified.

## Build

Prerequisites: Go 1.23 or newer.

```bash
git clone https://github.com/jsandas/minecraft-bedrock-broadcast-relay.git
cd minecraft-bedrock-broadcast-relay
make build            # builds ./nethernet_relay
# or directly:
go build -o nethernet_relay ./cmd/nethernet_relay
```

## Usage

```bash
./nethernet_relay -remote 10.4.10.113:7551
```

| Flag | Env Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `-remote` | `NN_REMOTE_ADDR` | `10.4.10.222:7551` | Backend discovery address (server or load balancer `IP:7551`) |
| `-listen` | `NN_LISTEN_ADDR` | `0.0.0.0:7551` | Local UDP address that receives LAN broadcasts |
| `-ping-interval` | — | `5s` | How often the backend is queried |
| `-ttl` | — | `15s` | Stop answering discovery if the backend has been silent this long |
| `-public-ip` | `NN_PUBLIC_IP` | *(empty)* | Rewrite ICE candidate IPs from the backend to this address |

## Ports

| Port | Where | Purpose |
| :--- | :--- | :--- |
| UDP `7551` | Relay host (inbound from the LAN) | Discovery and signaling from clients |
| UDP `7551` | Server or load balancer | Discovery and signaling toward the backend |
| UDP game port (default `19132`) | Server or load balancer | WebRTC game traffic from clients. Set with `server-udp-ports` in `server.properties`. |

The server must publish the game port **with the same number on the host (or load balancer) and in the container**, because only the IP of ICE candidates is rewritten, never the port. A single port is enough. For example `server-udp-ports=19132:19132` served two clients at the same time in testing.

## When do you need `-public-ip`?

The server advertises its own address as an ICE candidate, which is usually a container or pod address the client cannot reach. A connection still succeeds when the **server can open the connection to the client** (the server is the active ICE side), which is the normal case on a flat network.

| Situation | `-public-ip` |
| :--- | :--- |
| Server can reach the client's LAN address (flat network, permissive egress) | Not needed |
| Server cannot initiate to the client (firewall, no route back to the client's subnet, different network) | **Required**: set it to the address where the server is reachable, for example the **load balancer IP** |

`-public-ip` must be the address of the **server side**, never the relay's own host (unless the server runs on the same machine).

This was verified by blocking container-originated UDP to the client with an iptables rule in `DOCKER-USER` (`-m conntrack --ctstate NEW -j DROP`): without `-public-ip` the join failed, with `-public-ip <server IP>` it worked.

## Running with Docker

The relay must receive LAN broadcasts, so it needs host networking on a machine on the clients' LAN:

```bash
docker build -t nethernet-relay .
docker run -d --name nethernet-relay --restart unless-stopped --net=host \
  -e NN_REMOTE_ADDR="10.4.10.113:7551" \
  nethernet-relay
```

Example `Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /relay ./cmd/nethernet_relay

FROM alpine:latest
COPY --from=builder /relay /relay
ENTRYPOINT ["/relay"]
```

## Server container (example)

[`docker-compose.yaml`](docker-compose.yaml) runs a Bedrock server that publishes the ports the NetherNet relay needs (`19132/udp` for WebRTC, `7551/udp` for discovery). Do not run it on the same host as the relay with the same `7551` mapping (the relay binds `7551` itself); publish it as `17551:7551/udp` or run it on another machine.

## Current limitations

* Tested against a Bedrock dedicated server (1.26.x) in Docker on a separate VM with two simultaneous clients. Not yet tested against a Kubernetes load balancer, where NAT and source-IP handling may differ.
* The discovery response is cached for up to `-ping-interval`, so a server that rotates its nonce may briefly advertise a stale one.
* ICE candidate rewriting changes the IP only. The game port must match end to end.
* The NetherNet protocol is reverse engineered and may change. See [df-mc/nethernet-spec](https://github.com/df-mc/nethernet-spec) and [df-mc/go-nethernet](https://github.com/df-mc/go-nethernet).

---

## Development

```bash
make test-unit     # go test -v ./...
make quality       # gofmt check, go mod tidy check, golangci-lint
make help          # all targets
```
