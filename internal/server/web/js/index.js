import { $, esc, clockStr, imgURL, ROLE_NAMES, CATEGORY_NAMES, api, toast, t, tp, sayInBrowser, events, onSettings, saveSettings, positionButtons, cfg } from './app.js';

let snap = null, tips = [], voiceUnlocked = false, calloutTimer = null;

// With the dashboard open in more than one tab or window, only one of them may speak, or every
// tip is heard twice. Tabs agree through a heartbeat in localStorage; the one you last
// looked at wins, and another takes over within seconds if it closes.
const voiceTab = (() => {
  const id = Math.random().toString(36).slice(2);
  const read = () => { try { return JSON.parse(localStorage.getItem('voice-owner') || 'null'); } catch (e) { return null; } };
  const claim = () => { try { localStorage.setItem('voice-owner', JSON.stringify({ id, at: Date.now() })); } catch (e) { /* private window */ } };
  const beat = () => { const o = read(); if (!o || o.id === id || Date.now() - o.at > 4000) claim(); };
  setInterval(beat, 1000);
  beat();
  document.addEventListener('visibilitychange', () => { if (!document.hidden) claim(); });
  window.addEventListener('focus', claim);
  window.addEventListener('pagehide', () => { const o = read(); if (o && o.id === id) { try { localStorage.removeItem('voice-owner'); } catch (e) { /* ignore */ } } });
  return { mine: () => { const o = read(); return !o || o.id === id; } };
})();

let lastSpoken = { text: '', at: 0 };

function speakBrowser(text, urgent) {
  if (!('speechSynthesis' in window) || !cfg || cfg.settings.voice !== 'browser' || !voiceTab.mine()) return;
  if (text === lastSpoken.text && Date.now() - lastSpoken.at < 6000) return;
  lastSpoken = { text, at: Date.now() };
  sayInBrowser(text, cfg.settings.language, cfg.settings.voice_rate, urgent);
}

document.addEventListener('click', () => {
  if (voiceUnlocked) return;
  voiceUnlocked = true;
  if ('speechSynthesis' in window) speechSynthesis.speak(new SpeechSynthesisUtterance(''));
  renderBanner();
});

function renderBanner() {
  $('voice-banner').classList.toggle('show', !!cfg && cfg.settings.voice === 'browser' && !voiceUnlocked);
}

function showCallout(tip) {
  const el = $('callout');
  el.className = `callout ${tip.category === 'ai' ? 'ai' : tip.severity}`;
  $('callout-text').textContent = tip.text;
  $('callout-meta').innerHTML = `${clockStr(tip.clock)} · <span>${esc(CATEGORY_NAMES[tip.category] || tip.category)}</span>`;
  clearTimeout(calloutTimer);
  calloutTimer = setTimeout(() => { el.className = 'callout'; }, tip.severity === 'info' ? 8000 : 15000);
}

function renderFeed() {
  const items = tips.slice().reverse();
  $('feed-count').textContent = items.length ? items.length : '';
  $('feed').innerHTML = items.length
    ? items.map((tip) => {
      const cat = esc(t(CATEGORY_NAMES[tip.category] || tip.category));
      const chip = tip.rule && tip.category !== 'ai' && tip.category !== 'system'
        ? `<a class="chip" href="/rules.html#rule=${encodeURIComponent(tip.rule)}" title="${esc(t('Open the rule that gave this tip'))}">${cat}</a>`
        : `<span class="chip">${cat}</span>`;
      return `<li class="${tip.severity} ${tip.category === 'ai' || tip.category === 'system' ? tip.category : ''}"><span class="t num">${clockStr(tip.clock)}</span><span>${esc(tip.text)}${chip}</span></li>`;
    }).join('')
    : '<li class="empty" style="display:block;background:none;border:0">No tips yet this session.</li>';
}

function systemNote(text) {
  tips.push({ severity: 'info', category: 'system', text: text.trim(), clock: snap && snap.game_state ? snap.clock : 0 });
  if (tips.length > 50) tips.shift();
  renderFeed();
}

let setupLoaded = false;
function renderSetup() {
  const card = $('setup');
  if (snap && snap.connected) { card.hidden = true; setupLoaded = false; return; }
  card.hidden = false;
  if (!setupLoaded) { setupLoaded = true; loadSetup(); }
}

async function loadSetup(checks) {
  try { checks = checks || await api('/api/setup'); } catch (e) { return; }
  const ready = checks.every((c) => c.ok);
  $('setup-body').innerHTML = `
    <p style="margin:0 0 8px">${ready
      ? 'Waiting for Dota 2. Start Dota (restart it if it was open when you installed Gourdian), then play a match or try Demo Hero.'
      : 'Waiting for Dota 2. Fix the items below, then start Dota.'}</p>
    <ul class="checks">${checks.map((c) => `<li class="${c.ok ? 'ok' : 'bad'}"><b>${c.ok ? '✓' : '!'}</b><span>${esc(c.label)}${c.detail ? `<small>${esc(c.detail)}</small>` : ''}</span>${c.fix === 'install_gsi' ? '<button class="btn small" data-fix="install_gsi">Fix it</button>' : ''}${c.fix === 'install_voice' ? `<button class="btn small" data-fix="install_voice">${t('Install voice')}</button>` : ''}</li>`).join('')}</ul>`;
}

$('setup-body').addEventListener('click', async (e) => {
  if (e.target.dataset.fix === 'install_voice') {
    e.target.disabled = true;
    try { toast(t((await api('/api/voice/install', { method: 'POST' })).text)); } catch (err) { e.target.disabled = false; toast(err.message, true); }
    return;
  }
  if (e.target.dataset.fix !== 'install_gsi') return;
  e.target.disabled = true;
  try { loadSetup(await api('/api/setup/install', { method: 'POST' })); } catch (err) { e.target.disabled = false; toast(err.message, true); }
});

// Snapshots arrive several times a second; rebuilding unchanged markup would restart image loads.
const rendered = {};
function renderOnce(key, value, fn) {
  const sig = JSON.stringify(value);
  if (rendered[key] === sig) return;
  rendered[key] = sig;
  fn();
}

function renderHeader() {
  $('clock').textContent = snap && snap.game_state ? clockStr(snap.clock) : '--:--';
  if (snap && snap.game_state) $('clock').textContent += snap.daytime ? ' ☀' : ' ☾';
  const h = snap && snap.hero;
  renderOnce('heroline', h && [h.img, h.name, h.level], () => {
    $('heroline').innerHTML = h ? `${h.img ? `<img src="${esc(imgURL(h.img))}" alt="">` : ''}<span><b>${esc(h.name)}</b> <span class="muted">Lv ${h.level}</span></span>` : '';
  });
}

function renderHero() {
  const h = snap && snap.hero, p = snap && snap.player;
  if (!h || !p) { $('hero').innerHTML = '<div class="empty">Pick a hero to see live stats.</div>'; $('hero-sub').textContent = ''; return; }
  $('hero-sub').textContent = snap.team ? t(snap.team === 'dire' ? 'Dire' : 'Radiant') : '';
  const bb = h.buyback_cooldown > 0 ? tp('cd {n}s', { n: h.buyback_cooldown }) : tp('{n}g', { n: h.buyback_cost });
  $('hero').innerHTML = `
    ${h.alive ? '' : `<div class="dead">${esc(tp('Dead · respawn in {n}s · shop now', { n: h.respawn_seconds }))}</div>`}
    <div class="bars">
      <div class="bar"><i style="width:${h.health_percent}%"></i><span class="num">${h.health} / ${h.max_health}</span></div>
      ${h.max_mana ? `<div class="bar mana"><i style="width:${h.mana_percent}%"></i><span class="num">${h.mana} / ${h.max_mana}</span></div>` : ''}
    </div>
    <div class="stats num">
      <div class="stat"><b>${p.kills}/${p.deaths}/${p.assists}</b><small>K / D / A</small></div>
      <div class="stat"><b>${p.last_hits}/${p.denies}</b><small>LH / DN</small></div>
      <div class="stat"><b>${p.gold}</b><small>Gold</small></div>
      <div class="stat"><b>${p.gpm}</b><small>GPM</small></div>
      <div class="stat"><b>${p.xpm}</b><small>XPM</small></div>
      <div class="stat"><b>${bb}</b><small>Buyback</small></div>
    </div>`;
}

function renderPace() {
  const pace = snap && snap.in_match && snap.pace;
  $('pace-card').hidden = !pace;
  if (!pace) return;
  const scale = Math.max(pace.target || 0, pace.expected, pace.last_hits, 1);
  const behind = pace.last_hits < pace.expected;
  const diff = pace.last_hits - pace.expected;
  const goal = (g) => {
    if (g.owned) return g.at ? tp('{item} by {by} · done at {at}', { item: g.name, by: clockStr(g.by), at: clockStr(g.at) }) : tp('{item} by {by} · done', { item: g.name, by: clockStr(g.by) });
    return tp('{item} by {by} · {n}g to go', { item: g.name, by: clockStr(g.by), n: g.remaining });
  };
  $('pace').innerHTML = `
    <div class="num"><b style="font-size:18px">${pace.last_hits}</b> ${esc(tp('last hits · expected {n} now', { n: pace.expected }))}
      <span class="${behind ? 'loss' : 'win'}">(${diff >= 0 ? '+' : ''}${diff})</span></div>
    <div class="pace-track"><i class="${behind ? 'behind' : ''}" style="width:${Math.min(100, pace.last_hits / scale * 100)}%"></i><em style="left:${Math.min(100, pace.expected / scale * 100)}%"></em></div>
    ${pace.checkpoint ? `<div class="muted">${esc(tp(pace.usual ? 'Target {n} by {at} · your usual is {usual}' : 'Target {n} by {at}', { n: pace.target, at: pace.checkpoint, usual: pace.usual }))}</div>` : ''}
    ${(snap.item_goals || []).map((g) => `<div class="muted">${esc(goal(g))}</div>`).join('')}`;
}

function timerLabel(label) {
  const tier = /^Neutral tier (\d+)$/.exec(label);
  return tier ? tp('Neutral tier {n}', { n: tier[1] }) : t(label);
}

function renderTimers() {
  const list = (snap && snap.in_match && snap.timers) || [];
  $('timers').innerHTML = list.length
    ? list.map((x) => { const inSec = x.at - snap.clock; return `<li class="${inSec <= 20 ? 'soon' : ''}"><span>${esc(timerLabel(x.label))}<span class="when num">${clockStr(x.at)}</span></span><b class="num">${clockStr(inSec)}</b></li>`; }).join('')
    : '<li class="empty" style="background:none">Timers appear once a match starts.</li>';
}

function renderItems() {
  const list = (snap && snap.items) || [];
  renderOnce('items', list, () => {
    $('items').innerHTML = list.length
      ? list.map((it) => `<div class="item ${it.slot.startsWith('stash') ? 'stash' : ''}" title="${esc(it.dname)} (${esc(it.slot)})" style="${it.img ? `background-image:url('${esc(imgURL(it.img))}')` : ''}">
          ${it.cooldown ? `<div class="cd num">${it.cooldown}</div>` : ''}<span>${esc(it.img ? (it.charges ? it.charges : '') : it.dname)}</span></div>`).join('')
      : '<div class="empty">Empty.</div>';
  });
}

function source(id) {
  return ((snap && snap.sources) || []).find((x) => x.id === id);
}

function renderSources() {
  const text = (id) => (source(id) || {}).from || '';
  renderOnce('sources', snap && snap.sources, () => {
    const build = source('build');
    $('build-short').textContent = build ? build.short : 'OpenDota';
    $('build-src').textContent = text('build');
    $('timers-src').textContent = text('timers');
    $('pace-src').textContent = [text('last_hits'), text('item_goals')].filter(Boolean).join(' ');
  });
}

function renderSkills() {
  const sk = snap && snap.in_match && snap.skill;
  $('skills-card').hidden = !sk;
  if (!sk) return;
  renderOnce('skills', sk, () => {
    const src = source('skills');
    $('skills-short').textContent = src ? src.short : '';
    $('skills-src').textContent = src ? src.from : '';
    $('skills').innerHTML = sk.order.map((name, i) =>
      `<div class="${i === sk.next_at ? 'next' : sk.done[i] ? 'done' : ''}"><span class="n num">${i + 1}</span><span>${esc(name)}</span></div>`).join('');
  });
}

function renderBuild() {
  const build = snap && snap.build;
  const gold = (snap && snap.player && snap.player.gold) || 0;
  const affordable = (build || []).map((it) => it.remaining <= gold);
  renderOnce('build', [build, affordable, !!(snap && snap.hero)], () => {
    if (!build || !build.length) {
      $('build').innerHTML = `<div class="empty">${snap && snap.hero ? 'Loading popular build…' : 'Shows the popular item build for your hero.'}</div>`;
      return;
    }
    const phases = { start: t('Starting'), early: t('Early game'), mid: t('Mid game'), late: t('Late game') };
    let html = '', cur = '';
    for (const it of build) {
      if (it.phase !== cur) { cur = it.phase; html += `<div class="phase">${phases[cur] || cur}</div>`; }
      let cost;
      if (it.owned) cost = `<span class="cost">${esc(t('✓ owned'))}</span>`;
      else if (it.skipped) cost = `<span class="cost">${esc(t('skipped'))}</span>`;
      else if (it.remaining <= gold) cost = `<span class="cost ok num">${esc(tp('{n}g · buy now', { n: it.remaining }))}</span>`;
      else cost = `<span class="cost num">${esc(tp(it.remaining < it.cost ? '{n}g left' : '{n}g', { n: it.remaining }))}</span>`;
      html += `<div class="bi ${it.owned ? 'owned' : ''} ${it.skipped ? 'skipped' : ''} ${it.next ? 'next' : ''}">${it.img ? `<img src="${esc(imgURL(it.img))}" alt="">` : '<span></span>'}<span>${esc(it.dname)}</span>${cost}</div>`;
    }
    $('build').innerHTML = html;
  });
}

let history = null;

async function loadHistory() {
  let h;
  try { h = await api('/api/history'); } catch (e) { return; }
  history = h;
  $('habit-sample').textContent = h.sample ? tp(h.from_simulated ? 'last {n} (practice or simulated)' : 'last {n}', { n: h.sample }) : '';
  $('habits').innerHTML = h.habits && h.habits.length
    ? `<div class="stats num" style="margin-bottom:10px">
         <div class="stat"><b>${h.avg_deaths.toFixed(1)}</b><small>Avg deaths</small></div>
         <div class="stat"><b>${Math.round(h.avg_gpm)}</b><small>Avg GPM</small></div>
         <div class="stat"><b>${h.avg_lh10 ? Math.round(h.avg_lh10) : '–'}</b><small>LH @ 10</small></div>
       </div>` + h.habits.map((x) => `<div class="habit"><b><span>${esc(x.label)}</span><span class="num muted">${tp('{n}/match', { n: x.per_match.toFixed(1) })}</span></b><p>${esc(x.advice)}</p></div>`).join('')
    : '<div class="empty">Finish a few matches with the trainer running. Your most common mistakes will show up here.</div>';
  $('matches').innerHTML = h.matches && h.matches.length
    ? `<table class="num"><thead><tr><th>Hero</th><th>Result</th><th>KDA</th><th>LH@10</th><th>GPM</th><th>MMR</th><th></th></tr></thead><tbody>${h.matches.map((m) => {
        const lh10 = m.last_hits_at && m.last_hits_at['10:00'] !== undefined ? m.last_hits_at['10:00'] : '–';
        const tag = m.simulated ? ' <span class="muted">(sim)</span>' : m.source === 'practice' ? ' <span class="muted">(practice)</span>' : '';
        return `<tr><td>${esc(m.hero)}${tag}</td><td class="${esc(m.result)}">${esc(m.result)}</td><td>${m.kills}/${m.deaths}/${m.assists}</td><td>${lh10}</td><td>${m.gpm}</td>
          <td>${mmrCell(m)}</td>
          <td><button class="btn small" data-review="${esc(m.match_id)}" title="${t('Ask the AI coach to review this match')}">${t('Review')}</button></td></tr>`;
      }).join('')}</tbody></table>`
    : '<div class="empty">No matches recorded yet.</div>';
}

// mmrCell shows the MMR logged for a match, or the way to log it: ranked matches ask
// straight away, the rest offer to be marked ranked first.
function mmrCell(m) {
  if (mmrByMatch[m.match_id]) return `<span class="num">${mmrByMatch[m.match_id]}</span>`;
  if (m.source === 'practice' || m.simulated) return '<span class="muted">–</span>';
  if (!m.ranked) return `<button class="btn small" data-ranked="${esc(m.match_id)}" title="${t('Mark this match as ranked')}">${t('Ranked?')}</button>`;
  return `<span class="mmr-cell"><input type="number" inputmode="numeric" data-mmrfor="${esc(m.match_id)}" placeholder="MMR" style="width:76px">
    <button class="btn small" data-savemmr="${esc(m.match_id)}">${t('Save')}</button></span>`;
}

// Drill: one habit at a time, counted while you play and scored after each match.
let drill = null;

async function loadDrill() {
  try { drill = await api('/api/drill'); renderDrill(); } catch (e) { /* offline */ }
}

function renderDrill() {
  if (!drill) return;
  const live = snap && snap.drill;
  const options = (drill.choices || []).map((c) => `<option value="${esc(c.rule)}" ${c.rule === drill.rule ? 'selected' : ''}>${esc(c.label)}${c.average ? ` · ${c.average.toFixed(1)}/${t('match')}` : ''}</option>`).join('');
  const bars = (drill.recent || []).slice().reverse().map((m) => `<span class="drill-bar" title="${esc(m.hero)}: ${m.count}" style="height:${Math.min(100, 12 + m.count * 14)}%"></span>`).join('');
  $('drill-sub').textContent = drill.rule && drill.average ? tp('usually {n} a match', { n: drill.average.toFixed(1) }) : '';
  $('drill').innerHTML = `
    <div class="field"><select id="drill-rule" style="flex:1"><option value="">${t('Nothing right now')}</option>${options}</select></div>
    ${drill.rule ? `
      ${live ? `<div class="drill-live ${live.count ? '' : 'clean'}">${tp('{n} this game', { n: live.count })}</div>`
        : `<div class="muted">${t('Counted while you play, scored after the match.')}</div>`}
      ${bars ? `<div class="drill-bars">${bars}</div><div class="muted" style="font-size:12px">${t('Your last matches, oldest first. Lower is better.')}</div>` : ''}
      ${drill.advice ? `<p class="hint">${esc(drill.advice)}</p>` : ''}`
      : `<p class="hint">${drill.suggestion_label ? tp('Pick one habit to work on. The trainer suggests {label}.', { label: esc(drill.suggestion_label) }) : t('Pick one habit to work on.')}</p>
         ${drill.suggestion ? `<button class="btn primary" id="drill-take">${t('Work on that')}</button>` : ''}`}`;
  if ($('drill-take')) $('drill-take').addEventListener('click', () => setDrill(drill.suggestion));
  $('drill-rule').addEventListener('change', (e) => setDrill(e.target.value));
}

async function setDrill(rule) {
  try { drill = await api('/api/drill', { method: 'PUT', body: { rule } }); renderDrill(); } catch (e) { toast(e.message, true); }
}

// Pick help while you are choosing a hero: your own record and the meta at your rank, with
// the reason each hero is on the list, since a name and a number alone say nothing.
function renderPicks(p) {
  $('picks-card').hidden = !p;
  if (!p) return;
  $('picks-sub').textContent = ROLE_NAMES[p.role] || '';
  const row = (h, avoid) => {
    const detail = [(h.why || []).join(' · '),
      h.avg_lh10 ? tp('{lh} last hits at 10:00', { lh: h.avg_lh10 }) : '',
      h.avg_deaths ? tp('{d} deaths a game', { d: h.avg_deaths.toFixed(1) }) : ''].filter(Boolean).join(' · ');
    return `<div class="habit pick">${h.img ? `<img src="${esc(imgURL(h.img))}" alt="">` : ''}<div>
      <b><span>${esc(h.hero)}</span>${h.games ? `<span class="num ${avoid ? 'bad' : ''}">${h.win_pct}% of ${h.games}</span>` : ''}</b>
      <p class="muted">${esc(detail)}</p></div></div>`;
  };
  const list = (heroes, label, avoid) => (heroes && heroes.length
    ? `<div class="label">${t(label)}</div>` + heroes.map((h) => row(h, avoid)).join('') : '');
  // The heroes read off the screen, so you can see what it made of them rather than having
  // to take the advice on trust.
  const enemies = (p.enemies || []).length
    ? `<div class="label">${t('The other side has taken')}</div><div class="enemies">` +
      p.enemies.map((h) => `<span class="enemy">${h.img ? `<img src="${esc(imgURL(h.img))}" alt="">` : ''}${esc(h.hero)}</span>`).join('') + '</div>'
    : '';
  const notes = (p.notes || []).length
    ? `<div class="notes">${p.notes.map((n) => `<div>${esc(n)}</div>`).join('')}</div>` : '';
  $('picks').innerHTML = enemies + notes
    + list(p.best, 'Your best on this position', false)
    + list(p.fresh, 'Strong right now, new to you', false)
    + list(p.avoid, 'Losing on this position', true);
}

function renderReview(r, status) {
  const card = $('review-card');
  if (!r && !status) { card.hidden = true; return; }
  card.hidden = false;
  if (status) {
    $('review-sub').textContent = '';
    $('review').innerHTML = `<div class="empty"><span class="spinner"></span>${esc(status.text || status)}</div>
      ${status.waiting && status.match_id ? `<button class="btn small" data-review-now="${esc(status.match_id)}">Review now with live data</button>` : ''}`;
    return;
  }
  $('review-sub').textContent = `${r.hero} · ${t(r.result)}`;
  const list = (items) => (items && items.length ? `<ul>${items.map((x) => `<li>${esc(x)}</li>`).join('')}</ul>` : '');
  $('review').innerHTML = `
    <div class="label" style="margin-top:0">Next game focus</div>
    <div class="focus">${esc(r.next_game_focus)}</div>
    ${r.followed_focus ? `<div class="label">Last game's focus</div><p>${esc(r.followed_focus)}</p>` : ''}
    <p>${esc(r.summary)}</p>
    <div class="label">Work on</div>${list(r.improve)}
    ${r.strengths && r.strengths.length ? `<div class="label">Went well</div>${list(r.strengths)}` : ''}`;
}

async function loadReviews() {
  try { renderReview((await api('/api/reviews'))[0]); } catch (e) { /* offline */ }
}

function renderBriefing() {
  const b = snap && snap.briefing;
  $('briefing-card').hidden = !b;
  if (!b) return;
  renderOnce('briefing', b, () => {
    $('briefing-sub').textContent = `${b.hero} · ${ROLE_NAMES[b.role] || b.role}`;
    const record = b.games ? `${b.games} games on this hero and position, ${Math.round(100 * b.wins / b.games)}% won` : 'First game on this hero and position';
    $('briefing').innerHTML = `<div class="brief">
      <div>${esc(record)}</div>
      ${b.target_10 ? `<div>Aim for <b>${b.target_10}</b> last hits at 10:00${b.usual_10 ? ` <span class="muted">(your usual is ${b.usual_10})</span>` : ''}</div>` : ''}
      ${(b.items || []).map((it) => `<div>${esc(it.name)} by <b>${clockStr(it.by)}</b>${it.usual ? ` <span class="muted">(usually ${clockStr(it.usual)})</span>` : ''}</div>`).join('')}
      ${b.last_review ? `<div class="muted">Last review on this hero: ${esc(b.last_review)}</div>` : ''}
    </div>`;
  });
}

async function loadGoals(g) {
  try { g = g && g.progress ? g : await api('/api/goals'); } catch (e) { return; }
  const list = g.progress || [];
  $('goals-card').hidden = !list.length;
  $('goals-sub').textContent = g.week || '';
  $('goals').innerHTML = list.map((p) => `<div class="goal ${p.done ? 'done' : ''}">
      <b><span>${esc(p.label)}</span><span class="num muted">${Math.min(p.met, g.done_after)}/${g.done_after}${p.done ? ' ✓' : ''}</span></b>
      <div class="track"><i style="width:${Math.min(100, 100 * p.met / g.done_after)}%"></i></div>
    </div>`).join('');
}

function renderAll() {
  const focus = snap && snap.focus;
  $('focus-line').hidden = !focus;
  $('focus-text').textContent = focus || '';
  renderPicks(snap && snap.picks); renderDrill(); renderBriefing(); renderSetup(); renderHeader(); renderHero(); renderPace(); renderTimers(); renderItems(); renderBuild(); renderSkills(); renderSources();
}

positionButtons($('positions'));
let renderedLang = null;
onSettings((c) => {
  if (c.settings.language !== renderedLang) {
    renderedLang = c.settings.language;
    for (const key of Object.keys(rendered)) delete rendered[key];
    renderAll();
    renderFeed();
  }
  $('voice').value = c.settings.voice;
  $('voice').querySelector('option[value=system]').disabled = !c.system_voice;
  $('ask-coach').disabled = !c.ai_ready;
  renderBanner();
});
$('voice').addEventListener('change', (e) => saveSettings({ voice: e.target.value }));
$('ask-coach').addEventListener('click', async () => {
  try { await api('/api/ai/ask', { method: 'POST' }); } catch (e) { systemNote(e.message); }
});
$('picks-ask').addEventListener('click', async () => {
  try { await api('/api/picks/ask', { method: 'POST' }); } catch (e) { systemNote(e.message); }
});
$('review').addEventListener('click', async (e) => {
  const id = e.target.dataset.reviewNow;
  if (!id) return;
  e.target.disabled = true;
  try { await api(`/api/matches/${encodeURIComponent(id)}/review`, { method: 'POST' }); } catch (err) { systemNote(err.message); e.target.disabled = false; }
});
$('matches').addEventListener('click', async (e) => {
  const id = e.target.dataset && e.target.dataset.review;
  if (!id) return;
  e.target.disabled = true;
  try { await api(`/api/matches/${encodeURIComponent(id)}/review`, { method: 'POST' }); } catch (err) { systemNote(err.message); e.target.disabled = false; }
});

events.on('snapshot', (s) => { snap = s; renderAll(); });
events.on('offline', () => { snap = null; renderAll(); });
events.on('tips', (list) => { tips = list || []; renderFeed(); if (tips.length) showCallout(tips[tips.length - 1]); });
events.on('tip', (tip) => {
  tips.push(tip);
  if (tips.length > 50) tips.shift();
  renderFeed();
  showCallout(tip);
  if (!tip.quiet) speakBrowser(tip.speech, tip.severity === 'urgent');
});
events.on('match', () => { loadHistory(); loadGoals(); });
events.on('goals', () => loadGoals());
events.on('ai_status', (st) => $('coach-status').classList.toggle('show', st === 'thinking'));
events.on('ai_error', (msg) => systemNote(`AI coach: ${msg}`));
events.on('review_status', (msg) => renderReview(null, msg));
events.on('review', (r) => renderReview(r));
events.onOpen(() => { loadMMR(); loadDrill(); loadHistory(); loadReviews(); loadGoals(); });
events.on('match', () => loadDrill());

renderAll();
renderFeed();
// After a match the trainer offers to log the new MMR; the buttons step from the last entry.
let mmrAsk = null;

function renderMMRPrompt() {
  $('mmr-card').hidden = !mmrAsk;
  if (!mmrAsk) return;
  $('mmr-sub').textContent = mmrAsk.ranked ? t('ranked') : '';
  const last = mmrAsk.last;
  $('mmr-body').innerHTML = `
    <p class="hint" style="margin-top:0">${tp('{hero}, {result}. What is your MMR now?', { hero: esc(mmrAsk.hero || ''), result: t(mmrAsk.result === 'win' ? 'win' : 'loss') })}</p>
    <div class="field" style="gap:8px;flex-wrap:wrap">
      ${last ? `<button class="btn primary" data-mmr="25">+25 → ${last + 25}</button>
        <button class="btn" data-mmr="-25">−25 → ${last - 25}</button>` : ''}
      <input id="mmr-exact" type="number" inputmode="numeric" placeholder="${last ? last : t('Current MMR')}" style="width:120px">
      <button class="btn" data-mmr="exact">${t('Save')}</button>
      <button class="btn small" data-mmr="skip">${t('Skip')}</button>
    </div>`;
}

$('mmr-card').addEventListener('click', async (e) => {
  const act = e.target.dataset.mmr;
  if (!act) return;
  try {
    if (act === 'skip') await api('/api/mmr/prompt', { method: 'DELETE' });
    else if (act === 'exact') {
      const value = Number($('mmr-exact').value);
      if (!value) return;
      await api(mmrAsk ? `/api/matches/${mmrAsk.match_id}/mmr` : '/api/mmr', { method: 'POST', body: { mmr: value, note: mmrAsk ? mmrAsk.result : '' } });
      await api('/api/mmr/prompt', { method: 'DELETE' });
    } else {
      await api('/api/mmr/change', { method: 'POST', body: { change: Number(act), note: mmrAsk ? mmrAsk.result : '' } });
    }
    mmrAsk = null;
    renderMMRPrompt();
    loadMMR();
  } catch (err) { toast(err.message, true); }
});

events.on('mmr_prompt', (p) => { mmrAsk = p; renderMMRPrompt(); });

// MMR logged per match, so the list shows it instead of asking again.
let mmrByMatch = {};
async function loadMMR() {
  try {
    const entries = await api('/api/mmr');
    mmrByMatch = {};
    for (const e of entries) if (e.match_id) mmrByMatch[e.match_id] = e.mmr;
    if (history) loadHistory();
  } catch (e) { /* the list still works without it */ }
}

$('matches').addEventListener('click', async (e) => {
  const ranked = e.target.dataset.ranked, save = e.target.dataset.savemmr;
  if (!ranked && !save) return;
  e.target.disabled = true;
  try {
    if (ranked) await api(`/api/matches/${ranked}/ranked`, { method: 'PUT', body: { ranked: true } });
    else {
      const value = Number(document.querySelector(`[data-mmrfor="${save}"]`).value);
      if (!value) { e.target.disabled = false; return; }
      await api(`/api/matches/${save}/mmr`, { method: 'POST', body: { mmr: value } });
    }
    await Promise.all([loadMMR(), loadHistory()]);
  } catch (err) { toast(err.message, true); e.target.disabled = false; }
});
events.onOpen(() => api('/api/mmr/prompt').then((p) => { mmrAsk = p; renderMMRPrompt(); }).catch(() => {}));
events.start();
