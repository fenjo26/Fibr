package awg

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/store"
)

func TestGenerateValid(t *testing.T) {
	for i := 0; i < 200; i++ {
		p, err := Generate()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if err := p.Validate(); err != nil {
			t.Fatalf("generated params invalid: %v", err)
		}
		if p.Jc < 4 || p.Jc > 12 {
			t.Errorf("Jc outside recommended range: %d", p.Jc)
		}
		hs := map[uint32]bool{p.H1: true, p.H2: true, p.H3: true, p.H4: true}
		if len(hs) != 4 {
			t.Errorf("H1-H4 not unique: %d %d %d %d", p.H1, p.H2, p.H3, p.H4)
		}
		if p.S1+56 == p.S2 {
			t.Errorf("S1+56 == S2: %d %d", p.S1, p.S2)
		}
	}
}

func TestValidateRejectsBad(t *testing.T) {
	good := func() *Params {
		p, err := Generate()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		return p
	}
	cases := []struct {
		name string
		mut  func(p *Params)
	}{
		{"jc zero", func(p *Params) { p.Jc = 0 }},
		{"jc too big", func(p *Params) { p.Jc = 129 }},
		{"jmin >= jmax", func(p *Params) { p.Jmin = p.Jmax }},
		{"jmax > 1280", func(p *Params) { p.Jmax = 1281; p.Jmin = 1280 }},
		{"jmin zero", func(p *Params) { p.Jmin = 0 }},
		{"s1 too big", func(p *Params) { p.S1 = 1133 }},
		{"s2 too big", func(p *Params) { p.S2 = 1189 }},
		{"s1+56 == s2", func(p *Params) { p.S1 = 100; p.S2 = 156 }},
		{"headers collide", func(p *Params) { p.H2 = p.H1 }},
		{"header zero", func(p *Params) { p.H3 = 0 }},
		{"header negative-range overflow", func(p *Params) { p.H4 = 2147483648 }},
	}
	for _, tc := range cases {
		p := good()
		tc.mut(p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: validation must fail", tc.name)
		}
	}
}

func TestLoadOrGenerateParamsIdempotent(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	p1, err := LoadOrGenerateParams(st)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	p2, err := LoadOrGenerateParams(st)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if *p1 != *p2 {
		t.Fatalf("params not persisted identically: %+v vs %+v", p1, p2)
	}
}

func TestLoadOrGenerateParamsRejectsCorrupt(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	if err := st.SetSetting(settingParamsKey, `{"jc":0}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := LoadOrGenerateParams(st); err == nil {
		t.Fatal("corrupt params must be rejected, not regenerated")
	}
}

func TestClientINI(t *testing.T) {
	p := &Params{Jc: 5, Jmin: 40, Jmax: 80, S1: 15, S2: 16, H1: 111, H2: 222, H3: 333, H4: 444}
	ini := p.ClientINI()
	for _, want := range []string{
		"Jc = 5\n", "Jmin = 40\n", "Jmax = 80\n", "S1 = 15\n", "S2 = 16\n",
		"H1 = 111\n", "H2 = 222\n", "H3 = 333\n", "H4 = 444\n",
	} {
		if !strings.Contains(ini, want) {
			t.Errorf("ClientINI missing %q:\n%s", want, ini)
		}
	}
	if !strings.HasSuffix(ini, "H4 = 444\n") {
		t.Errorf("ClientINI must end with H4 line:\n%s", ini)
	}
}

func TestUAPISet(t *testing.T) {
	p := &Params{Jc: 5, Jmin: 40, Jmax: 80, S1: 15, S2: 16, H1: 111, H2: 222, H3: 333, H4: 444}
	got := p.UAPISet()
	want := "jc=5\njmin=40\njmax=80\ns1=15\ns2=16\nh1=111\nh2=222\nh3=333\nh4=444"
	if got != want {
		t.Fatalf("UAPISet mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestParseHeaderSpec(t *testing.T) {
	if s, e, err := ParseHeaderSpec("12345"); err != nil || s != 12345 || e != 12345 {
		t.Errorf("single value: %d %d %v", s, e, err)
	}
	if s, e, err := ParseHeaderSpec("100-200"); err != nil || s != 100 || e != 200 {
		t.Errorf("range: %d %d %v", s, e, err)
	}
	if _, _, err := ParseHeaderSpec("200-100"); err == nil {
		t.Error("reversed range must fail")
	}
	if _, _, err := ParseHeaderSpec("abc"); err == nil {
		t.Error("garbage must fail")
	}
}

// TestApplyParamsSock runs a fake UAPI daemon on a unix socket and checks
// both the request the daemon receives and the errno handling.
func TestApplyParamsSock(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "wg0.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer os.Remove(sock)

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		var req strings.Builder
		for {
			line, err := r.ReadString('\n')
			req.WriteString(line)
			if line == "\n" {
				break
			}
			if err != nil {
				return
			}
		}
		received <- req.String()
		conn.Write([]byte("errno=0\n\n"))
	}()

	p, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := ApplyParamsSock(sock, p); err != nil {
		t.Fatalf("apply: %v", err)
	}
	req := <-received
	if !strings.HasPrefix(req, "set=1\n") || !strings.Contains(req, "jc="+strconv.Itoa(p.Jc)+"\n") || !strings.Contains(req, "h4=") {
		t.Errorf("unexpected uapi request:\n%s", req)
	}
	if !strings.HasSuffix(req, "\n\n") {
		t.Errorf("request must end with empty line:\n%q", req)
	}
}

func TestApplyParamsSockErrno(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "wg0.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer os.Remove(sock)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if line == "\n" || err != nil {
				break
			}
		}
		conn.Write([]byte("errno=22\n\n"))
	}()

	p, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := ApplyParamsSock(sock, p); err == nil {
		t.Fatal("daemon errno != 0 must surface as an error")
	}
}

func TestApplyParamsSockInvalidParams(t *testing.T) {
	p := &Params{Jc: 0} // invalid, must be rejected before dialing
	if err := ApplyParamsSock("/nonexistent/wg0.sock", p); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid params must fail validation, got: %v", err)
	}
}
