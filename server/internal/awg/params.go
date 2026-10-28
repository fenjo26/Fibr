// Package awg implements AmneziaWG support: generation and persistence of the
// obfuscation parameters (Jc/Jmin/Jmax/S1/S2/H1-H4), a small client for the
// amneziawg-go UAPI socket and lifecycle management of the userspace daemon.
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
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	"github.com/fenjo26/Fibr/internal/store"
)

const settingParamsKey = "awg_params"

// Params holds the AmneziaWG obfuscation parameters shared by the server
// daemon and every client config.
type Params struct {
	Jc   int    `json:"jc"`   // junk packet count before each handshake: 1..128 (recommended 4..12)
	Jmin int    `json:"jmin"` // minimum junk packet size: < Jmax, < 1280
	Jmax int    `json:"jmax"` // maximum junk packet size: > Jmin, <= 1280
	S1   int    `json:"s1"`   // initiation packet padding: <= 1132, S1+56 != S2 (recommended 15..150)
	S2   int    `json:"s2"`   // response packet padding: <= 1188 (recommended 15..150)
	H1   uint32 `json:"h1"`   // magic header override bytes, must be unique among H1-H4
	H2   uint32 `json:"h2"`
	H3   uint32 `json:"h3"`
	H4   uint32 `json:"h4"`
}

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
	return fmt.Sprintf("Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\nH1 = %d\nH2 = %d\nH3 = %d\nH4 = %d\n",
		p.Jc, p.Jmin, p.Jmax, p.S1, p.S2, p.H1, p.H2, p.H3, p.H4)
}

// UAPISet renders the parameters as the device part of a cross-platform UAPI
// "set" request (see https://www.wireguard.com/xplatform/), which is how the
// amneziawg-go daemon is configured at runtime.
func (p *Params) UAPISet() string {
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
