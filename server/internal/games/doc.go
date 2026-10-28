// Package games serves the catalog of games whose server traffic can be
// split-tunnelled through a dedicated WireGuard peer: per-game CIDR ranges
// that become the AllowedIPs of the gaming tunnel (see docs/dns-games.md §2).
//
// The catalog boots from a builtin snapshot (snapshot.go), upgrades to the
// on-disk cache DataDir/games.json when one exists, and refreshes from the
// live sources every 24 hours with per-source failover. A failed refresh
// never empties the catalog: the previous ranges keep serving until a later
// refresh succeeds.
//
// # Wiring (cmd/fibr/main.go)
//
//	cat := games.NewCatalog(games.Options{DataDir: cfg.DataDir})
//	go cat.Run(ctx)
//
//	mux.HandleFunc("GET /api/games", func(w http.ResponseWriter, r *http.Request) {
//		writeJSON(w, cat.List())
//	})
//	mux.HandleFunc("GET /api/games/routes", func(w http.ResponseWriter, r *http.Request) {
//		ids := strings.Split(r.URL.Query().Get("ids"), ",")
//		writeJSON(w, cat.Routes(ids)) // unknown/empty ids are silently skipped
//	})
//
// # Responses
//
// GET /api/games — the catalog without the CIDR arrays:
//
//	[
//	  {"id":"cs2","name":"Counter-Strike 2","name_ru":"Counter-Strike 2",
//	   "cidr_count":47,"updated":"2026-10-28T00:00:00Z"},
//	  {"id":"dota2","name":"Dota 2","name_ru":"Dota 2",
//	   "cidr_count":47,"updated":"2026-10-28T00:00:00Z"},
//	  {"id":"lol","name":"League of Legends","name_ru":"League of Legends",
//	   "cidr_count":15,"updated":"2026-10-28T00:00:00Z"},
//	  {"id":"valorant","name":"Valorant","name_ru":"Valorant",
//	   "cidr_count":15,"updated":"2026-10-28T00:00:00Z"}
//	]
//
// GET /api/games/routes?ids=cs2,dota2 — the ranges for the client's
// AllowedIPs:
//
//	{
//	  "cs2":   ["45.113.191.0/24", "45.121.184.0/24", "..."],
//	  "dota2": ["45.113.191.0/24", "45.121.184.0/24", "..."]
//	}
//
// Ranges are validated (net.ParseCIDR), canonicalised, de-duplicated,
// bogon-filtered and numerically sorted before they enter the catalog.
package games
