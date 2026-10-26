# Minecraft Bedrock Broadcast Relay

A lightweight Layer 4 (UDP) proxy and LAN discovery relay for Minecraft Bedrock Edition servers.

This service allows Bedrock servers running in isolated environments (such as Kubernetes clusters, Docker networks, or remote cloud VMs) to appear automatically in the **"Friends / LAN Games"** tab of local Bedrock clients (Xbox, PlayStation, Nintendo Switch, iOS, Android, and Windows).

---

## The Problem

1. **LAN Discovery is Broadcast-Based:** When Bedrock clients scan for local games, they broadcast a RakNet `Unconnected Ping` UDP packet to `255.255.255.255:19132`.
2. **Kubernetes & Network Isolation:** Kubernetes Services and cloud networks do not route UDP broadcast traffic across subnets or into pods.
3. **Console Limitations:** Consoles (Xbox, PlayStation, Nintendo Switch) cannot manually enter custom server IP addresses or ports without workarounds. They strictly depend on the built-in LAN discovery mechanism.
4. **No IP in RakNet Pong:** The RakNet `Unconnected Pong` response packet contains metadata (MOTD, player count, version, ports), but **does not contain a server IP address**. The client connects directly to the **source IP** of the UDP packet it receives.

---

## How It Works

```
                                      LAN Broadcast Domain
 +----------------------------------------------------------------------------------------+
 |                                                                                        |
 |  [Bedrock Client]                                                                      |
 |  (Xbox / Mobile / PC)                                                                  |
 |         |                                                                              |
 |         | 1. UDP Broadcast Ping (255.255.255.255:19132)                                |
 |         v                                                                              |
 |  [Broadcast Relay] <----+ 2. Replies with Pong (Source IP = Relay LAN IP)              |
 |  (Running on LAN)       |                                                              |
 |         |               |                                                              |
 |         | 3. Client clicks server -> Sends game packets to RelayIP:19132               |
 |         |                                                                              |
 +---------|------------------------------------------------------------------------------+
           |
           | 4. Transparent Layer 4 UDP Forwarding (Multiplexed per client)
           v
 +-----------------------------------+
 |  Kubernetes / Remote Network      |
 |                                   |
 |  [Service LoadBalancer/NodePort]  |
 |  (e.g., 10.4.10.222:19132)        |
 |         |                         |
 |         v                         |
 |  [Bedrock Server Pod]             |
 +-----------------------------------+
```

1. **Status Poller:** The relay periodically pings the backend server (`10.4.10.222:19132`) using RakNet to cache current MOTD, player counts, protocol version, and server GUID. It rewrites the advertised port to match the relay's local port.
2. **Ping Responder:** The relay listens on UDP port `19132` (`0.0.0.0`) on the local network. When clients broadcast an `Unconnected Ping` (`0x01` or `0x02`), the relay immediately replies with the cached `Unconnected Pong` (`0x1c`).
3. **Session Multiplexer:** When the client connects, the relay dynamically allocates a backend UDP socket to the Kubernetes Load Balancer and transparently pipes all game packets back and forth.
4. **Idle Session Reaper:** Inactive sessions are automatically cleaned up after an idle timeout (default `60s`).

---

## Key Features

- **Protocol Version Agnostic:** Operates at Layer 4 (raw UDP forwarding). It does not decrypt or deserialize game packets, meaning **it will never break when Minecraft Bedrock updates**.
- **Full Console Support:** Enables Xbox, PlayStation, and Nintendo Switch players to join private dedicated servers without DNS hacks or custom mods.
- **End-to-End Encryption & Auth:** Xbox Live authentication and encryption keys are negotiated directly between the client and the real server through the tunnel.
- **Ultra Lightweight:** Written in Go with minimal resource usage (~0% CPU, few MB of RAM).

---

## Installation & Building

### Prerequisites
* Go 1.23 or newer

### Build from source
```bash
git clone https://github.com/jsandas/minecraft-bedrock-broadcast-relay.git
cd minecraft-bedrock-broadcast-relay
go build -o minecraft-bedrock-broadcast-relay .
```

---

## Usage

### Command Line Flags

```bash
./minecraft-bedrock-broadcast-relay \
  -remote 172.18.0.2:19132 \
  -listen 0.0.0.0:19132
```

### Environment Variables

Environment variables can be used instead of CLI flags (ideal for containers and systemd services):

```bash
export REMOTE_ADDR="10.4.10.222:19132"
export LISTEN_ADDR="0.0.0.0:19132"
./minecraft-bedrock-broadcast-relay
```

### Configuration Options

| Flag | Env Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `-remote` | `REMOTE_ADDR` | `10.4.10.222:19132` | Destination Bedrock server or K8s Service LoadBalancer IP:Port |
| `-listen` | `LISTEN_ADDR` | `0.0.0.0:19132` | Local UDP address to bind and receive LAN broadcasts |
| `-ping-interval` | — | `5s` | Interval to query backend server for MOTD and player count |
| `-idle-timeout` | — | `1m0s` | Duration of inactivity before pruning disconnected sessions |

---

## Running with Docker

You can run the relay as a lightweight container on any machine on your home LAN (e.g., Raspberry Pi, home server, NAS).

> **Important:** The container must use **host networking** (`--net=host`) so it can receive UDP broadcast packets on the physical network.

### `Dockerfile`
```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o relay .

FROM alpine:latest
WORKDIR /
COPY --from=builder /app/relay /relay
ENTRYPOINT ["/relay"]
```

### Run Container
```bash
docker build -t bedrock-relay .

docker run -d \
  --name bedrock-relay \
  --restart unless-stopped \
  --net=host \
  -e REMOTE_ADDR="10.4.10.222:19132" \
  -e LISTEN_ADDR="0.0.0.0:19132" \
  bedrock-relay
```

---

## Testing

Run unit tests:
```bash
go test -v ./...
```

---

## Network Considerations & Trade-Offs

* **Client IP Visibility:** Because the relay operates as a user-space UDP proxy, the Minecraft server in Kubernetes will log player connections as coming from the relay's IP address.
  * *Note:* Player identity, whitelisting, permissions, OP roles, and inventories are tied to the player's **Xbox Live XUID / UUID**, so in-game functionality is completely unaffected.
* **Port Availability:** The machine running this relay must have UDP port `19132` free (it cannot run on the same physical host that is already running another local Bedrock server on port 19132).
