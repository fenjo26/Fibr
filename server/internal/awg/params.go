// Package awg implements AmneziaWG support: generation and persistence of the
// obfuscation parameters, a small client for the amneziawg-go UAPI socket and
// lifecycle management of the userspace daemon.
//
// Two parameter generations exist:
//
//   - AWG 1.0 (legacy): Jc/Jmin/Jmax/S1/S2 and fixed H1-H4 values. Its
//     fixed-size, fixed-header handshake is fingerprinted by Russian TSPU
//     since 2025 (docs/awg.md).
//   - AWG 2.0 (default, Version 2): adds S3/S4 padding of the cookie and
//     transport messages, H1-H4 as non-overlapping RANGES (every packet gets a
//     fresh random header from its range) and the I1 "signature" packet sent
//     before each handshake, here shaped like a QUIC Initial. Understood by
//     current AmneziaVPN / AmneziaWG apps and by amneziawg-go >= v0.2.15.
//
// AmneziaWG extends WireGuard with traffic masking: junk packets before the
// handshake (Jc of random size Jmin..Jmax), padding on handshake packets
// (S1/S2) and randomised magic header bytes (H1-H4). S1/S2/H1-H4 must match
// between client and server; Jc/Jmin/Jmax may differ. The official parameter
// constraints come from the AmneziaWG kernel module documentation:
// https://github.com/amnezia-vpn/amneziawg-linux-kernel-module
package awg

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/fenjo26/Fibr/internal/store"
)

const (
	settingParamsKey   = "awg_params"  // AWG 1.0 parameter set (legacy)
	settingParamsKeyV2 = "awg2_params" // AWG 2.0 parameter set
)

// HRange is an AWG 2.0 magic-header range (inclusive).
type HRange struct {
	Min uint32 `json:"min"`
	Max uint32 `json:"max"`
}

func (r HRange) String() string {
	if r.Min == r.Max {
		return strconv.FormatUint(uint64(r.Min), 10)
	}
	return fmt.Sprintf("%d-%d", r.Min, r.Max)
}

// Params holds the AmneziaWG obfuscation parameters shared by the server
// daemon and every client config.
type Params struct {
	// Version: 0 or 1 = AWG 1.0 (H1-H4 single values), 2 = AWG 2.0 (S3/S4,
	// HR ranges, I1).
	Version int `json:"version,omitempty"`

	Jc   int    `json:"jc"`   // junk packet count before each handshake: 1..128 (recommended 4..12)
	Jmin int    `json:"jmin"` // minimum junk packet size: < Jmax, < 1280
	Jmax int    `json:"jmax"` // maximum junk packet size: > Jmin, <= 1280
	S1   int    `json:"s1"`   // initiation packet padding: <= 1132, S1+56 != S2 (recommended 15..150)
	S2   int    `json:"s2"`   // response packet padding: <= 1188 (recommended 15..150)
	H1   uint32 `json:"h1"`   // magic header override bytes, must be unique among H1-H4
	H2   uint32 `json:"h2"`
	H3   uint32 `json:"h3"`
	H4   uint32 `json:"h4"`

	// AWG 2.0 only.
	S3 int      `json:"s3,omitempty"` // cookie reply padding (server-side, must match)
	S4 int      `json:"s4,omitempty"` // transport message padding (must match)
	HR []HRange `json:"hr,omitempty"` // H1-H4 ranges, exactly 4, non-overlapping (must match)
	I1 string   `json:"i1,omitempty"` // signature packet before each handshake (client-side)
}

// V2 reports whether the set is AWG 2.0.
func (p *Params) V2() bool { return p.Version >= 2 }

// Generate returns fresh random parameters inside the ranges recommended by
// the AmneziaWG documentation.
func Generate() (*Params, error) {
	p := &Params{}
	var err error
	if p.Jc, err = randInt(4, 12); err != nil {
		return nil, err
	}
	if p.Jmin, err = randInt(40, 60); err != nil {
		return nil, err
	}
	if p.Jmax, err = randInt(p.Jmin+30, p.Jmin+90); err != nil {
		return nil, err
	}
	if p.S1, err = randInt(15, 150); err != nil {
		return nil, err
	}
	// S1+56 != S2 is required by the protocol (the sizes must not collide
	// with the padded packet header); 56 is far below our range, so any value
	// in 15..150 satisfies it — but keep the check explicit anyway.
	for {
		if p.S2, err = randInt(15, 150); err != nil {
			return nil, err
		}
		if p.S1+56 != p.S2 {
			break
		}
	}
	// Distinct header values from the recommended range 5..2147483647.
	used := make(map[uint32]bool)
	for _, h := range []*uint32{&p.H1, &p.H2, &p.H3, &p.H4} {
		for {
			v, err := randInt(5, 2147483647)
			if err != nil {
				return nil, err
			}
			if !used[uint32(v)] {
				used[uint32(v)] = true
				*h = uint32(v)
				break
			}
		}
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("generated params invalid: %w", err)
	}
	return p, nil
}

// Validate checks the documented AmneziaWG constraints (assuming path MTU of
// 1280, as the official docs do).
func (p *Params) Validate() error {
	if p.V2() {
		return p.validateV2()
	}
	if p.Jc < 1 || p.Jc > 128 {
		return fmt.Errorf("Jc must be in 1..128, got %d", p.Jc)
	}
	if p.Jmin <= 0 || p.Jmin >= p.Jmax || p.Jmax > 1280 {
		return fmt.Errorf("need 0 < Jmin < Jmax <= 1280, got Jmin=%d Jmax=%d", p.Jmin, p.Jmax)
	}
	if p.S1 < 0 || p.S1 > 1132 {
		return fmt.Errorf("S1 must be in 0..1132, got %d", p.S1)
	}
	if p.S2 < 0 || p.S2 > 1188 {
		return fmt.Errorf("S2 must be in 0..1188, got %d", p.S2)
	}
	if p.S1+56 == p.S2 {
		return fmt.Errorf("S1+56 must not equal S2 (got S1=%d S2=%d)", p.S1, p.S2)
	}
	hs := map[uint32]string{p.H1: "H1", p.H2: "H2", p.H3: "H3", p.H4: "H4"}
	if len(hs) != 4 {
		return fmt.Errorf("H1-H4 must be unique, got %d,%d,%d,%d", p.H1, p.H2, p.H3, p.H4)
	}
	for v, label := range hs {
		if v == 0 || v > 2147483647 {
			return fmt.Errorf("%s must be in 1..2147483647, got %d", label, v)
		}
	}
	return nil
}

// validateV2 checks an AWG 2.0 set: junk as in 1.0; paddings within the
// MTU budget; the three fixed-size handshake messages distinguishable by
// size; four non-overlapping header ranges; a parseable I1.
func (p *Params) validateV2() error {
	if p.Jc < 1 || p.Jc > 128 {
		return fmt.Errorf("Jc must be in 1..128, got %d", p.Jc)
	}
	if p.Jmin <= 0 || p.Jmin > p.Jmax || p.Jmax > 1280 {
		return fmt.Errorf("need 0 < Jmin <= Jmax <= 1280, got Jmin=%d Jmax=%d", p.Jmin, p.Jmax)
	}
	if p.S1 < 0 || p.S1 > 1132 || p.S2 < 0 || p.S2 > 1188 || p.S3 < 0 || p.S3 > 1216 || p.S4 < 0 || p.S4 > 64 {
		return fmt.Errorf("paddings out of range: S1=%d (0..1132) S2=%d (0..1188) S3=%d (0..1216) S4=%d (0..64)", p.S1, p.S2, p.S3, p.S4)
	}
	// Handshake initiation 148, response 92, cookie reply 64 bytes.
	sizes := map[int]string{148 + p.S1: "S1", 92 + p.S2: "S2"}
	if _, dup := sizes[64+p.S3]; dup || len(sizes) != 2 {
		return fmt.Errorf("padded handshake sizes collide (S1=%d S2=%d S3=%d)", p.S1, p.S2, p.S3)
	}
	if len(p.HR) != 4 {
		return fmt.Errorf("AWG 2.0 needs exactly 4 header ranges, got %d", len(p.HR))
	}
	for i, r := range p.HR {
		if r.Min < 5 || r.Max < r.Min || r.Max > 2147483647 {
			return fmt.Errorf("H%d range %s out of 5..2147483647", i+1, r)
		}
		for j := 0; j < i; j++ {
			o := p.HR[j]
			if r.Min <= o.Max && o.Min <= r.Max {
				return fmt.Errorf("H%d range %s overlaps H%d range %s", i+1, r, j+1, o)
			}
		}
	}
	if err := ValidateSignature(p.I1); err != nil {
		return fmt.Errorf("I1: %w", err)
	}
	return nil
}

// ValidateSignature checks an I1-I5 tag chain the way amneziawg-go parses
// it: <b 0xHEX>, <r N>, <rc N>, <rd N>, <t>; empty is allowed (no packet).
func ValidateSignature(spec string) error {
	rest := strings.TrimSpace(spec)
	if rest == "" {
		return nil
	}
	size := 0
	for rest != "" {
		if rest[0] != '<' {
			return fmt.Errorf("expected '<' at %q", rest)
		}
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return fmt.Errorf("missing '>' in %q", rest)
		}
		parts := strings.Fields(rest[1:end])
		rest = strings.TrimSpace(rest[end+1:])
		if len(parts) == 0 {
			return fmt.Errorf("empty tag")
		}
		switch parts[0] {
		case "b":
			if len(parts) != 2 {
				return fmt.Errorf("<b> needs one hex argument")
			}
			h := strings.TrimPrefix(parts[1], "0x")
			raw, err := hex.DecodeString(h)
			if err != nil || len(raw) == 0 {
				return fmt.Errorf("<b %s>: bad hex", parts[1])
			}
			size += len(raw)
		case "r", "rc", "rd":
			if len(parts) != 2 {
				return fmt.Errorf("<%s> needs a size", parts[0])
			}
			n, err := strconv.Atoi(parts[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("<%s %s>: bad size", parts[0], parts[1])
			}
			size += n
		case "t":
			size += 4
		default:
			return fmt.Errorf("unknown tag <%s>", parts[0])
		}
	}
	if size > 1280 {
		return fmt.Errorf("signature packet is %d bytes, over the 1280-byte safe MTU", size)
	}
	return nil
}

// GenerateV2 returns a fresh AWG 2.0 parameter set.
func GenerateV2() (*Params, error) {
	base, err := Generate() // Jc/Jmin/Jmax/S1/S2 as in 1.0
	if err != nil {
		return nil, err
	}
	p := &Params{Version: 2, Jc: base.Jc, Jmin: base.Jmin, Jmax: base.Jmax, S1: base.S1, S2: base.S2}
	for {
		if p.S3, err = randInt(15, 56); err != nil {
			return nil, err
		}
		if p.S4, err = randInt(4, 24); err != nil {
			return nil, err
		}
		if 64+p.S3 != 148+p.S1 && 64+p.S3 != 92+p.S2 {
			break
		}
	}
	if p.HR, err = genHeaderRanges(); err != nil {
		return nil, err
	}
	if p.I1, err = QUICInitialSignature(); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("generated awg2 params invalid: %w", err)
	}
	return p, nil
}

// genHeaderRanges splits 5..2^31-1 into four bands, shuffles which message
// type gets which band, and picks a random sub-range inside each — so the
// ranges never overlap and differ from server to server.
func genHeaderRanges() ([]HRange, error) {
	const lo, hi = 5, 2147483647
	band := (hi - lo + 1) / 4
	order := []int{0, 1, 2, 3}
	for i := len(order) - 1; i > 0; i-- { // Fisher-Yates
		j, err := randInt(0, i)
		if err != nil {
			return nil, err
		}
		order[i], order[j] = order[j], order[i]
	}
	out := make([]HRange, 4)
	for i, b := range order {
		bLo := lo + b*band
		start, err := randInt(bLo, bLo+band/2)
		if err != nil {
			return nil, err
		}
		width, err := randInt(band/16, band/3)
		if err != nil {
			return nil, err
		}
		out[i] = HRange{Min: uint32(start), Max: uint32(start + width)}
	}
	return out, nil
}

// QUICInitialSignature builds an I1 chain that looks like a QUIC v1 Initial
// packet: long header (0xc3), version 1, an 8-byte random destination
// connection id, empty source id and token, a length field matching the
// rest, then random "encrypted" payload — 1200 bytes, the minimum size of a
// real client Initial. The fixed prefix differs per server (header byte low
// bits are random), the connection id and payload are fresh per handshake.
func QUICInitialSignature() (string, error) {
	pn, err := randInt(0, 3)
	if err != nil {
		return "", err
	}
	first := 0xc0 | pn
	const total = 1200
	const head = 1 + 4 + 1 + 8 + 1 + 1 + 2 // flags, version, dcid len+dcid, scid len, token len, length
	rest := total - head
	length := 0x4000 | rest // 2-byte QUIC varint
	return fmt.Sprintf("<b 0x%02x0000000108><r 8><b 0x0000%04x><r %d>", first, length, rest), nil
}

// LoadOrGenerate returns the stored parameters of the requested generation
// (1 or 2), generating and persisting them on first use. i1 (FIBR_AWG_I1),
// when set, replaces the stored I1 signature of a 2.0 set — it is
// client-side only, so changing it never breaks existing configs.
func LoadOrGenerate(st *store.Store, version int, i1 string) (*Params, error) {
	if version < 2 {
		return LoadOrGenerateParams(st)
	}
	raw, err := st.GetSetting(settingParamsKeyV2)
	var p *Params
	switch {
	case err == nil:
		p = &Params{}
		if jerr := json.Unmarshal([]byte(raw), p); jerr != nil {
			return nil, fmt.Errorf("stored awg2 params are corrupt: %w", jerr)
		}
		if verr := p.Validate(); verr != nil {
			return nil, fmt.Errorf("stored awg2 params are invalid: %w", verr)
		}
	case err == store.ErrNotFound:
		if p, err = GenerateV2(); err != nil {
			return nil, err
		}
		blob, merr := json.Marshal(p)
		if merr != nil {
			return nil, merr
		}
		if serr := st.SetSetting(settingParamsKeyV2, string(blob)); serr != nil {
			return nil, fmt.Errorf("persist awg2 params: %w", serr)
		}
	default:
		return nil, err
	}
	if i1 = strings.TrimSpace(i1); i1 != "" {
		if verr := ValidateSignature(i1); verr != nil {
			return nil, fmt.Errorf("FIBR_AWG_I1: %w", verr)
		}
		p.I1 = i1
	}
	return p, nil
}

// LoadOrGenerateParams returns the stored server-wide parameters, generating
// and persisting fresh ones on first start. The values must never change
// after clients have been issued configs, so they live in the settings table.
func LoadOrGenerateParams(st *store.Store) (*Params, error) {
	raw, err := st.GetSetting(settingParamsKey)
	switch {
	case err == nil:
		var p Params
		if jerr := json.Unmarshal([]byte(raw), &p); jerr != nil {
			return nil, fmt.Errorf("stored awg params are corrupt: %w", jerr)
		}
		if verr := p.Validate(); verr != nil {
			return nil, fmt.Errorf("stored awg params are invalid: %w", verr)
		}
		return &p, nil
	case err == store.ErrNotFound:
		p, gerr := Generate()
		if gerr != nil {
			return nil, gerr
		}
		blob, merr := json.Marshal(p)
		if merr != nil {
			return nil, merr
		}
		if serr := st.SetSetting(settingParamsKey, string(blob)); serr != nil {
			return nil, fmt.Errorf("persist awg params: %w", serr)
		}
		return p, nil
	default:
		return nil, err
	}
}

// ClientINI renders the parameters in the wg-quick-style INI syntax that
// AmneziaVPN client apps understand (extra keys in the [Interface] section;
// the official WireGuard app rejects them).
func (p *Params) ClientINI() string {
	if p.V2() {
		ini := fmt.Sprintf("Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\nS3 = %d\nS4 = %d\nH1 = %s\nH2 = %s\nH3 = %s\nH4 = %s\n",
			p.Jc, p.Jmin, p.Jmax, p.S1, p.S2, p.S3, p.S4, p.HR[0], p.HR[1], p.HR[2], p.HR[3])
		if p.I1 != "" {
			ini += "I1 = " + p.I1 + "\n"
		}
		return ini
	}
	return fmt.Sprintf("Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\nH1 = %d\nH2 = %d\nH3 = %d\nH4 = %d\n",
		p.Jc, p.Jmin, p.Jmax, p.S1, p.S2, p.H1, p.H2, p.H3, p.H4)
}

// UAPISet renders the parameters as the device part of a cross-platform UAPI
// "set" request (see https://www.wireguard.com/xplatform/), which is how the
// amneziawg-go daemon is configured at runtime.
//
// The I1 signature is deliberately not pushed to the server: it is a
// client-side knob (sent by the handshake initiator), and the server only
// has to recognise the paddings and header ranges.
func (p *Params) UAPISet() string {
	if p.V2() {
		return fmt.Sprintf("jc=%d\njmin=%d\njmax=%d\ns1=%d\ns2=%d\ns3=%d\ns4=%d\nh1=%s\nh2=%s\nh3=%s\nh4=%s",
			p.Jc, p.Jmin, p.Jmax, p.S1, p.S2, p.S3, p.S4, p.HR[0], p.HR[1], p.HR[2], p.HR[3])
	}
	return fmt.Sprintf("jc=%d\njmin=%d\njmax=%d\ns1=%d\ns2=%d\nh1=%d\nh2=%d\nh3=%d\nh4=%d",
		p.Jc, p.Jmin, p.Jmax, p.S1, p.S2, p.H1, p.H2, p.H3, p.H4)
}

func randInt(min, max int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return 0, err
	}
	return min + int(n.Int64()), nil
}

// ParseHeaderSpec parses a UAPI h* value, which may be a single number or a
// "start-end" range, both decimal. Exposed for tests and diagnostics.
func ParseHeaderSpec(spec string) (start, end uint32, err error) {
	dash := -1
	for i := 0; i < len(spec); i++ {
		if spec[i] == '-' {
			dash = i
			break
		}
	}
	parse := func(s string) (uint32, error) {
		v, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return 0, err
		}
		return uint32(v), nil
	}
	if dash < 0 {
		v, err := parse(spec)
		return v, v, err
	}
	start, err = parse(spec[:dash])
	if err != nil {
		return 0, 0, err
	}
	end, err = parse(spec[dash+1:])
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, fmt.Errorf("wrong range %q", spec)
	}
	return start, end, nil
}
