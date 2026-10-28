package games

import "time"

// Builtin snapshot of game-server CIDR ranges, generated 2026-10-28 (UTC).
//
// Sources (fetched live on that date):
//   - cs2, dota2 — Valve matchmaking network: union of the two community
//     lists https://raw.githubusercontent.com/denniskupec/valve-matchmaking-ip-ranges/master/old%20list.md
//     (mm-range.txt in that repo was deleted; "old list.md" is the live file)
//     and https://github.com/ryad-lindner/csgo-matchmaking-ip-ranges (README),
//     cross-checked against the announcements of Valve's AS32590 via RIPEstat
//     (https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS32590):
//     24 of the /24s below are announced (or covered by an announced
//     aggregate) by AS32590 itself; the rest are partner-hosted regional POPs
//     (South Africa, South America, India, Japan, Peru) that both community
//     lists agree on, which is what the cross-check distinguishes them by.
//   - lol, valorant — Riot Games AS6507 via RIPEstat
//     (https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS6507),
//     with adjacent announced blocks aggregated into covering supernets that
//     span only announced space (e.g. 151.106.246.0/22 = the four announced
//     /24s .246-.249).
//
// Policy decisions baked into this list:
//   - Steam content-CDN blocks (205.196.6.0/24 and 208.64.200.0/24 through
//     208.64.203.0/24, announced by AS32590) are EXCLUDED on purpose: the
//     game tunnel must carry match traffic only, not game downloads.
//   - Single-host /32 relics from the community lists (197.80.4.37/32,
//     116.202.224.146/32, 61.14.157.158/32) are dropped in favour of the
//     covering POP ranges.
//   - Private/bogon entries that appear in the mirrors (10.130.205.0/24,
//     10.156.7.0/24 in the ryad-lindner README) are filtered out.
//   - cs2 and dota2 share the same Valve matchmaking network; lol and
//     valorant share Riot's AS6507 — identical ranges, kept as separate ids.
//
// The catalog refreshes from the live sources every 24h (DefaultSources);
// this snapshot only boots the server before the first successful refresh
// and serves as the offline fallback.
var builtinSnapshot = []Game{
	{
		ID:        "cs2",
		Name:      "Counter-Strike 2",
		NameRu:    "Counter-Strike 2",
		CIDRCount: 47,
		Updated:   snapshotDate,
		CIDRs: []string{
			"45.113.191.0/24",
			"45.121.184.0/24",
			"45.121.185.0/24",
			"45.121.186.0/24",
			"45.121.187.0/24",
			"103.10.124.0/24",
			"103.10.125.0/24",
			"103.28.54.0/24",
			"103.28.55.0/24",
			"143.137.146.0/24",
			"146.66.152.0/24",
			"146.66.153.0/24",
			"146.66.155.0/24",
			"146.66.156.0/24",
			"146.66.157.0/24",
			"146.66.158.0/24",
			"146.66.159.0/24",
			"152.111.192.0/24",
			"155.133.228.0/24",
			"155.133.229.0/24",
			"155.133.240.0/24",
			"155.133.241.0/24",
			"155.133.242.0/24",
			"155.133.243.0/24",
			"155.133.244.0/24",
			"155.133.246.0/24",
			"155.133.247.0/24",
			"155.133.248.0/24",
			"155.133.249.0/24",
			"162.254.194.0/24",
			"162.254.199.0/24",
			"180.149.41.0/24",
			"185.25.180.0/24",
			"185.25.181.0/24",
			"185.25.182.0/24",
			"185.25.183.0/24",
			"192.69.96.0/24",
			"192.69.97.0/24",
			"196.38.180.0/24",
			"197.80.200.0/24",
			"197.84.209.0/24",
			"205.185.194.0/24",
			"208.78.164.0/24",
			"208.78.165.0/24",
			"208.78.166.0/24",
			"209.197.25.0/24",
			"209.197.29.0/24",
		},
	},
	{
		ID:        "dota2",
		Name:      "Dota 2",
		NameRu:    "Dota 2",
		CIDRCount: 47,
		Updated:   snapshotDate,
		CIDRs: []string{
			"45.113.191.0/24",
			"45.121.184.0/24",
			"45.121.185.0/24",
			"45.121.186.0/24",
			"45.121.187.0/24",
			"103.10.124.0/24",
			"103.10.125.0/24",
			"103.28.54.0/24",
			"103.28.55.0/24",
			"143.137.146.0/24",
			"146.66.152.0/24",
			"146.66.153.0/24",
			"146.66.155.0/24",
			"146.66.156.0/24",
			"146.66.157.0/24",
			"146.66.158.0/24",
			"146.66.159.0/24",
			"152.111.192.0/24",
			"155.133.228.0/24",
			"155.133.229.0/24",
			"155.133.240.0/24",
			"155.133.241.0/24",
			"155.133.242.0/24",
			"155.133.243.0/24",
			"155.133.244.0/24",
			"155.133.246.0/24",
			"155.133.247.0/24",
			"155.133.248.0/24",
			"155.133.249.0/24",
			"162.254.194.0/24",
			"162.254.199.0/24",
			"180.149.41.0/24",
			"185.25.180.0/24",
			"185.25.181.0/24",
			"185.25.182.0/24",
			"185.25.183.0/24",
			"192.69.96.0/24",
			"192.69.97.0/24",
			"196.38.180.0/24",
			"197.80.200.0/24",
			"197.84.209.0/24",
			"205.185.194.0/24",
			"208.78.164.0/24",
			"208.78.165.0/24",
			"208.78.166.0/24",
			"209.197.25.0/24",
			"209.197.29.0/24",
		},
	},
	{
		ID:        "lol",
		Name:      "League of Legends",
		NameRu:    "League of Legends",
		CIDRCount: 15,
		Updated:   snapshotDate,
		CIDRs: []string{
			"43.229.64.0/22",
			"45.7.36.0/22",
			"45.250.208.0/22",
			"103.219.128.0/22",
			"103.240.224.0/22",
			"104.160.128.0/19",
			"138.0.12.0/22",
			"151.106.246.0/22",
			"151.106.250.0/22",
			"151.106.254.0/24",
			"162.249.72.0/21",
			"185.40.64.0/22",
			"192.64.168.0/21",
			"192.207.0.0/24",
			"2a04:82c0::/29",
		},
	},
	{
		ID:        "valorant",
		Name:      "Valorant",
		NameRu:    "Valorant",
		CIDRCount: 15,
		Updated:   snapshotDate,
		CIDRs: []string{
			"43.229.64.0/22",
			"45.7.36.0/22",
			"45.250.208.0/22",
			"103.219.128.0/22",
			"103.240.224.0/22",
			"104.160.128.0/19",
			"138.0.12.0/22",
			"151.106.246.0/22",
			"151.106.250.0/22",
			"151.106.254.0/24",
			"162.249.72.0/21",
			"185.40.64.0/22",
			"192.64.168.0/21",
			"192.207.0.0/24",
			"2a04:82c0::/29",
		},
	},
}

// snapshotDate is the generation date stamped on every builtin entry.
var snapshotDate = time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)

// builtinGames returns a sanitized deep copy of the snapshot (the copy
// function re-derives CIDRCount, so hardcoded counts cannot drift).
func builtinGames() []Game {
	out := make([]Game, len(builtinSnapshot))
	for i, g := range builtinSnapshot {
		g.CIDRs = normalize(g.CIDRs)
		g.CIDRCount = len(g.CIDRs)
		out[i] = g
	}
	return out
}
