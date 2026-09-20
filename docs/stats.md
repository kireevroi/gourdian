# Statistics

[← README](../README.md)

Everything lives in `trainer.data` in the app folder: matches, per-minute samples, tips, item timings, MMR, reviews, goals and your rules.

- **Stats page**: MMR, rolling win rate, last hits at 10:00, GPM, deaths, last-hit curves, mistakes per match, weekly goals, reviews and a hero table.
- **Import**: Settings › Match history adds your last 50 matches from OpenDota (your Friend ID is learned in your first match). OpenDota needs Expose Public Match Data turned on in Dota.
- **Recordings**: every match's game data is saved to `recordings\` (the newest 20 are kept), so rules can be tested on real games.

## CSV export

**Settings › Export CSV** writes it all to the `stats\` folder for spreadsheets:

| File | One row per | Useful for |
|---|---|---|
| `matches.csv` | match | results, KDA, GPM/XPM, last hits at checkpoints, death times, each habit's warnings (`mistakes_<rule>`), rank, and after OpenDota parses it: lane, net worth, damage, wards, stacks, teamfight participation, percentiles, enemy heroes |
| `timeline.csv` | game minute | gold, last hits, GPM, level and deaths over time |
| `tips.csv` | tip | what the coach told you and when |
| `items.csv` | core item bought | item timings, seen live (`gsi`) or from OpenDota |
| `mmr.csv` | MMR entry | your rating over time |
| `reviews.csv` | reviewed match | the AI's summary, improvements and next-game focus |
| `goals.csv` | weekly goal | the goals reviews set |

`source` in `matches.csv` is `live`, `opendota` (imported), `practice` (lobby and bot games, which Dota reports as match 0) or `sim`. Practice and simulated matches are left out of trends, habits and AI history unless you tick them in.

## Patch timings

Map timings are built into each version; updating the app brings new patch timings. This version follows **patch 7.41f**, checked against Valve's patch notes (the `dota2.com/datafeed/patchnotes` feed) and Liquipedia:

| What | When | Since |
|---|---|---|
| Bounty runes | 0:00, then every 4:00 | 7.38 (was every 3:00) |
| Water runes | 2:00 and 4:00 | |
| Power runes | 6:00, then every 2:00 | |
| Shrines of Wisdom (replaced wisdom runes) | every 7:00; stand in yours for 3 s, an enemy reverses the countdown | 7.38, 7.41 |
| Lotus pools | a lotus every 3:00, six at most | |
| Day and night | day from 0:00, then five minutes each | |
| Tormentor | 20:00, then 10:00 after it dies | 7.39 (was 15:00) |
| Aghanim's Shard | on sale from 15:00 for 1,400 gold; a Tormentor drops one too | |
| Neutral items | crafted from Madstone; tiers at 5, 15, 25, 35 and 60 minutes, tier 1 costs 5 Madstone | 7.38 |
| Roshan | respawns 8–11 minutes after he dies; the Aegis lasts 5:00 | |
| Skill points | one per level; talents have their own points at 10, 15, 20, 25 and 27–30 | 7.40 |

"Keep buyback gold from 30:00" is coaching advice, not a game rule.
