A simple rendezvous + WebSocket signaling backend for WebRTC, designed for NoisyTransfer-style workflows. It mints short‑lived numeric codes for pairing and relays SDP/ICE between two peers over WebSockets.

## Quick links
- **GitHub repository:** <https://github.com/collapsinghierarchy/nt-backend-wrtc>
- **Related packages**
  - **CLI:** <https://www.npmjs.com/package/@noisytransfer/cli>
  - **noisyauth:** <https://www.npmjs.com/package/@noisytransfer/noisyauth>
  - **noisystream:** <https://www.npmjs.com/package/@noisytransfer/noisystream>

## Features
- **Rendezvous service**: short‑lived numerical 4‑digit codes, single‑use redeem, reclaimed on expiry.
- **WebSocket signaling** for SDP/ICE exchange between two sides (`A`/`B`).
- **Mailbox frames**: lightweight message queue (`hello`, `send`, `delivered`) in addition to `offer`/`answer`/`ice`; optional `telemetry` events.
- **Rate limiting** (per‑IP, fixed window) for HTTP and WS upgrades.
- **Observability**: Prometheus `/metrics`, `/healthz` (liveness), `/readyz` (readiness).
- **TLS**: optional, with sensible defaults.
- **Janitor**: background sweeper that prunes expired codes.

## Quick start

### 1) Run with Docker Compose
```bash
docker compose up --build -d
```

Create a code:
```bash
curl -fsS -X POST http://localhost:1234/rendezvous/code
# -> {"code":"2802","appID":"...","expiresAt":"..."}
```

Redeem (once):
```bash
curl -fsS -X POST http://localhost:1234/rendezvous/redeem   -H 'content-type: application/json' -d '{"code":"2802"}'
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

## Endpoints

### Rendezvous (`/rendezvous` prefix)
- `POST /code` → `{"code","appID","expiresAt"}` — mint a fresh code.
- `POST /redeem` body: `{"code":"NNNN"}` → `200 {"appID","expiresAt"}`; returns **410 Gone** if used/expired/unknown.

### WebSocket signaling
- `GET /ws?appID=<uuid>&side=A|B` — upgrade to WS.
- **Accepted frames** (JSON with `type`): `offer`, `answer`, `ice`, `hello`, `send`, `delivered`, `telemetry`.
  - Relay frames (`offer`/`answer`/`ice`) forward to the opposite side.
  - **Mailbox**: `hello` (trim), `send` (enqueue to `to`), `delivered` (ack up to `seq`).
  - `telemetry` (optional): e.g. `{ "type":"telemetry","event":"ice-connected" }`.

### Health & metrics
- `GET /healthz` → 200
- `GET /readyz` → 200 when ready
- `GET /metrics` → Prometheus text exposition

## Configuration (environment variables)

| Variable           | Default     | Description                                                  |
|--------------------|-------------|--------------------------------------------------------------|
| `HOST`             | `0.0.0.0`   | Bind address for HTTP server                                 |
| `PORT`             | `1234`      | HTTP/TLS port                                                |
| `ROOM_TTL`         | `10m`       | Rendezvous code time‑to‑live                                 |
| `HEARTBEAT`        | `20s`       | WS ping interval & read‑deadline base                        |
| `WS_MAX_MSG`       | `1048576`   | Max WS message bytes (read limit)                            |
| `WS_READ_BUF`      | `4096`      | Gorilla upgrader read buffer                                 |
| `WS_WRITE_BUF`     | `4096`      | Gorilla upgrader write buffer                                |
| `HTTP_RATE_PER_MIN`| `0`         | Per‑IP HTTP limit; `0` disables                              |
| `WS_RATE_PER_MIN`  | `0`         | Per‑IP WS upgrade limit; `0` disables                        |
| `CORS_ORIGINS`     | *(empty)*   | Comma‑separated allowlist of origins (prod)                  |
| `DEV`              | `true`      | If `true`, allow all origins                                 |
| `TLS_CERT_FILE`    | *(empty)*   | Path to TLS cert (requires key too)                          |
| `TLS_KEY_FILE`     | *(empty)*   | Path to TLS key (requires cert too)                          |
| `METRICS_ROUTE`    | `/metrics`  | Prometheus endpoint path                                     |
| `LOG_LEVEL`        | `info`      | Log level                                                    |

> **Note:** The server refuses to start if only one of `TLS_CERT_FILE` or `TLS_KEY_FILE` is set.

## Build from source
```bash
go mod tidy
go build -trimpath -o ./bin/server ./cmd/server
```

Self‑signed TLS for local tests:
```bash
openssl req -x509 -newkey rsa:2048 -nodes -keyout server.key -out server.crt -days 365 -subj "/CN=localhost"
TLS_CERT_FILE=$PWD/server.crt TLS_KEY_FILE=$PWD/server.key ./bin/server
```

## Tests
```bash
go clean -testcache
GOMAXPROCS=4 go test ./... -race -count=2
```

## Integration in the NoisyTransfer stack
- Pairing flow: clients mint a short code via `/rendezvous/code`, redeem once via `/rendezvous/redeem` to get an `appID`, then connect both sides (`A`/`B`) to `/ws` and exchange `offer`/`answer`/`ice`.
- Use with the **CLI** (`@noisytransfer/cli`) or your own app built on `@noisytransfer/noisyauth` + `@noisytransfer/noisystream` or `@noisytransfer/transport` .
- Deploy behind a reverse proxy if you need TLS termination, sticky IP limits, or custom CORS.

## License
**Apache‑2.0**

---

If you’re building end‑to‑end flows, also see:
- **CLI:** `@noisytransfer/cli` — <https://www.npmjs.com/package/@noisytransfer/cli>
- **Auth:** `@noisytransfer/noisyauth` — <https://www.npmjs.com/package/@noisytransfer/noisyauth>
- **PQ Encrypted Transport:** `@noisytransfer/noisystream` — <https://www.npmjs.com/package/@noisytransfer/noisystream>
