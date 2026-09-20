import { $, esc, ROLE_NAMES, api, toast, t, tp, sayInBrowser, events, onSettings, setSettings, saveSettings, positionButtons, cfg } from './app.js';

positionButtons($('positions'));
const HOTKEYS = [['hud_edit', 'Move, resize and fade the HUD'], ['dashboard', 'Open this dashboard'], ['hud_toggle', 'Hide or show the HUD']];
let capturing = null;

// keyName turns a keydown into the key part of a shortcut, or '' for keys that can't be one.
function keyName(e) {
  const c = e.code;
  if (/^F\d{1,2}$/.test(c)) return c;
  if (/^Key[A-Z]$/.test(c)) return c.slice(3);
  if (/^Digit\d$/.test(c)) return c.slice(5);
  if (/^Numpad\d$/.test(c)) return `Num${c.slice(6)}`;
  return ['Home', 'End', 'PageUp', 'PageDown', 'Insert', 'Delete', 'Space', 'Pause'].includes(c) ? c : '';
}

$('hotkeys').addEventListener('click', (e) => {
  const key = e.target.dataset.hotkey;
  if (!key) return;
  capturing = { key, button: e.target };
  e.target.classList.add('capturing');
  e.target.textContent = 'Press keys… (Esc cancels)';
});
window.addEventListener('keydown', async (e) => {
  if (!capturing) return;
  e.preventDefault();
  e.stopPropagation();
  if (e.code === 'Escape') { capturing = null; setSettings(cfg); return; }
  const key = keyName(e);
  if (!key) return; // a modifier on its own: wait for the rest
  const combo = [e.ctrlKey && 'Ctrl', e.altKey && 'Alt', e.shiftKey && 'Shift', e.metaKey && 'Win', key].filter(Boolean).join('+');
  const { key: setting } = capturing;
  capturing = null;
  await saveSettings({ hotkeys: { [setting]: combo } });
}, true);

onSettings((c) => {
  if (document.activeElement !== $('language')) $('language').value = c.settings.language || 'en';
  const s = c.settings;
  const heroes = Object.entries(s.hero_roles || {}).sort((a, b) => (c.hero_names[a[0]] || '').localeCompare(c.hero_names[b[0]] || ''));
  $('hero-roles').innerHTML = heroes.length
    ? `<table class="hero-roles"><tbody>${heroes.map(([id, role]) => `<tr><td>${esc(c.hero_names[id] || id)}</td><td><select data-hero="${esc(id)}">${c.roles.map((r) => `<option value="${r}" ${r === role ? 'selected' : ''}>${ROLE_NAMES[r]}</option>`).join('')}</select></td><td><button class="btn small" data-forget="${esc(id)}">Forget</button></td></tr>`).join('')}</tbody></table>`
    : '<div class="empty">None yet. The position you play on a hero is remembered after each match.</div>';

  $('voice').value = s.voice;
  $('voice').querySelector('option[value=system]').disabled = !c.system_voice;
  $('voice-level').value = s.voice_level;
  $('rate').value = s.voice_rate;
  $('rate-v').textContent = s.voice_rate > 0 ? `+${s.voice_rate}` : s.voice_rate;
  $('voice-missing').hidden = !(!c.natural_voice && s.language !== 'en' && s.voice === 'system' && c.voice_langs && !c.voice_langs.includes(s.language));

  $('screen-draft').checked = !!(s.screen && s.screen.draft);
  for (const [id, key] of PICK_FIELDS) {
    if (document.activeElement !== $(id)) $(id).value = s.picks[key];
  }

  $('rec-auto').checked = s.recording.auto;
  if (document.activeElement !== $('rec-keep')) $('rec-keep').value = s.recording.keep;
  $('rec-state').textContent = c.recording ? tp('Recording to {file}', { file: c.recording.split(/[\\/]/).pop() }) : t('Not recording right now.');
  $('rec-now').textContent = c.recording ? 'Stop recording' : 'Record now';

  if (document.activeElement !== $('account-id')) $('account-id').value = s.account_id || '';

  $('dashboard-window').checked = s.dashboard_window;
  $('quiet-fights').checked = s.quiet_in_fights !== false;
  if (navigator.platform.toLowerCase().includes('linux') && !$('autostart-label').dataset.linux) {
    $('autostart-label').dataset.linux = '1';
    $('autostart-label').innerHTML = `${t('Start when you log in')}<small>${t('Runs in the background and wakes up when Dota starts.')}</small>`;
  }
  renderNaturalVoice(c);
  // Name the speech this machine actually has: speech-dispatcher or eSpeak on Linux.
  if (c.speech) $('voice-system').textContent = t(c.speech);
  $('voice-system').disabled = !c.speech;
  $('tilt').checked = s.tilt_check;
  if (!capturing) {
    $('hotkeys').innerHTML = HOTKEYS.map(([key, label]) => {
      const problem = (c.hotkey_problems || {})[key];
      return `<span>${label}</span><kbd>${esc(s.hotkeys[key])}</kbd><button class="btn small" data-hotkey="${key}">Change</button>${problem ? `<span class="problem">${esc(problem)}</span>` : ''}`;
    }).join('');
  }
  $('autostart-row').hidden = !c.can_autostart;
  $('autostart').checked = c.autostart;
  $('version').textContent = `Gourdian ${c.version}. Rune, Roshan and neutral item timings are built into this version; updates bring new patch timings.`;
});

$('hero-roles').addEventListener('change', (e) => {
  const id = e.target.dataset.hero;
  if (id) saveSettings({ hero_roles: { ...cfg.settings.hero_roles, [id]: e.target.value } });
});
$('hero-roles').addEventListener('click', (e) => {
  const id = e.target.dataset.forget;
  if (!id) return;
  const roles = { ...cfg.settings.hero_roles };
  delete roles[id];
  // hero_roles is replaced as a whole, so the forgotten hero disappears.
  saveSettings({ hero_roles: roles });
});

$('voice').addEventListener('change', (e) => saveSettings({ voice: e.target.value }));
$('voice-level').addEventListener('change', (e) => saveSettings({ voice_level: e.target.value }));
$('rate').addEventListener('input', (e) => { $('rate-v').textContent = e.target.value > 0 ? `+${e.target.value}` : e.target.value; });
$('rate').addEventListener('change', (e) => saveSettings({ voice_rate: Number(e.target.value) }));
$('test-voice').addEventListener('click', async () => {
  try { await api('/api/voice/test', { method: 'POST' }); } catch (e) { toast(e.message, true); }
  if (cfg && cfg.settings.voice === 'browser') {
    const ru = cfg.settings.language === 'ru';
    sayInBrowser(ru ? 'Проверка голоса. Руна силы через 15 секунд.' : 'Gourdian voice check. Power rune in 15 seconds.', cfg.settings.language, cfg.settings.voice_rate, false);
  }
});
// renderNaturalVoice shows Piper's voices on a Linux desktop: which is used, and whether it's here.
function renderNaturalVoice(c) {
  const nv = c.natural_voice;
  $('natural-voice').hidden = !nv;
  if (!nv) return;
  const lang = c.settings.language;
  if (document.activeElement !== $('piper-voice')) {
    $('piper-voice').innerHTML = nv.voices.filter((v) => v.lang === lang).map((v) =>
      `<option value="${esc(v.id)}" ${v.id === nv.chosen[lang] ? 'selected' : ''}>${esc(v.name)}${v.installed ? '' : ` · ${esc(tp('{n} MB to download', { n: Math.round(v.size / 1048576) }))}`}</option>`).join('');
  }
  renderVoiceState(nv);
  $('piper-install').hidden = !['missing', 'failed'].includes(nv.state);
  $('piper-source').textContent = tp('Piper runs on this computer, offline. It and its voices come from github.com/rhasspy/piper and huggingface.co/rhasspy/piper-voices, are checked against pinned checksums, and are kept in {folder}.', { folder: nv.folder });
}

function renderVoiceState(nv) {
  const states = {
    ready: t('Downloaded and in use.'),
    missing: t('Not downloaded yet: Piper and this voice, about 90 MB.'),
    installing: tp('Downloading: {done} of {size} MB', { done: nv.done_mb || 0, size: nv.size_mb || '…' }),
    failed: tp("Didn't download: {err}", { err: nv.text || '' }),
    no_player: t('Nothing can play it: install PipeWire (pw-play) or alsa-utils (aplay).'),
  };
  $('piper-state').textContent = states[nv.state] || '';
}

$('piper-voice').addEventListener('change', (e) => saveSettings({ piper_voices: { [cfg.settings.language]: e.target.value } }));
$('piper-install').addEventListener('click', async (e) => {
  e.target.disabled = true;
  try { await api('/api/voice/install', { method: 'POST' }); } catch (err) { toast(err.message, true); }
  e.target.disabled = false;
});
events.on('voice_progress', (p) => {
  if (cfg && cfg.natural_voice) renderVoiceState({ ...cfg.natural_voice, state: 'installing', done_mb: p.done_mb, size_mb: p.size_mb });
});
$('voice-install').addEventListener('click', async (e) => {
  e.target.disabled = true;
  try { toast(t((await api('/api/voice/install', { method: 'POST' })).text)); } catch (err) { toast(err.message, true); }
  e.target.disabled = false;
});
$('voice-recheck').addEventListener('click', async () => {
  try { await api('/api/voice/recheck', { method: 'POST' }); } catch (err) { toast(err.message, true); }
});
events.on('voice_install', (st) => toast(t(st.text), st.state === 'failed'));

// Pick help: each box saves the one number it holds, so a rejected value can't take the rest
// of the card with it.
const PICK_FIELDS = [['pick-days', 'days'], ['pick-half', 'half_life_days'], ['pick-trust', 'trust_after'],
  ['pick-min', 'min_games'], ['pick-avoid', 'avoid_pct'], ['pick-show', 'show'], ['pick-fresh', 'fresh'], ['pick-avoid-n', 'avoid']];
for (const [id, key] of PICK_FIELDS) {
  $(id).addEventListener('change', async (e) => {
    try { await saveSettings({ picks: { [key]: Number(e.target.value) } }); } catch (err) { toast(err.message, true); }
  });
}
$('picks-reset').addEventListener('click', () => saveSettings({ picks: null }));
$('screen-draft').addEventListener('change', (e) => saveSettings({ screen: { draft: e.target.checked } }));

$('rec-auto').addEventListener('change', (e) => saveSettings({ recording: { auto: e.target.checked } }));
$('rec-keep').addEventListener('change', (e) => saveSettings({ recording: { keep: Number(e.target.value) } }));
$('rec-now').addEventListener('click', async () => {
  try { setSettings(await api('/api/recording', { method: 'POST', body: { on: !cfg.recording } })); } catch (e) { toast(e.message, true); }
});

$('account-id').addEventListener('change', (e) => {
  let id = e.target.value.replace(/\D/g, '');
  if (id.length > 10) id = String(BigInt(id) - 76561197960265728n);
  saveSettings({ account_id: id });
});
$('import-matches').addEventListener('click', async () => {
  try { await api('/api/import', { method: 'POST', body: { count: 50 } }); } catch (e) { $('import-note').textContent = e.message; }
});
events.on('import_status', (st) => {
  $('import-matches').disabled = st.running;
  if (st.running) $('import-note').textContent = st.total ? `Importing: ${st.done} of ${st.total} checked, ${st.added} added…` : 'Asking OpenDota for your matches…';
  else if (st.error) $('import-note').textContent = `Import stopped: ${st.error}`;
  else $('import-note').textContent = `Import finished: ${st.added} new ${st.added === 1 ? 'match' : 'matches'} added.`;
});

$('dashboard-window').addEventListener('change', (e) => saveSettings({ dashboard_window: e.target.checked }));
$('export-csv').addEventListener('click', async (e) => {
  e.target.disabled = true;
  try { toast((await api('/api/export/csv', { method: 'POST' })).status); } catch (err) { toast(err.message, true); }
  e.target.disabled = false;
});
$('language').addEventListener('change', (e) => saveSettings({ language: e.target.value }));
$('quiet-fights').addEventListener('change', (e) => saveSettings({ quiet_in_fights: e.target.checked }));
$('tilt').addEventListener('change', (e) => saveSettings({ tilt_check: e.target.checked }));
$('autostart').addEventListener('change', async (e) => {
  try { setSettings(await api('/api/autostart', { method: 'PUT', body: { on: e.target.checked } })); } catch (err) { toast(err.message, true); e.target.checked = !e.target.checked; }
});
document.querySelector('.folders').addEventListener('click', async (e) => {
  const name = e.target.dataset.folder;
  if (!name) return;
  try { await api(`/api/folders/${name}`, { method: 'POST' }); } catch (err) { toast(err.message, true); }
});

events.start();
