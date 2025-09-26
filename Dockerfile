# syntax=docker/dockerfile:1

FROM golang:1.24.4 as build
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