package xray

// A tiny client for xray-core's gRPC HandlerService — just AddUser and
// RemoveUser on the VLESS inbound — so client changes apply LIVE instead of
// waiting for `docker compose restart xray`, and the sharing limiter can cut
// one link without touching the others.
//
// No grpc/protobuf modules: gRPC is HTTP/2 POST with a 5-byte length prefix,
// spoken here over cleartext HTTP/2 (stdlib, Go 1.24+), and the three
// messages involved are hand-encoded with the protobuf wire format.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// InboundTag is the tag of the VLESS + Reality inbound in the rendered config.
const InboundTag = "vless-in"

// XHTTPInboundTag is the tag of the VLESS-over-XHTTP inbound that Caddy
// proxies to (control plane only, see xray.go).
const XHTTPInboundTag = "vless-xhttp"

// APIClient talks to xray's HandlerService at addr ("host:port").
type APIClient struct {
	addr string
	http *http.Client
}

// NewAPIClient returns nil for an empty / "off" address.
func NewAPIClient(addr string) *APIClient {
	addr = strings.TrimSpace(addr)
	if addr == "" || addr == "off" {
		return nil
	}
	var protos http.Protocols
	protos.SetUnencryptedHTTP2(true) // gRPC over h2c, prior knowledge
	return &APIClient{
		addr: addr,
		http: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{Protocols: &protos},
		},
	}
}

// ErrGRPC is a non-OK gRPC status from xray.
type ErrGRPC struct {
	Code    string
	Message string
}

func (e *ErrGRPC) Error() string { return "xray api: grpc status " + e.Code + ": " + e.Message }

// AddUser adds a VLESS client (id = uuid, email = uuid so the access log
// names the link) to the inbound with the given tag. flow is the client flow
// ("xtls-rprx-vision" on Reality/TCP, "" on XHTTP). "already exists" counts
// as success.
func (c *APIClient) AddUser(ctx context.Context, tag, uuid, flow string) error {
	acct := pbString(1, uuid)
	if flow != "" {
		acct = cat(acct, pbString(2, flow))
	}
	account := typedMessage("xray.proxy.vless.Account", cat(acct, pbString(3, "none")))
	user := cat(pbString(2, uuid), pbBytes(3, account)) // level 0 omitted
	op := typedMessage("xray.app.proxyman.command.AddUserOperation", pbBytes(1, user))
	err := c.alterInbound(ctx, tag, op)
	if isGRPC(err, "already exists") {
		return nil
	}
	return err
}

// RemoveUser drops a VLESS client by email (= uuid) from the inbound with
// the given tag. "not found" counts as success.
func (c *APIClient) RemoveUser(ctx context.Context, tag, uuid string) error {
	op := typedMessage("xray.app.proxyman.command.RemoveUserOperation", pbString(1, uuid))
	err := c.alterInbound(ctx, tag, op)
	if isGRPC(err, "not found") {
		return nil
	}
	return err
}

func isGRPC(err error, substr string) bool {
	var g *ErrGRPC
	return errors.As(err, &g) && strings.Contains(strings.ToLower(g.Message), substr)
}

// alterInbound calls HandlerService.AlterInbound{tag, operation}.
func (c *APIClient) alterInbound(ctx context.Context, tag string, operation []byte) error {
	req := cat(pbString(1, tag), pbBytes(2, operation))
	return c.unary(ctx, "/xray.app.proxyman.command.HandlerService/AlterInbound", req)
}

func (c *APIClient) unary(ctx context.Context, method string, msg []byte) error {
	body := make([]byte, 5+len(msg)) // flag 0 (uncompressed) + big-endian length
	binary.BigEndian.PutUint32(body[1:5], uint32(len(msg)))
	copy(body[5:], msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.addr+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("xray api: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // trailers arrive after the body
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("xray api: http %d", resp.StatusCode)
	}
	return grpcStatus(resp.Header, resp.Trailer)
}

// grpcStatus reads grpc-status from the trailers (or the headers of a
// trailers-only response).
func grpcStatus(h, tr http.Header) error {
	code, msg := tr.Get("Grpc-Status"), tr.Get("Grpc-Message")
	if code == "" {
		code, msg = h.Get("Grpc-Status"), h.Get("Grpc-Message")
	}
	if code == "" || code == "0" {
		return nil
	}
	return &ErrGRPC{Code: code, Message: percentDecode(msg)}
}

// percentDecode undoes gRPC's grpc-message percent-encoding.
func percentDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			var v byte
			if _, err := fmt.Sscanf(s[i+1:i+3], "%02x", &v); err == nil {
				b.WriteByte(v)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

/* ---------------- protobuf wire format (just what we need) ---------------- */

func typedMessage(typeName string, value []byte) []byte {
	return cat(pbString(1, typeName), pbBytes(2, value))
}

func pbString(field int, s string) []byte { return pbBytes(field, []byte(s)) }

func pbBytes(field int, v []byte) []byte {
	out := appendVarint(nil, uint64(field)<<3|2) // wire type 2: length-delimited
	out = appendVarint(out, uint64(len(v)))
	return append(out, v...)
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
