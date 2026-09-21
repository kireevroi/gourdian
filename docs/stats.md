# Statistics

[← README](../README.md)

Everything lives in `trainer.data` in the app folder: matches, per-minute samples, tips, item timings, MMR, reviews, goals and your rules.

- **Stats page**: MMR and the time to a target rank, rolling win rate, last hits at 10:00, GPM, deaths, last-hit curves, mistakes per match, weekly goals, reviews and a hero table.
- **Import**: Settings › Match history adds your last 50 matches from OpenDota (your Friend ID is learned in your first match). OpenDota needs Expose Public Match Data turned on in Dota.
- **Recordings**: every match's game data is saved to `recordings\` (the newest 20 are kept), so rules can be tested on real games.

## Time to a rank

Pick a **Target rank** on the MMR chart and the page tells you how far away it is and how long
the climb takes at your recent pace. The target also shows as a line on the chart.

The pace comes from your MMR log. It is measured from the entry about 30 days before your
latest one (or your first entry, if the log is newer than that) up to the latest:

- **Matches**: the MMR you gained, divided by the ranked matches you played in that time. The
  estimate needs at least 5 matches. A match counts when OpenDota says it was ranked or when you
  logged MMR against it. Matches you played without the trainer running count only after you
  import them.
- **Days**: the MMR you gained, divided by the days the stretch covers, counted from today. The
  estimate needs at least a week of log. Breaks between sessions are part of the pace, so it
  only holds if you keep playing as often as you have been.

If your MMR has gone down or stayed flat over that time, the page says so and gives no
estimate.

Valve doesn't publish the MMR each medal starts at, so these are the thresholds players have
measured: 154 MMR a star from Herald to Ancient (Legend 1 is 3080), 200 a star in Divine
(Divine 1 is 4620) and Immortal from 5620. The squish in patch 7.41e only rescaled MMR inside
Immortal, so none of these moved.

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

## Turbo

A Turbo game pays about twice the gold and experience of a normal one, so its GPM, XPM, last
hits and item timings say nothing about anything else. Mixed together they describe neither:
a median across both lands somewhere no game of either kind ever reaches.

So Turbo is recorded and then left out of everything the trainer works out from history —
personal targets, last-hit pace, item timing goals, hero win rates, the habits and averages on
the dashboard. Two things still count it, because they aren't about how much a game pays: the
break reminder, since a run of losses is a run of losses, and the match review and MMR prompt,
which a Turbo game gets like any other.

The statistics page offers **Mode** once you have played one, and shows the two apart rather
than together. The CSV export carries `game_mode` and `turbo` columns so you can split it
yourself.

Which mode a match was comes from OpenDota a couple of minutes after it ends; Dota's own live
feed doesn't say. That means the targets you are coached against *during* a Turbo game are
still the normal-game ones — there is no way for the trainer to know what it is looking at
until afterwards. Matches recorded before this version are looked up once, in the background,
so an old history stops dragging the numbers.
