# syntax=docker/dockerfile:1
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
VOLUME /data
EXPOSE 8080 51820/udp
ENTRYPOINT ["/usr/local/bin/fibr"]
