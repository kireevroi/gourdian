# AI coach

[← README](../README.md)

The AI coach asks for situational advice every few minutes and when you die, and reviews every match afterwards with three improvements, a focus for the next game and measurable goals for the week.

## Providers

The AI coach page lists every provider with its status and a fix button:

| Provider | Uses | Setup |
|---|---|---|
| Claude Code | your Claude subscription | Install and connect (downloads it, then opens `claude auth login`); **Switch account** signs out and signs in as someone else |
| OpenAI Codex CLI | your ChatGPT subscription | Install and connect (downloads it, then opens `codex login`); **Switch account** signs out and signs in as someone else |
| Anthropic API | an API key, billed per request | paste the key |
| OpenAI, OpenRouter, Google Gemini API, Groq, DeepSeek | an API key, billed per request | paste the key |
| Custom OpenAI-compatible URL | your own server, such as Ollama or LM Studio | enter the URL and, if needed, a key |

Choose a provider, model and thinking effort for live tips and for reviews separately, plus an optional fallback that answers while the main one is logged out or out of usage. **Test** sends a sample request and shows the answer and how long it took. API keys are encrypted for your Windows account (DPAPI) in `secrets.json` and never shown again in full.

## When a provider fails

**Check** on a provider forgets an old usage limit or logout before asking again, so a fresh account is picked up straight away. If a provider is logged out, rejects its key or hits a usage limit, the coach pauses instead of failing silently: the dashboard shows a banner with a login button, and the next match starts with one spoken notice. A logged-out provider is re-checked every 5 minutes and resumes by itself; a usage limit pauses it for 10 minutes. Reviews that couldn't be written are retried once it works again.

## What gets sent

Your hero's live stats, items, timers and recent tips, your per-minute timeline, your averages and recurring mistakes, your MMR log, this week's goals and your "About you" text. After a real match the trainer waits up to 30 minutes for OpenDota to parse the replay, so the review also gets your lane, lane opponents, both teams' heroes, percentiles against other players of the hero and item timings. Nothing else about other players is available to send.

The coach is given the item build professional players use on your hero (OpenDota's item popularity, "analyzed from professional games") and is told to recommend only items from it.

Claude CLI calls run with tools, MCP servers and settings files off. Codex runs read-only in an empty folder, with the answer's shape enforced by a JSON schema.

## Installs

Installs come from the publishers themselves: Claude Code from `downloads.claude.ai`, verified against the SHA-256 in its release manifest and then run with `claude install`; the Codex CLI from its GitHub release, saved in the app folder's `tools\`. Both are single native programs, so the trainer never installs Node.js or touches anything else on the PC. The Gemini CLI is not offered, because it is the one that would need Node.js; the Google Gemini API key works the same as any other key.
