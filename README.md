# nt-backend-wrtc (mini)

Minimal **rendezvous + signaling** backend for NoisyTransfer, written in Go.  
**In-memory only** (no persistence), **WebSocket** based, with **Prometheus metrics**, **health/ready** endpoints, structured logs, and **Docker**.

- Module path: `github.com/collapsinghierarchy/nt-backend-wrtc`
- Go version: **1.22+**
- Purpose: provide only **rendezvous + signaling** for peers (exchange SDP & ICE).

---

## Features
- 🧭 **Rendezvous & signaling** via WebSocket (`/ws?room=<code>[&key=...]`)
- 🫧 **Ephemeral rooms** in memory with TTL (default 10m) and heartbeat cleanup
- 📈 **/metrics** (Prometheus)
- ❤️ **/healthz** and **/readyz**
- 🪵 **Structured logs** (zap)
- 🐳 **Dockerfile** & **docker-compose.yml**
- 🔒 Optional shared key for WS upgrades (`RENDEZVOUS_WS_KEY`)

> When a room empties or TTL expires, it is removed.

---

## API

### Health/Readiness
- `GET /healthz` → `{"ok": true}`
- `GET /readyz` → `{"ready": true}`

### Metrics
- `GET /metrics` → Prometheus format  
  Exposes counters/gauges like:
  - `nt_ws_connections_total`
  - `nt_ws_messages_total{type="..."}`
  - `nt_ws_errors_total`
  - `nt_rooms_active`
  - `nt_peers_active`

### WebSocket: `/ws?room=<CODE>[&key=<SECRET>]`
**Query params**
- `room` (required) — rendezvous code / room id
- `key` (optional) — must match `RENDEZVOUS_WS_KEY` if set

**Client → Server messages**
```jsonc
// First message must be JOIN
{ "type": "join", "peerId": "optional-fixed-id" }

// Relay signaling data (SDP/ICE)
{ "type": "signal", "to": "optional-peer-id", "data": { /* your payload */ } }

// Leave the room
{ "type": "leave" }
```

**Server → Client messages**
```jsonc
// Sent right after a successful join
{ "type": "ready", "selfId": "abc", "peers": [ { "peerId": "def" } ] }

// When a peer arrives/leaves
{ "type": "peer-joined", "peerId": "def" }
{ "type": "peer-left",   "peerId": "def" }

// Relayed signaling data
{ "type": "signal", "from": "abc", "data": { /* payload */ } }
```

**Heartbeat**
- Server sends WS **ping** every `HEARTBEAT` (default 20s).
- Client must reply **pong** (handled automatically by most WS libs).
- If a client stops responding, the connection closes and the peer is removed.

---

## Configuration (env)

| Var | Default | Description |
|-----|---------|-------------|
| `HOST` | `0.0.0.0` | Listen address |
| `PORT` | `8080` | Listen port |
| `RENDEZVOUS_WS_KEY` | *(empty)* | Optional shared key for `/ws` (passed as `?key=`) |
| `ROOM_TTL` | `10m` | Duration until an idle room expires |
| `HEARTBEAT` | `20s` | WS ping interval (and read deadline extension) |
| `MAX_PEERS_PER_ROOM` | `2` | Max peers in a room |
| `METRICS_ROUTE` | `/metrics` | Metrics path |
| `LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |

Create a `.env` (optional):
```env
RENDEZVOUS_WS_KEY=dev-secret
ROOM_TTL=10m
HEARTBEAT=20s
MAX_PEERS_PER_ROOM=2
LOG_LEVEL=info
```

---

## Quick start (local)

```bash
# Go 1.22+
go run ./cmd/server
# with env
RENDEZVOUS_WS_KEY=dev go run ./cmd/server
```

Test locally:
```bash
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz
curl -s localhost:8080/metrics | head -n 20
```

Connect a WS client:
```bash
# Example using websocat (brew install websocat / apt install websocat)
websocat "ws://localhost:8080/ws?room=ABC-123&key=dev"
# then send:
# {"type":"join","peerId":"sender"}
# and later:
# {"type":"signal","to":"receiver","data":{"sdp":"...","type":"offer"}}
```

---

## Docker

**Dockerfile**
```Dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/nt-backend-wrtc ./cmd/server

FROM gcr.io/distroless/base-debian12
ENV PORT=8080
EXPOSE 8080
COPY --from=build /out/nt-backend-wrtc /usr/local/bin/nt-backend-wrtc
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/nt-backend-wrtc"]
```

**docker-compose.yml**
```yaml
version: "3.8"
services:
  rendezvous:
    build: .
    image: nt-backend-wrtc:latest
    restart: unless-stopped
    environment:
      - HOST=0.0.0.0
      - PORT=8080
      - RENDEZVOUS_WS_KEY=${RENDEZVOUS_WS_KEY}
      - ROOM_TTL=10m
      - HEARTBEAT=20s
      - MAX_PEERS_PER_ROOM=2
      - METRICS_ROUTE=/metrics
      - LOG_LEVEL=info
    ports:
      - "8080:8080"
```

Run:
```bash
docker compose up --build
```

---

## Code layout

```
cmd/server/main.go            # server startup, wiring
internal/config/config.go     # env + defaults
internal/logs/logger.go       # zap + request logging middleware
internal/metrics/metrics.go   # prometheus metrics & handler
internal/rooms/rooms.go       # in-memory rooms with TTL + relay
internal/ws/handler.go        # WebSocket upgrade + protocol
internal/health/health.go     # /healthz, /readyz
Dockerfile, docker-compose.yml
```

---

## Production notes
- The service is **stateless**; run multiple instances behind a load balancer with **sticky sessions** on `/ws` (or a consistent hash of `room`). Without Redis, rooms are node-local.
- Set `RENDEZVOUS_WS_KEY` so random clients can’t squat on codes.
- Expose `/metrics` internally only; scrape with Prometheus.
- Tune `ROOM_TTL` and `HEARTBEAT` to your UX (shorter TTLs free memory aggressively).

---

## License
AGPL-3.0-only (adjust to your project’s license as needed)
