# syntax=docker/dockerfile:1

# amneziawg-go — userspace AmneziaWG daemon (wireguard-go fork) serving the
# awg0 tunnel (and wg0 when FIBR_WG_ENGINE=awg). It is published as a Go
# module whose root is the daemon's main package, so a pinned version
# installs straight from the module proxy (the /v3 suffix still yields a
# binary named amneziawg-go). v3.x speaks AWG 2.0 parameters (S3/S4, H1-H4
# ranges, I1) and stays wire-compatible with older AmneziaWG 2.0 clients
# (verified against amneziawg-go v0.2.19 in both directions).
FROM golang:1.26-alpine AS awg
RUN GOBIN=/out go install github.com/amnezia-vpn/amneziawg-go/v3@v3.1.20260828

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/fibr ./cmd/fibr

FROM alpine:3.24
# iproute2/iptables manage the interface and NAT; wireguard-tools for debugging.
RUN apk add --no-cache ca-certificates iproute2 iptables wireguard-tools
COPY --from=build /out/fibr /usr/local/bin/fibr
COPY --from=awg /out/amneziawg-go /usr/local/bin/amneziawg-go
VOLUME /data
EXPOSE 8080 51820/udp
ENTRYPOINT ["/usr/local/bin/fibr"]
