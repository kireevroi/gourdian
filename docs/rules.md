# Rules

[← README](../README.md)

The Rules page on the dashboard lists every rule with how often it fired in your last 10 matches.

**Built-in rules** can be switched off, limited to certain positions, given another severity, made silent or always spoken, and retuned: how long a problem must last before a warning, how often it repeats, the HP percentage, the gold threshold and so on. **Reset to default** undoes the changes.

**Your own rules** are cards:

- **When**: while the conditions hold (for some seconds), the moment they become true, when something happens (the horn, you die or respawn, level up, get a kill, get an item, Roshan dies, the Aegis is picked up), or at game times (first, every, until, and how early to warn).
- **If**: all or any of a list of conditions on about 50 game values: clock, gold, HP and mana, level, K/D/A, last hits against pace, items owned, in the stash or ready, charges, abilities and ultimate ready, buyback, being in base, your position, and more.
- **Then**: the text to show, what to say (or nothing), the severity, whether it counts as a mistake in your habits, and how often it may repeat. Text can include values like `{gold}`, `{clock}`, `{respawn}` or `{item_name:black_king_bar}`.

While a match runs, each condition shows its current value and whether it holds. **Run on recording** replays a recorded match through the rule and lists every time it would have fired.

Start from templates (Save for BKB, use Magic Wand when low, low mana, Lotus pool, night falls, shop while dead, ultimate is up, idle in base), duplicate rules, and export or import them as JSON.

Rules are stored in `trainer.data` with everything else. Built-in defaults come with the app, so updates can improve them unless you changed that rule.
