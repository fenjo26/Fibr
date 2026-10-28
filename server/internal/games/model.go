package games

import "time"

// Game is one entry of the game catalog: identifying names plus the CIDR
// ranges of that game's servers, used as the AllowedIPs of a split-tunnel
// (see docs/dns-games.md §2).
//
// In listing responses the CIDR array is omitted (json:"cidrs,omitempty")
// and only cidr_count is exposed; the ranges themselves are served per-game
// via Catalog.Routes.
type Game struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	NameRu    string    `json:"name_ru"`
	Processes []string  `json:"processes,omitempty"`
	CIDRs     []string  `json:"cidrs,omitempty"`
	CIDRCount int       `json:"cidr_count"`
	Updated   time.Time `json:"updated"`
}
