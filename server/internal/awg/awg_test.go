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
	if p1.UAPISet() != p2.UAPISet() {
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

func TestGenerateV2Valid(t *testing.T) {
	for i := 0; i < 300; i++ {
		p, err := GenerateV2()
		if err != nil {
			t.Fatalf("generate v2: %v", err)
		}
		if !p.V2() || len(p.HR) != 4 || p.I1 == "" || p.S3 == 0 || p.S4 == 0 {
			t.Fatalf("incomplete v2 set: %+v", p)
		}
		if err := p.Validate(); err != nil {
			t.Fatalf("v2 invalid: %v", err)
		}
	}
}

func TestValidateV2RejectsBad(t *testing.T) {
	good := func() *Params {
		p, err := GenerateV2()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]func(p *Params){
		"overlap":         func(p *Params) { p.HR[1] = HRange{Min: p.HR[0].Min, Max: p.HR[0].Max} },
		"three ranges":    func(p *Params) { p.HR = p.HR[:3] },
		"reversed":        func(p *Params) { p.HR[2] = HRange{Min: 900, Max: 800} },
		"too low":         func(p *Params) { p.HR[3] = HRange{Min: 1, Max: 4} },
		"size collide":    func(p *Params) { p.S1 = 10; p.S3 = 94 }, // 148+10 == 64+94
		"s4 too big":      func(p *Params) { p.S4 = 65 },
		"bad i1 tag":      func(p *Params) { p.I1 = "<x 1>" },
		"bad i1 hex":      func(p *Params) { p.I1 = "<b 0xabc>" },
		"i1 over mtu":     func(p *Params) { p.I1 = "<r 1300>" },
		"i1 unterminated": func(p *Params) { p.I1 = "<r 10" },
	}
	for name, mut := range cases {
		p := good()
		mut(p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestQUICInitialSignature(t *testing.T) {
	s, err := QUICInitialSignature()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSignature(s); err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	// 6 + 8 + 4 + 1182 = 1200 bytes; length varint 0x4000|1182.
	if !strings.Contains(s, "0000000108><r 8><b 0x0000449e><r 1182>") || !strings.HasPrefix(s, "<b 0xc") {
		t.Errorf("unexpected QUIC signature %q", s)
	}
}

func TestClientINIAndUAPIV2(t *testing.T) {
	p := &Params{Version: 2, Jc: 5, Jmin: 40, Jmax: 80, S1: 15, S2: 16, S3: 20, S4: 8,
		HR: []HRange{{100, 200}, {300, 400}, {500, 600}, {700, 700}}, I1: "<b 0xc0><r 10>"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	ini := p.ClientINI()
	for _, want := range []string{"S3 = 20\n", "S4 = 8\n", "H1 = 100-200\n", "H4 = 700\n", "I1 = <b 0xc0><r 10>\n"} {
		if !strings.Contains(ini, want) {
			t.Errorf("ClientINI missing %q:\n%s", want, ini)
		}
	}
	want := "jc=5\njmin=40\njmax=80\ns1=15\ns2=16\ns3=20\ns4=8\nh1=100-200\nh2=300-400\nh3=500-600\nh4=700"
	if got := p.UAPISet(); got != want {
		t.Errorf("UAPISet v2:\n got %q\nwant %q", got, want)
	}
	// The daemon's own parser must accept every header spec we render.
	for _, r := range p.HR {
		if s, e, err := ParseHeaderSpec(r.String()); err != nil || s != r.Min || e != r.Max {
			t.Errorf("ParseHeaderSpec(%s) = %d %d %v", r, s, e, err)
		}
	}
}

func TestLoadOrGenerateV2(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	v1, err := LoadOrGenerate(st, 1, "")
	if err != nil || v1.V2() {
		t.Fatalf("v1: %+v %v", v1, err)
	}
	a, err := LoadOrGenerate(st, 2, "")
	if err != nil || !a.V2() {
		t.Fatalf("v2: %+v %v", a, err)
	}
	b, err := LoadOrGenerate(st, 2, "")
	if err != nil || b.UAPISet() != a.UAPISet() || b.I1 != a.I1 {
		t.Fatalf("v2 not persisted: %v\n%s\n%s", err, a.UAPISet(), b.UAPISet())
	}
	// The legacy set stays untouched next to it.
	v1b, _ := LoadOrGenerate(st, 1, "")
	if v1b.UAPISet() != v1.UAPISet() {
		t.Errorf("legacy params changed")
	}
	// I1 override (client-side only) replaces the stored signature.
	c, err := LoadOrGenerate(st, 2, "<b 0x01><r 5>")
	if err != nil || c.I1 != "<b 0x01><r 5>" || c.UAPISet() != a.UAPISet() {
		t.Fatalf("i1 override: %+v %v", c, err)
	}
	if _, err := LoadOrGenerate(st, 2, "<zz>"); err == nil {
		t.Error("bad FIBR_AWG_I1 must fail")
	}
}
