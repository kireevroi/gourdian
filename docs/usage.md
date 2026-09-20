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

If you don't pick one, the trainer watches which lane your hero stands in from 0:45 to 2:30 (up to 5:00 if it started late), and switches when that doesn't fit, saying so. Mid lane means mid. In a side lane, early wards or a support position mean the support position, otherwise the core one. A position you pick yourself is never overridden.

## The dashboard

The dashboard is at http://127.0.0.1:4570 and opens in its own window. Its pages:

- **Live**: the tip feed, a briefing before the horn, hero stats, farm pace against your target, core item goals, timers, inventory, the popular build with what to buy next, this week's goals, habits to fix, recent matches and the last review. Position buttons and a voice switch sit at the top.
- **Stats**: charts and tables over your CSV statistics, weekly goals and every review.
- **Rules**: every tip rule, built-in and your own — see [Rules](rules.md).
- **HUD**: widgets, order, look and position, with a live preview.
- **AI coach**: connections, who answers live tips and reviews, and what the coach knows about you — see [AI coach](ai-coach.md).
- **Settings**: position per hero, voice, recording, match history import, start with Windows, break reminders, hotkeys and folders.

## HUD

The HUD shows only during a match, never takes focus and lets clicks through to the game. Its content comes from widgets you order and switch on the HUD page:

| Widget | Shows |
|---|---|
| Alerts | The latest tip in large type for a few seconds; choose every tip, warnings and up, or urgent only, and whether AI tips show |
| Position | Before 2:30, the position you're coached as |
| Briefing | Before the horn: your record on the hero, the last-hit target, core item goals and unfinished weekly goals |
| Focus | Your focus from the last review, before the horn and while dead |
| While dead | Respawn time and gold to spend |
| Timers | Runes, neutral items, Roshan and Tormentor, stack pulls; how many, which kinds, and how far ahead |
| Last-hit pace | Your last hits against the pace to your target |
| Next item | The next item in the popular build and whether you can buy it |
| Item goal | The next core item and its timing goal, from five minutes before it |
| Stats line | K/D/A, GPM and last hits (off by default) |

Look settings: width, text size, background opacity (0% shows text only), whole-HUD opacity and a text shadow. The HUD is drawn with real transparency, so backgrounds fade without blurring the text.

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
