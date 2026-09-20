import { $, esc, api, toast, t, events, onSettings, saveSettings, cfg } from './app.js';

const WIDGETS = {
  alerts: ['Alerts', 'Tips from the rules and the AI coach, in large type for a few seconds.'],
  position: ['Position', 'Before 2:30: the position you are coached as, and how to change it.'],
  drill: ['Drill', 'The habit you are drilling, and how often it happened this game.'],
  picks: ['Your best heroes', 'While you pick: your best and worst heroes in this position.'],
  briefing: ['Briefing', 'Before the horn: your record on the hero, the last-hit target and item goals.'],
  focus: ['Focus', 'Your focus from the last match review, before the horn and while dead.'],
  death: ['While dead', 'Respawn time and gold to spend.'],
  timers: ['Timers', 'Runes, neutral items, Roshan, Tormentor and stack pulls.'],
  pace: ['Last-hit pace', 'Last hits against the target pace for your position.'],
  next_item: ['Next item', 'The next item in the build, and when you can buy it.'],
  skill: ['Skill', 'With a skill point to spend: the ability pros level next, and how many pro games that order comes from.'],
  item_goal: ['Item goal', 'Your next core item and its timing goal, from five minutes before it.'],
  stats: ['Stats line', 'Kills, deaths, assists, GPM and last hits.'],
};
const KIND_NAMES = { rune: 'Runes', neutral: 'Neutral items', objective: 'Roshan and Tormentor', stack: 'Stacks' };
const SLIDERS = [['width', 'hud_width', 'px'], ['scale', 'hud_scale', '%'], ['background', 'hud_background', '%'], ['opacity', 'hud_opacity', '%']];
let hudPayload = null, widgets = [], dragIndex = -1;

function renderPreview() {
  if (!cfg) return;
  if (!hudPayload || !hudPayload.sample) {
    $('preview').innerHTML = '<div class="muted">Loading the preview…</div>';
    return;
  }
  const o = cfg.settings.overlay;
  const live = hudPayload.live && (hudPayload.live.alert || (hudPayload.live.rows || []).length);
  const view = ($('sample').checked || !live ? hudPayload.sample : hudPayload.live) || {};
  $('preview-note').textContent = view === hudPayload.live ? 'Live from your match' : 'Sample data';
  const bg = `rgba(14,17,22,${o.hud_background / 100})`;
  const panel = (lines, bar, big) => `<div class="hp ${big ? 'big' : ''} ${o.hud_shadow ? 'shadow' : ''}" style="background:${bg};--bar:${bar}">${lines.map((l) => `<div class="k-${esc(l.kind)}">${esc(l.text)}</div>`).join('')}</div>`;
  const colors = { info: '#5b9cf0', warn: '#e5a93b', urgent: '#ef4a4a', coach: '#9085e9', good: '#3fb97a', text: '#e6e9ef', muted: '#8b95a7' };
  let html = '';
  if (view.alert) html += panel([{ ...view.alert, text: view.alert.text + (view.more ? `  (+${view.more})` : '') }], colors[view.alert.kind] || '#262d3a', true);
  if ((view.rows || []).length) html += panel(view.rows, '#262d3a', false);
  const el = $('preview');
  el.innerHTML = html || '<div class="muted">Nothing enabled.</div>';
  el.style.width = `${o.hud_width}px`;
  el.style.opacity = o.hud_opacity / 100;
  el.style.transform = `scale(${o.hud_scale / 100})`;
  $('stage').className = `stage ${o.hud_placed ? 'right' : o.hud_corner === 'top-left' ? '' : o.hud_corner === 'top-center' ? 'center' : 'right'}`;
}

function widgetOptions(w, i) {
  if (w.id === 'alerts') {
    return `<div class="opts">
      <label>Show <select data-i="${i}" data-opt="min_severity">${[['info', 'every tip'], ['warn', 'warnings and up'], ['urgent', 'urgent only']].map(([v, n]) => `<option value="${v}" ${w.min_severity === v ? 'selected' : ''}>${n}</option>`).join('')}</select></label>
      <label><input type="checkbox" data-i="${i}" data-opt="coach" ${w.coach ? 'checked' : ''}> AI coach tips</label></div>`;
  }
  if (w.id === 'timers') {
    return `<div class="opts">
      <label>Show <select data-i="${i}" data-opt="count">${[1, 2, 3, 4, 5, 6, 7].map((n) => `<option ${w.count === n ? 'selected' : ''}>${n}</option>`).join('')}</select></label>
      <label>due within <select data-i="${i}" data-opt="within">${[[0, 'any time'], [30, '30 s'], [60, '1 min'], [120, '2 min'], [300, '5 min']].map(([v, n]) => `<option value="${v}" ${(w.within || 0) === v ? 'selected' : ''}>${n}</option>`).join('')}</select></label>
      ${Object.entries(KIND_NAMES).map(([k, n]) => `<label><input type="checkbox" data-i="${i}" data-kind="${k}" ${(w.kinds || []).includes(k) ? 'checked' : ''}> ${n}</label>`).join('')}</div>`;
  }
  return '';
}

function renderWidgets() {
  $('widgets').innerHTML = widgets.map((w, i) => {
    const [name, desc] = WIDGETS[w.id] || [w.id, ''];
    return `<div class="widget ${w.on ? '' : 'off'}" draggable="true" data-index="${i}">
      <span class="grip" title="Drag to reorder">⋮⋮</span>
      <input type="checkbox" data-i="${i}" data-opt="on" ${w.on ? 'checked' : ''} aria-label="Show ${esc(name)}">
      <div><b>${esc(name)}</b><small>${esc(desc)}</small>${widgetOptions(w, i)}</div>
      <span class="moves"><button class="btn small" data-move="-1" data-i="${i}" ${i === 0 ? 'disabled' : ''} aria-label="Move up">↑</button><button class="btn small" data-move="1" data-i="${i}" ${i === widgets.length - 1 ? 'disabled' : ''} aria-label="Move down">↓</button></span>
    </div>`;
  }).join('');
}

const saveWidgets = () => saveSettings({ hud_widgets: widgets });

$('widgets').addEventListener('change', (e) => {
  const i = Number(e.target.dataset.i);
  if (Number.isNaN(i)) return;
  const w = widgets[i];
  if (e.target.dataset.kind) {
    const kinds = new Set(w.kinds || []);
    e.target.checked ? kinds.add(e.target.dataset.kind) : kinds.delete(e.target.dataset.kind);
    w.kinds = Object.keys(KIND_NAMES).filter((k) => kinds.has(k));
  } else {
    const opt = e.target.dataset.opt;
    w[opt] = e.target.type === 'checkbox' ? e.target.checked : ['count', 'within'].includes(opt) ? Number(e.target.value) : e.target.value;
  }
  saveWidgets();
});
$('widgets').addEventListener('click', (e) => {
  const move = Number(e.target.dataset.move);
  if (!move) return;
  const i = Number(e.target.dataset.i), j = i + move;
  [widgets[i], widgets[j]] = [widgets[j], widgets[i]];
  saveWidgets();
});
$('widgets').addEventListener('dragstart', (e) => {
  const row = e.target.closest('.widget');
  dragIndex = Number(row.dataset.index);
  row.classList.add('dragging');
});
$('widgets').addEventListener('dragover', (e) => {
  const row = e.target.closest('.widget');
  if (!row) return;
  e.preventDefault();
  document.querySelectorAll('.widget.over').forEach((el) => el.classList.remove('over'));
  row.classList.add('over');
});
$('widgets').addEventListener('drop', (e) => {
  const row = e.target.closest('.widget');
  if (!row || dragIndex < 0) return;
  e.preventDefault();
  const [moved] = widgets.splice(dragIndex, 1);
  widgets.splice(Number(row.dataset.index), 0, moved);
  dragIndex = -1;
  saveWidgets();
});
$('widgets').addEventListener('dragend', () => { dragIndex = -1; renderWidgets(); });

onSettings((c) => {
  const o = c.settings.overlay;
  widgets = structuredClone(c.settings.hud_widgets || []);
  renderWidgets();
  $('corner').value = o.hud_placed ? 'placed' : o.hud_corner;
  for (const [id, key, unit] of SLIDERS) {
    if (document.activeElement !== $(id)) $(id).value = o[key];
    $(`${id}-v`).textContent = `${o[key]}${unit}`;
  }
  $('shadow').checked = o.hud_shadow;
  renderPreview();
});

$('corner').addEventListener('change', (e) => saveSettings({ overlay: { hud_corner: e.target.value, hud_placed: false } }));
for (const [id, key, unit] of SLIDERS) {
  $(id).addEventListener('input', (e) => {
    $(`${id}-v`).textContent = `${e.target.value}${unit}`;
    if (cfg) { cfg.settings.overlay[key] = Number(e.target.value); renderPreview(); }
  });
  $(id).addEventListener('change', (e) => saveSettings({ overlay: { [key]: Number(e.target.value) } }));
}
$('shadow').addEventListener('change', (e) => saveSettings({ overlay: { hud_shadow: e.target.checked } }));
$('sample').addEventListener('change', renderPreview);
$('edit').addEventListener('click', async () => {
  try { toast((await api('/api/hud/edit', { method: 'POST' })).status); } catch (e) { toast(e.message, true); }
});

events.on('hud', (p) => { hudPayload = p; renderPreview(); });
// The preview is fetched on load and again whenever the event stream (re)connects, so a
// restart of the trainer doesn't leave it empty.
async function loadHUD() {
  try {
    hudPayload = await api('/api/hud');
    renderPreview();
  } catch (e) {
    setTimeout(loadHUD, 2000);
  }
}
events.onOpen(loadHUD);
loadHUD();
events.start();
