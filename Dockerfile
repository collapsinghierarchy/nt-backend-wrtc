# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.24.6 AS build
WORKDIR /src

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Copy sources
COPY . .

# Build a small static binary
RUN CGO_ENABLED=0 \
    GOFLAGS="-buildvcs=false" \
    go build -trimpath -ldflags="-s -w" \
      -o /out/nt-backend-wrtc ./cmd/server

# --- runtime stage ---
# Use the fully static base (smallest); use :base if you need certs/tools
FROM gcr.io/distroless/static-debian12:nonroot

# Keep the default port consistent with the project (8080)
ENV PORT=8080
EXPOSE 8080

COPY --from=build /out/nt-backend-wrtc /usr/local/bin/nt-backend-wrtc

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/nt-backend-wrtc"]
