package dota

// Timings are in-game clock seconds. They ship with each app version instead of being
// user settings, so updating the app is how they follow a patch.
type Timings struct {
	BountyRuneEvery  int   `json:"bounty_rune_every"`
	WaterRunes       []int `json:"water_runes"`
	PowerRuneFirst   int   `json:"power_rune_first"`
	PowerRuneEvery   int   `json:"power_rune_every"`
	WisdomRuneEvery  int   `json:"wisdom_rune_every"`
	LotusEvery       int   `json:"lotus_every"`
	DayNightEvery    int   `json:"day_night_every"`
	TormentorSpawn   int   `json:"tormentor_spawn"`
	NeutralTiers     []int `json:"neutral_tiers"`
	ShardFrom        int   `json:"shard_from"`
	RoshanRespawnMin int   `json:"roshan_respawn_min"`
	RoshanRespawnMax int   `json:"roshan_respawn_max"`
	AegisDuration    int   `json:"aegis_duration"`
	GlyphCooldown    int   `json:"glyph_cooldown"`
	BuybackFrom      int   `json:"buyback_from"`
}

// DefaultTimings are the map timings of patch 7.41f, checked against Valve's patch notes
// (dota2.com/datafeed/patchnotes) and Liquipedia on 2026-09-18.
func DefaultTimings() Timings {
	return Timings{
		BountyRuneEvery:  240,                               // 0:00, then every 4:00 since 7.38
		WaterRunes:       []int{120, 240},                   // 2:00 and 4:00
		PowerRuneFirst:   360,                               // 6:00
		PowerRuneEvery:   120,                               // then every 2:00
		WisdomRuneEvery:  420,                               // Shrines of Wisdom every 7:00 since 7.38
		LotusEvery:       180,                               // a lotus every 3:00, six at most
		DayNightEvery:    300,                               // day from 0:00, night from 5:00, and so on
		TormentorSpawn:   1200,                              // 20:00 since 7.39, then 10:00 after it dies
		NeutralTiers:     []int{300, 900, 1500, 2100, 3600}, // Madstone cap rises at 5/15/25/35/60 min
		ShardFrom:        900,                               // Aghanim's Shard goes on sale at 15:00
		RoshanRespawnMin: 480,                               // 8 to 11 minutes after he dies
		RoshanRespawnMax: 660,
		AegisDuration:    300,
		GlyphCooldown:    300,  // or less: losing a tower or barracks brings the Glyph back
		BuybackFrom:      1800, // coaching choice, not a game rule: keep buyback gold from 30:00
	}
}

// ShardItem is Aghanim's Shard under OpenDota's item names.
const ShardItem = "aghanims_shard"

// ShardCost is what Aghanim's Shard costs when OpenDota's item prices haven't loaded yet.
const ShardCost = 1400
