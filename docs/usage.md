# Using Gourdian

[← README](../README.md)

The app has no window of its own; it lives in the **system tray** near the clock. **Left-click** the tray icon for the dashboard; **right-click** it for stats, HUD layout, hiding the HUD, recording, the app folder and Quit.

| In game | Does |
|---|---|
| **Ctrl+Shift+1…5** | Pick your position, until 2:30 |
| **Ctrl+Shift+F10** | Move and resize the HUD: drag it, scroll for size, Ctrl+scroll for background, click Done |
| **Ctrl+Shift+F11** | Open the dashboard |
| **Ctrl+Shift+F9** | Hide or show the HUD |

The F-key shortcuts can be changed on the Settings page.

## Position

Tips, targets and the AI coach depend on your position. When a match starts on a hero, the trainer uses the position you played on it last time, then your most common position on it in your history, then a guess from the hero's roles. The HUD shows the choice until 2:30.

The keys work while you are choosing a hero as well as in the match, and so does the dashboard: a position you set before you queue counts as much as one you press mid-draft. That matters, because pick help is worked out for a position and is offered only while that position is your own answer. Choosing one during the draft reads the advice out again for it, and sticks: taking a hero you usually play elsewhere doesn't quietly put it back.

If you don't pick one, the trainer watches which lane your hero stands in from 0:45 to 2:30 (up to 5:00 if it started late), and switches when that doesn't fit, saying so. Mid lane means mid. In a side lane, early wards or a support position mean the support position, otherwise the core one. A position you pick yourself is never overridden.

## The dashboard

The dashboard is at http://127.0.0.1:4570 and opens in its own window. Its pages:

- **Live**: the tip feed, a briefing before the horn, hero stats, farm pace against your target, core item goals, timers, inventory, the popular build with what to buy next, this week's goals, habits to fix, recent matches and the last review. Position buttons and a voice switch sit at the top.
- **Stats**: charts and tables over your match history, weekly goals and every review.
- **Rules**: every tip rule, built-in and your own — see [Rules](rules.md).
- **HUD**: widgets, order, look and position, with a live preview.
- **AI coach**: connections, who answers live tips and reviews, and what the coach knows about you — see [AI coach](ai-coach.md).
- **Settings**: position per hero, voice, recording, match history import, start with Windows, break reminders, hotkeys and folders.

## HUD

The HUD shows during a match and while you're choosing a hero, never takes focus and lets clicks through to the game. Its content comes from widgets you order and switch on the HUD page:

| Widget | Shows |
|---|---|
| Alerts | The latest tip in large type for a few seconds; choose every tip, warnings and up, or urgent only, and whether AI tips show |
| Position | Before 2:30, the position you're coached as |
| Drill | The habit you're drilling, and how often it happened this game |
| Pick help | While you choose a hero: the heroes worth taking in your position |
| Briefing | Before the horn: your record on the hero, the last-hit target, core item goals and unfinished weekly goals |
| Focus | Your focus from the last review, before the horn and while dead |
| While dead | Respawn time and gold to spend |
| Timers | Runes, neutral items, Roshan and Tormentor, stack pulls, the turn of day and night; how many, which kinds, and how far ahead |
| Last-hit pace | Your last hits against the pace to your target |
| Next item | The next item in the popular build and whether you can buy it |
| Item goal | The next core item and its timing goal, from five minutes before it |
| Stats line | K/D/A, GPM and last hits (off by default) |

Look settings: width, text size, background opacity (0% shows text only), whole-HUD opacity and a text shadow. The HUD is drawn with real transparency, so backgrounds fade without blurring the text.

## Pick help

While you're choosing a hero, the HUD and the dashboard rank the heroes worth taking in your position, and each line says why it's there. Your position is whichever one you last set yourself — in the dashboard while you queue, on the settings page, or with **Ctrl+Shift+1…5** in game. Saying a different one while you're still choosing swaps the advice over and reads it out again.

The one time no heroes are offered is when the position isn't yours: the trainer works one out for itself when a match starts on a hero, from what you played on it last time, and after that it asks rather than hand you carry heroes because that's what you played last night. Which heroes are worth taking depends entirely on the position, so a guess is worth less than a question.

A hero's score starts at an even game and moves with three things:

- **Your own record** on it in that position over the last year, with each match counting half as much every 45 days, so a hero you've drifted away from falls behind one you play now. The window is long on purpose: the fade, not the cutoff, is what decides how much an old game counts, so by the far end a match is worth under a hundredth of a fresh one and no hero drops off a cliff the day its games turn too old. The record is also shrunk towards an even game by how few matches it rests on: four games at 100% is thin evidence and is scored as such, while the list still shows you the plain `100% of 4` so you can judge it yourself.
- **How the hero is doing** in public games at your own rank, from OpenDota. Your medal comes from your Steam account; without it, every rank counts together.
- **Whether the hero suits the position**, a small nudge from the roles OpenDota gives it.

Heroes you've played at least three times are ranked as your own pool. Under them sit up to two heroes doing well at your rank that you *don't* play, kept in their own list so nothing ever quietly tells you to first-pick a hero you've never touched. Heroes you win under 40% on are listed as ones to avoid.

All of those numbers are yours to change, on Settings › **Pick help**, because what counts as recent depends on how much you play:

| Setting | Default | What it does |
|---|---|---|
| Look back over | 365 days | How much history is read at all |
| A match counts half after | 45 days | How fast an old game fades. Shorter follows your current form; longer forgives a break |
| Trust a record after | 20 games | Below this, a win rate is pulled towards an even game. 0 takes every record at face value, so a 4–0 run outranks a long steady one |
| Rank a hero after | 3 games | Fewer games than this and a hero isn't ranked |
| Avoid below | 40% won | The line for the list of heroes to avoid |
| Show | 4 / 2 / 2 | How many of yours, from the meta, and to avoid. Set the meta list to 0 to only ever be shown heroes you play |

**Reset** puts them all back. How much each part of the score is allowed to move a hero — your record against the patch against the hero's roles — is fixed, since that's the shape of the model rather than a matter of taste.

The top of the list is read aloud for each position you name, and again once the other side has finished each wave of picks, since that is when the advice has changed. Each reading starts with who they have, so you can hear whether the trainer is reading your screen correctly. The AI coach adds a sentence on which to take (AI page › **A word on your pick**), and it answers again on each wave too — its first answer is made before anyone has picked.

Once you take a hero the advice about which to take goes, since it is settled, but the draft stays up: who the other side has taken and what the two line-ups are short of keep filling in while the rest of them pick, which is what tells you what to buy and what to expect. It all goes when the game starts.

### Where the enemy picks come from

Valve sends the draft to spectators and not to players, so Dota's own feed tells your tools nothing about who the other side took. `gourdian doctor` says so, and will say otherwise if that ever changes.

What is left is the screen. Settings › Pick help › **Read the enemy picks off your screen** turns on reading the ten hero portraits Dota draws along the top of the game: the same pixels a screenshot would take, from the strip those portraits sit in, only while you are choosing a hero. It is off until you ask for it.

Once it is on, the heroes the other side has taken are shown on the pick card, and each hero you might take is weighed against them — a line like `+4% against their picks`. That weighing is deliberately gentle: OpenDota's record of two heroes meeting is a hundred-odd games measured across every position, so it orders heroes that are otherwise level rather than choosing one for you.

It recognises the portraits by their colours, against the art from your own Dota install, so an Arcana, a persona or an alternate style is read as the hero it is. It says nothing rather than guessing: an unpicked slot, a hero it isn't sure of, or the same hero seemingly in two places are all left blank, and a hero has to be read the same way twice before it is believed. Measured over three drafts, all ten heroes were read within two to three readings, none wrongly.

Once a few heroes are picked on either side, it also says what the two line-ups are short of or heavy in — three of them able to stun you, nobody on your side who can take a beating, a side that is all melee. Those come from nothing more than the roles OpenDota tags each hero with, so they are the sort of thing anyone would say looking at the board rather than a judgement of the draft, and nothing is said until at least four heroes of a side are known.

It needs the game drawn where a program can read it: on Linux that means an X11 session, not Wayland. `gourdian doctor` says whether it is on, whether this machine can read its screen, and where it expects the portraits to be.

## Personal targets

- **Last hits**: for carry, mid and offlane, each checkpoint (5, 10, 15, 20 and 30 minutes) aims 10% above your median over your last 10 matches on that hero and position, once you have 3 of them. Otherwise it uses a fixed table for the position. Imported matches count.
- **Core items**: two items, your usual first big items on the hero if you have 3 or more matches with item data, otherwise the popular build's first mid- or late-game items costing 2,000 gold or more. The goal is the earliest OpenDota timing bucket that holds a tenth of games and wins at least as often as the item does overall, or a minute before your own median timing, whichever is later.
- The item goal rule gives a heads-up 2 minutes before a goal, marks the item late a minute after it (unless you finished a different item of similar cost instead), and says how your timing went.

## Weekly goals and break reminders

Each match review sets one to three goals for the week from measurable metrics: last hits at a checkpoint, deaths, GPM, XPM, kills plus assists, or how often a habit's warning fired. After every real match the trainer says which goals you met, and a goal is done after five matches that meet it. The briefing, Live page and Stats page show progress.

After two losses in a row, three of the last four games lost, or an MMR drop of 50 or more within a session (matches less than 90 minutes apart), the trainer suggests a break. If you queue again within 10 minutes, it gives one calm-down reminder at the start. Turn this off under Settings › Break reminders.

## Troubleshooting

- **Dashboard says "Waiting for Dota 2"**: the Setup card lists what's missing, with a Fix button for Dota's game-state config. Restart Dota after fixing it.
- **No HUD over the game**: Dota must be in borderless window mode. If a hotkey is taken by another program, Settings says so; pick another one.
- **AI coach paused**: follow the banner, or open the AI coach page and click Check.
- **Something went wrong**: read `logs\trainer.log` in the app folder, or run `"Gourdian.exe" doctor` from a terminal there.
- **Coaching in another language**: add "Answer in Russian" (or any language) under "How the coach should talk to you". The AI's tips and reviews follow it; built-in tips stay in English, and Windows only speaks languages with an installed voice.
- **Left a game early?** Matches that stop sending updates for 3 minutes are still recorded, with the result marked unknown.
