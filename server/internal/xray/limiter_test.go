package xray

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseAccessLine(t *testing.T) {
	cases := []struct {
		line, ip, email string
		ok              bool
	}{
		{"2026/10/28 18:00:00.123456 from 203.0.113.7:51234 accepted tcp:example.com:443 [vless-in -> direct] email: aaaa-bbbb", "203.0.113.7", "aaaa-bbbb", true},
		{"2026/10/28 18:00:00 from tcp:198.51.100.9:4000 accepted tcp:1.1.1.1:443 [vless-in >> direct] email: u1\n", "198.51.100.9", "u1", true},
		{"2026/10/28 18:00:00 from [2001:db8::1]:4000 accepted tcp:x:443 [vless-in -> direct] email: u6", "2001:db8::1", "u6", true},
		{"2026/10/28 18:00:00 from 203.0.113.7:51234 accepted tcp:example.com:443 [api -> api]", "", "", false},
		{"random noise", "", "", false},
	}
	for _, c := range cases {
		e, ok := parseAccessLine(c.line)
		if ok != c.ok || e.ip != c.ip || e.email != c.email {
			t.Errorf("parseAccessLine(%q) = %+v,%v; want ip=%q email=%q ok=%v", c.line, e, ok, c.ip, c.email, c.ok)
		}
	}
}

func TestIPTrackerOver(t *testing.T) {
	tr := newIPTracker()
	now := time.Now()
	tr.add(accessEntry{ip: "1.1.1.1", email: "a"}, now.Add(-5*time.Minute)) // outside window
	tr.add(accessEntry{ip: "2.2.2.2", email: "a"}, now)
	tr.add(accessEntry{ip: "3.3.3.3", email: "a"}, now)
	tr.add(accessEntry{ip: "4.4.4.4", email: "b"}, now)
	tr.add(accessEntry{ip: "4.4.4.4", email: "b"}, now) // same ip twice
	over := tr.over(now, 2*time.Minute, 1)
	if len(over) != 1 || len(over["a"]) != 2 {
		t.Fatalf("over = %v, want only a with 2 ips", over)
	}
	if _, stale := tr.seen["a"]["1.1.1.1"]; stale {
		t.Fatal("stale ip not pruned")
	}
	if got := tr.over(now, 2*time.Minute, 2); len(got) != 0 {
		t.Fatalf("limit 2: over = %v, want none", got)
	}
}

// TestAPIClientAddRemove runs the hand-rolled gRPC client against a
// cleartext HTTP/2 server and checks the request framing and the encoded
// protobuf (the exact bytes xray-core would decode).
func TestAPIClientAddRemove(t *testing.T) {
	type call struct {
		path string
		msg  []byte
	}
	calls := make(chan call, 4)
	status := "0"
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.Header.Get("Content-Type") != "application/grpc" {
			t.Errorf("bad request: proto %d ct %q", r.ProtoMajor, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) < 5 || int(binary.BigEndian.Uint32(body[1:5])) != len(body)-5 {
			t.Errorf("bad grpc frame: %x", body)
		}
		calls <- call{r.URL.Path, body[5:]}
		w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
		w.Header().Set("Content-Type", "application/grpc")
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Grpc-Status", status)
		if status != "0" {
			w.Header().Set("Grpc-Message", "User x already exists.")
		}
	})
	var protos http.Protocols
	protos.SetUnencryptedHTTP2(true)
	srv := &http.Server{Handler: h, Protocols: &protos}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()

	c := NewAPIClient(ln.Addr().String())
	ctx := context.Background()
	if err := c.AddUser(ctx, "uuid-1"); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	got := <-calls
	if got.path != "/xray.app.proxyman.command.HandlerService/AlterInbound" {
		t.Fatalf("path %q", got.path)
	}
	account := typedMessage("xray.proxy.vless.Account", cat(pbString(1, "uuid-1"), pbString(2, ClientFlow), pbString(3, "none")))
	user := cat(pbString(2, "uuid-1"), pbBytes(3, account))
	want := cat(pbString(1, InboundTag), pbBytes(2, typedMessage("xray.app.proxyman.command.AddUserOperation", pbBytes(1, user))))
	if string(got.msg) != string(want) {
		t.Fatalf("AddUser message mismatch\n got %x\nwant %x", got.msg, want)
	}
	// Field 1 tag 0x0a + len 8 + "vless-in" — sanity-check the hand encoder.
	if !strings.HasPrefix(string(got.msg), "\x0a\x08vless-in") {
		t.Fatalf("unexpected prefix %x", got.msg[:10])
	}

	if err := c.RemoveUser(ctx, "uuid-1"); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if got := <-calls; !strings.Contains(string(got.msg), "RemoveUserOperation") {
		t.Fatalf("remove message %q", got.msg)
	}

	status = "2" // "already exists" is success for AddUser
	if err := c.AddUser(ctx, "uuid-1"); err != nil {
		t.Fatalf("AddUser on existing: %v", err)
	}
	<-calls
	if err := c.RemoveUser(ctx, "uuid-1"); err == nil {
		t.Fatal("RemoveUser must surface a non-'not found' grpc error")
	}
	<-calls
}

func TestNewAPIClientOff(t *testing.T) {
	if NewAPIClient("off") != nil || NewAPIClient("") != nil {
		t.Fatal("off/empty must disable the client")
	}
}
