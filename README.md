# nt-backend-wrtc

A simple rendezvous + WebSocket signaling backend for WebRTC. It exposes:

* **Rendezvous API** to mint short-lived numeric codes and redeem them once.
* **WebSocket signaling** for SDP/ICE exchange between two peers (A/B).
* **Metrics** (Prometheus), **health/ready** probes, optional **TLS**, and simple **rate limiting**.

Designed to be small, idiomatic, and production-friendly.

---

## Features

* **WS signaling**: Relays `offer`, `answer`, `ice` JSON frames between sides; supports mailbox messages (`hello`, `send`, `delivered`).
* **Rendezvous**: 4‑digit codes, unique until expiry; codes reclaimed on expiry; single-use `redeem`.
* **Per-connection write lock**: All WS writes (data + control/ping) are serialized to prevent gorilla/websocket races.
* **Rate limiting (instance-based)**: Per-IP fixed window (per-minute); configurable for HTTP & WS upgrades.
* **CORS / Origin policy**: Allow-all in dev; allowlist in prod.
* **Metrics**: Prometheus endpoint (`/metrics`).
* **Health**: `/healthz` (liveness), `/readyz` (readiness).
* **TLS**: Optional; supports modern defaults.
* **Janitor**: Background sweeper to prune expired codes.

---

## Quick start

### 1) Run with Docker Compose

```bash
docker compose up --build -d
# Rendezvous — create a code
curl -fsS -X POST http://localhost:1234/rendezvous/code
# {"code":"2802","appID":"...","expiresAt":"..."}

# Redeem once
curl -fsS -X POST http://localhost:1234/rendezvous/redeem \
  -H 'content-type: application/json' -d '{"code":"2802"}'
```

### 2) WebSocket signaling (two terminals)

```bash
# Terminal A
wscat -c "ws://localhost:1234/ws?appID=<app-id>&side=A"
# Terminal B
wscat -c "ws://localhost:1234/ws?appID=<app-id>&side=B"
# Send from A, receive on B
> {"type":"offer","sdp":"..."}
```

---

## Configuration

Environment variables (sane defaults baked in):

| Variable            |    Default | Description                                      |
| ------------------- | ---------: | ------------------------------------------------ |
| `HOST`              |  `0.0.0.0` | Bind address for HTTP server                     |
| `PORT`              |     `1234` | HTTP/TLS port                                    |
| `ROOM_TTL`          |      `10m` | Rendezvous code time-to-live                     |
| `HEARTBEAT`         |      `20s` | WS ping interval & read deadline base            |
| `WS_MAX_MSG`        |  `1048576` | Max WS message bytes (read limit)                |
| `WS_READ_BUF`       |     `4096` | Gorilla upgrader read buffer                     |
| `WS_WRITE_BUF`      |     `4096` | Gorilla upgrader write buffer                    |
| `HTTP_RATE_PER_MIN` |        `0` | Per-IP HTTP limit; `0` disables                  |
| `WS_RATE_PER_MIN`   |        `0` | Per-IP WS upgrade limit; `0` disables            |
| `CORS_ORIGINS`      |  *(empty)* | Comma‑sep allowlist of origins (prod)            |
| `DEV`               |     `true` | If `true`, allow all origins                     |
| `TLS_CERT_FILE`     |  *(empty)* | Path to TLS cert (enables TLS when set with key) |
| `TLS_KEY_FILE`      |  *(empty)* | Path to TLS key                                  |
| `METRICS_ROUTE`     | `/metrics` | Prometheus endpoint path                         |
| `LOG_LEVEL`         |     `info` | Log level                                        |

> **Note:** If only one of `TLS_CERT_FILE`/`TLS_KEY_FILE` is set, the server refuses to start.

---

## Endpoints

### Rendezvous (prefixed with `/rendezvous`)

* `POST /code` → `{ code, appID, expiresAt }`
  Mints a fresh code; unique until TTL expires.

* `POST /redeem` body: `{ "code": "NNNN" }`
  On success: `200 { appID, expiresAt }`
  On used/expired/unknown: **410 Gone**.

### WebSocket signaling

* `GET /ws?appID=<uuid>&side=A|B` upgrades to WS.
* Accepted frames (JSON with `type`): `offer`, `answer`, `ice`, `hello`, `send`, `delivered`, `telemetry`.

  * Relay frames (`offer`/`answer`/`ice`) are forwarded to the other side.
  * Mailbox: `hello` (trim), `send` (enqueue to `to`), `delivered` (ack up to `seq`).
  * `telemetry` (optional): `{ "type":"telemetry","event":"ice-connected" }` updates time‑to‑first metric once.

### Health & metrics

* `GET /healthz` → 200
* `GET /readyz` → 200 when ready
* `GET /metrics` → Prometheus text format

---

## Build from source

```bash
go mod tidy
go build -trimpath -o ./bin/server ./cmd/server
```

TLS (self‑signed) for local tests:

```bash
openssl req -x509 -newkey rsa:2048 -nodes -keyout server.key -out server.crt -days 365 -subj "/CN=localhost"
TLS_CERT_FILE=$PWD/server.crt TLS_KEY_FILE=$PWD/server.key ./bin/server
```

---

## Tests

### Unit + race detector

```bash
go clean -testcache
GOMAXPROCS=4 go test ./... -race -count=2
```

### What’s covered

* **Hub**: mailbox concurrency, `MarkEstablished` exactly-once.
* **Rendezvous**: create/redeem under contention, expiry paths, janitor sweep.
* **WS**: integration relay, ping-vs-write races, mailbox (`hello`/`send`/`delivered`).
* **Middleware**: HTTP/WS rate limit behavior.

---

## Security

* **govulncheck**: run in CI; locally:

  ```bash
  go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  ```
* Go toolchain pinned (recommend `1.24.6`).
* No reachable vulnerabilities at time of writing; protobuf pinned to a fixed version.

---

## Operational notes

* **Origin policy**: In `DEV=true`, any `Origin` is accepted; in production, set `CORS_ORIGINS` to a comma‑separated list (e.g., `https://example.com,https://app.example.com`). Unknown origins receive `403` before WS upgrade.
* **Rate limiting**: Instance-based per‑IP limiter; set `HTTP_RATE_PER_MIN` & `WS_RATE_PER_MIN` (>0 to enable). Behind proxies, ensure `X-Forwarded-For` is set by a trusted proxy or keep `DEV` deployments simple.
* **Graceful shutdown**: The server listens for SIGINT/SIGTERM; rendezvous janitor stops on context cancellation.

---

## Docker

```bash
docker build -t nt-backend-wrtc:local .
docker compose up --build -d
```

Example compose:

```yaml
version: "3.8"
services:
  rendezvous:
    build: .
    image: nt-backend-wrtc:local
    restart: unless-stopped
    environment:
      - HOST=0.0.0.0
      - PORT=1234
      - ROOM_TTL=10m
      - HEARTBEAT=20s
      - WS_MAX_MSG=1048576
      - HTTP_RATE_PER_MIN=0
      - WS_RATE_PER_MIN=0
      - METRICS_ROUTE=/metrics
      - LOG_LEVEL=info
      - DEV=true
    ports:
      - "1234:1234"
```

---

## Development workflow

* Format & imports: `gofmt -s -w . && go run golang.org/x/tools/cmd/goimports@latest -w .`
* Vet & lint: `go vet ./... && go run honnef.co/go/tools/cmd/staticcheck@latest ./...`
* Tests (race): `GOMAXPROCS=4 go test ./... -race -count=1`
* Vulnerabilities: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`

---

## License

Apache License Version 2.0,

---

## FAQ

* **Why 4‑digit codes?** Small UX footprint. The store reclaims expired slots and prevents reuse during TTL; expand to 6 digits by changing one modulus/format string; Not relevant for security.
* **Do I need telemetry frames?** No; they only enrich metrics (`time_to_first`).
* **Why not JWT/auth now?** Kept minimal by design; can be added behind a proxy or via middleware with little surface change.
