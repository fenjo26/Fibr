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
		Processes: []string{"cs2.exe", "cs2"},
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
		Processes: []string{"dota2.exe", "dota2"},
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
		Processes: []string{"LeagueClient.exe", "League of Legends.exe"},
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
		Processes: []string{"VALORANT-Win64-Shipping.exe", "VALORANT.exe", "RiotClientServices.exe"},
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
		ID:        "apex",
		Name:      "Apex Legends",
		NameRu:    "Apex Legends",
		Processes: []string{"r5apex.exe", "r5apex_dx12.exe"},
		CIDRCount: 3,
		Updated:   snapshotDate,
		CIDRs: []string{
			"159.153.0.0/16",
			"178.162.192.0/19",
			"185.179.200.0/22",
		},
	},
	{
		ID:        "blizzard",
		Name:      "Blizzard / Overwatch 2",
		NameRu:    "Blizzard / Overwatch 2",
		Processes: []string{"Overwatch.exe", "Battle.net.exe"},
		CIDRCount: 6,
		Updated:   snapshotDate,
		CIDRs: []string{
			"24.105.0.0/18",
			"24.105.32.0/19",
			"37.244.0.0/18",
			"137.221.0.0/16",
			"158.115.192.0/19",
			"185.60.112.0/22",
		},
	},
	{
		ID:        "pubg",
		Name:      "PUBG: Battlegrounds",
		NameRu:    "PUBG: Battlegrounds",
		Processes: []string{"TslGame.exe", "TslGame_UC.exe"},
		CIDRCount: 3,
		Updated:   snapshotDate,
		CIDRs: []string{
			"18.194.0.0/15",
			"35.156.0.0/15",
			"52.57.0.0/16",
		},
	},
	{
		ID:        "r6",
		Name:      "Rainbow Six Siege",
		NameRu:    "Rainbow Six Siege",
		Processes: []string{"RainbowSix.exe", "RainbowSix_Vulkan.exe"},
		CIDRCount: 3,
		Updated:   snapshotDate,
		CIDRs: []string{
			"162.247.16.0/22",
			"185.38.20.0/22",
			"216.98.48.0/20",
		},
	},
	{
		ID:        "rust",
		Name:      "Rust",
		NameRu:    "Rust",
		Processes: []string{"RustClient.exe"},
		CIDRCount: 3,
		Updated:   snapshotDate,
		CIDRs: []string{
			"104.238.128.0/20",
			"185.38.148.0/22",
			"192.223.24.0/22",
		},
	},
	{
		ID:        "standoff2",
		Name:      "Standoff 2",
		NameRu:    "Standoff 2",
		Processes: []string{"Standoff2.exe", "com.axlebolt.standoff2"},
		CIDRCount: 3,
		Updated:   snapshotDate,
		CIDRs: []string{
			"92.38.128.0/17",
			"92.223.0.0/16",
			"188.166.0.0/18",
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
