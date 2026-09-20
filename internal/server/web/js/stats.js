import { $, esc, api, t, tp, events, onSettings } from './app.js';

const LH_TARGET_10 = { carry: 65, mid: 60, offlane: 40 };
const NS = 'http://www.w3.org/2000/svg';
let data = null;

function svgEl(tag, attrs, parent) {
  const el = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) el.setAttribute(k, v);
  if (parent) parent.appendChild(el);
  return el;
}

function niceTicks(min, max, count = 4) {
  if (max === min) max = min + 1;
  const raw = (max - min) / count;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw);
  const lo = Math.floor(min / step) * step, hi = Math.ceil(max / step) * step;
  const ticks = [];
  for (let v = lo; v <= hi + step / 2; v += step) ticks.push(Math.round(v * 100) / 100);
  return ticks;
}

const locale = () => document.documentElement.lang || undefined;
const fmt = (n) => Math.round(n).toLocaleString(locale());
const fmtClock = (sec) => `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, '0')}`;
const fmtDate = (d) => new Date(d).toLocaleDateString(locale(), { month: 'short', day: 'numeric' });

// ---- tooltip -----------------------------------------------------------------
const tip = $('tooltip');
function showTip(x, y, title, rows) {
  tip.replaceChildren();
  const t = document.createElement('div');
  t.className = 't-title';
  t.textContent = title;
  tip.appendChild(t);
  for (const r of rows) {
    const row = document.createElement('div');
    row.className = 't-row';
    if (r.color) { const k = document.createElement('i'); k.className = 't-key'; k.style.background = r.color; row.appendChild(k); }
    const b = document.createElement('b'); b.textContent = r.value; row.appendChild(b);
    if (r.label) { const s = document.createElement('span'); s.textContent = r.label; row.appendChild(s); }
    tip.appendChild(row);
  }
  tip.style.display = 'block';
  const w = tip.offsetWidth, h = tip.offsetHeight;
  tip.style.left = `${Math.min(window.innerWidth - w - 8, x + 14)}px`;
  tip.style.top = `${Math.max(8, y - h - 12)}px`;
}
const hideTip = () => { tip.style.display = 'none'; };

// ---- charts ------------------------------------------------------------------
// series: [{name, color, values: [number|null]}]; all series share the x positions.
function lineChart(el, { series, xLabels, title, yFormat = fmt, yMin, yMax, reference, height = 220, endLabels = false }) {
  el.replaceChildren();
  const n = xLabels.length;
  const all = series.flatMap((s) => s.values).filter((v) => v !== null && v !== undefined);
  if (n === 0 || all.length === 0) { el.innerHTML = '<div class="empty">No data in this range yet.</div>'; return; }
  const width = el.clientWidth || 600;
  const m = { l: 44, r: endLabels ? 96 : 14, t: 12, b: 26 };
  const lo = yMin ?? Math.min(...all, reference ? reference.value : Infinity);
  const hi = yMax ?? Math.max(...all, reference ? reference.value : -Infinity);
  const ticks = niceTicks(yMin ?? Math.min(lo, hi * 0.9), hi);
  const y0 = ticks[0], y1 = ticks[ticks.length - 1];
  const px = (i) => m.l + (n === 1 ? (width - m.l - m.r) / 2 : (i / (n - 1)) * (width - m.l - m.r));
  const py = (v) => m.t + (1 - (v - y0) / (y1 - y0 || 1)) * (height - m.t - m.b);

  const svg = svgEl('svg', { viewBox: `0 0 ${width} ${height}`, height, tabindex: 0, role: 'img', 'aria-label': title }, el);
  for (const t of ticks) {
    svgEl('line', { x1: m.l, x2: width - m.r, y1: py(t), y2: py(t), stroke: t === y0 ? 'var(--axis)' : 'var(--grid)', 'stroke-width': 1 }, svg);
    const label = svgEl('text', { x: m.l - 8, y: py(t) + 4, 'text-anchor': 'end', fill: 'var(--muted)', 'font-size': 11 }, svg);
    label.textContent = yFormat(t);
  }
  const labelEvery = Math.max(1, Math.ceil(n / Math.max(2, Math.floor((width - m.l - m.r) / 60))));
  let lastShown = null;
  xLabels.forEach((lab, i) => {
    if ((i % labelEvery !== 0 && i !== n - 1) || lab === lastShown) return;
    lastShown = lab;
    const t = svgEl('text', { x: px(i), y: height - 6, 'text-anchor': 'middle', fill: 'var(--muted)', 'font-size': 11 }, svg);
    t.textContent = lab;
  });
  if (reference) {
    svgEl('line', { x1: m.l, x2: width - m.r, y1: py(reference.value), y2: py(reference.value), stroke: 'var(--muted)', 'stroke-width': 1 }, svg);
    const t = svgEl('text', { x: m.l + 6, y: py(reference.value) - 6, 'text-anchor': 'start', fill: 'var(--text-2)', 'font-size': 11 }, svg);
    t.textContent = reference.label;
  }
  for (const s of series) {
    let d = '';
    s.values.forEach((v, i) => { if (v === null || v === undefined) return; d += `${d && s.values[i - 1] != null ? 'L' : 'M'}${px(i)},${py(v)}`; });
    svgEl('path', { d, fill: 'none', stroke: s.color, 'stroke-width': 2, 'stroke-linejoin': 'round', 'stroke-linecap': 'round' }, svg);
    if (n <= 40) {
      s.values.forEach((v, i) => { if (v !== null && v !== undefined) svgEl('circle', { cx: px(i), cy: py(v), r: 4, fill: s.color, stroke: 'var(--panel)', 'stroke-width': 2 }, svg); });
    }
  }
  if (endLabels) {
    const ends = series.map((s) => s.values.map((v, i) => [v, i]).filter(([v]) => v !== null && v !== undefined).pop()).filter(Boolean);
    const ys = ends.map(([v]) => py(v)).sort((a, b) => a - b);
    const collide = ys.some((y, i) => i > 0 && y - ys[i - 1] < 16);
    if (!collide) {
      series.forEach((s, k) => {
        const last = ends[k];
        if (!last) return;
        const t = svgEl('text', { x: px(last[1]) + 8, y: py(last[0]) + 4, fill: 'var(--text-2)', 'font-size': 12 }, svg);
        t.textContent = `${s.name} ${yFormat(last[0])}`;
      });
    }
  }
  const cross = svgEl('line', { y1: m.t, y2: height - m.b, stroke: 'var(--axis)', 'stroke-width': 1, visibility: 'hidden' }, svg);
  const hit = svgEl('rect', { x: m.l - 10, y: 0, width: width - m.l - m.r + 20, height, fill: 'transparent' }, svg);
  let focusIdx = n - 1;
  const show = (i, cx, cy) => {
    cross.setAttribute('x1', px(i)); cross.setAttribute('x2', px(i)); cross.setAttribute('visibility', 'visible');
    showTip(cx, cy, xLabels.detail ? xLabels.detail[i] : xLabels[i],
      series.map((s) => ({ color: s.color, value: s.values[i] == null ? '–' : yFormat(s.values[i]), label: series.length > 1 ? s.name : '' })));
  };
  hit.addEventListener('pointermove', (e) => {
    const r = svg.getBoundingClientRect();
    const x = (e.clientX - r.left) * (width / r.width);
    focusIdx = Math.max(0, Math.min(n - 1, Math.round(n === 1 ? 0 : ((x - m.l) / (width - m.l - m.r)) * (n - 1))));
    show(focusIdx, e.clientX, e.clientY);
  });
  hit.addEventListener('pointerleave', () => { cross.setAttribute('visibility', 'hidden'); hideTip(); });
  svg.addEventListener('keydown', (e) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    focusIdx = Math.max(0, Math.min(n - 1, focusIdx + (e.key === 'ArrowRight' ? 1 : -1)));
    const r = svg.getBoundingClientRect();
    show(focusIdx, r.left + px(focusIdx) * (r.width / width), r.top + 20);
  });
  svg.addEventListener('focus', () => { const r = svg.getBoundingClientRect(); show(focusIdx, r.left + px(focusIdx) * (r.width / width), r.top + 20); });
  svg.addEventListener('blur', () => { cross.setAttribute('visibility', 'hidden'); hideTip(); });
}

function columnChart(el, { values, xLabels, title, color = 'var(--series-1)', yFormat = fmt, yMax, height = 200 }) {
  el.replaceChildren();
  const n = values.length;
  if (n === 0) { el.innerHTML = '<div class="empty">No data in this range yet.</div>'; return; }
  const width = el.clientWidth || 600;
  const m = { l: 36, r: 10, t: 10, b: 24 };
  const ticks = niceTicks(0, Math.max(yMax ?? 0, ...values, 1), 3);
  const top = ticks[ticks.length - 1];
  const band = (width - m.l - m.r) / n;
  const barW = Math.max(2, Math.min(24, band - 2));
  const py = (v) => m.t + (1 - v / top) * (height - m.t - m.b);
  const svg = svgEl('svg', { viewBox: `0 0 ${width} ${height}`, height, role: 'img', 'aria-label': title }, el);
  for (const t of ticks) {
    svgEl('line', { x1: m.l, x2: width - m.r, y1: py(t), y2: py(t), stroke: t === 0 ? 'var(--axis)' : 'var(--grid)', 'stroke-width': 1 }, svg);
    const label = svgEl('text', { x: m.l - 6, y: py(t) + 4, 'text-anchor': 'end', fill: 'var(--muted)', 'font-size': 11 }, svg);
    label.textContent = yFormat(t);
  }
  const labelEvery = Math.max(1, Math.ceil(n / Math.max(2, Math.floor((width - m.l - m.r) / 50))));
  values.forEach((v, i) => {
    const cx = m.l + band * (i + 0.5);
    const h = py(0) - py(v);
    const g = svgEl('g', { tabindex: 0, role: 'img', 'aria-label': `${xLabels.detail ? xLabels.detail[i] : xLabels[i]}: ${yFormat(v)}` }, svg);
    if (h > 0) {
      const r = Math.min(4, barW / 2, h);
      const x = cx - barW / 2, yTop = py(v), yBase = py(0);
      svgEl('path', { d: `M${x},${yBase}V${yTop + r}Q${x},${yTop} ${x + r},${yTop}H${x + barW - r}Q${x + barW},${yTop} ${x + barW},${yTop + r}V${yBase}Z`, fill: color }, g);
    }
    const hitRect = svgEl('rect', { x: m.l + band * i, y: m.t, width: band, height: height - m.t - m.b, fill: 'transparent' }, g);
    const show = (cx2, cy2) => showTip(cx2, cy2, xLabels.detail ? xLabels.detail[i] : xLabels[i], [{ value: yFormat(v) }]);
    hitRect.addEventListener('pointermove', (e) => show(e.clientX, e.clientY));
    hitRect.addEventListener('pointerleave', hideTip);
    g.addEventListener('focus', () => { const b = g.getBoundingClientRect(); show(b.left + b.width / 2, b.top); });
    g.addEventListener('blur', hideTip);
    if (i % labelEvery === 0 || i === n - 1) {
      const t = svgEl('text', { x: cx, y: height - 6, 'text-anchor': 'middle', fill: 'var(--muted)', 'font-size': 11 }, svg);
      t.textContent = xLabels[i];
    }
  });
}

// ---- data shaping ------------------------------------------------------------
const isReal = (m) => !m.simulated && m.source !== 'practice';

function filtered() {
  const range = Number($('f-range').value);
  const hero = $('f-hero').value;
  let ms = data.matches.filter((m) => $('f-sim').checked || isReal(m));
  if (hero) ms = ms.filter((m) => m.hero === hero);
  return range ? ms.slice(-range) : ms;
}

function matchLabels(ms) {
  const labels = ms.map((_, i) => String(i + 1));
  labels.detail = ms.map((m, i) => `${tp('Match {n}', { n: i + 1 })} · ${m.hero} · ${t(m.result)} · ${fmtDate(m.ended_at)}`);
  return labels;
}

function avg(ms, fn) {
  const vals = ms.map(fn).filter((v) => v !== null && v !== undefined);
  return vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null;
}

const lh10 = (m) => (m.last_hits_at && m.last_hits_at['10:00'] !== undefined ? m.last_hits_at['10:00'] : null);
const winPct = (ms) => { const d = ms.filter((m) => m.result !== 'unknown'); return d.length ? (100 * d.filter((m) => m.result === 'win').length) / d.length : null; };

function tile(label, value, delta) {
  const el = document.createElement('div');
  el.className = 'tile';
  const l = document.createElement('div'); l.className = 'label'; l.textContent = label;
  const v = document.createElement('div'); v.className = 'value'; v.textContent = value;
  el.append(l, v);
  if (delta) {
    const d = document.createElement('div');
    d.className = `delta ${delta.tone || ''}`;
    d.textContent = delta.text;
    el.appendChild(d);
  }
  return el;
}

function deltaOf(now, before, { digits = 0, suffix = '', upIsGood = true } = {}) {
  if (now === null || before === null) return { text: t('vs previous 10: not enough matches') };
  const diff = now - before;
  const sign = diff > 0 ? '+' : diff < 0 ? '−' : '±';
  const good = diff === 0 ? '' : (diff > 0) === upIsGood ? 'good' : 'bad';
  return { text: tp('{change} vs previous 10', { change: `${sign}${Math.abs(diff).toFixed(digits)}${t(suffix)}` }), tone: good };
}

function render() {
  if (!data) return;
  const ms = filtered();
  const recent = ms.slice(-10), before = ms.slice(-20, -10);
  $('f-note').textContent = data.matches.some(isReal) || $('f-sim').checked ? '' : 'No real matches recorded yet. Tick "Include practice and simulated matches" to preview the charts.';

  const tiles = $('tiles');
  tiles.replaceChildren(
    tile('Matches in range', fmt(ms.length)),
    tile('Win rate, last 10', recent.length ? `${Math.round(winPct(recent) ?? 0)}%` : '–', before.length ? deltaOf(winPct(recent), winPct(before), { suffix: ' pts' }) : null),
    tile('Deaths per match, last 10', recent.length ? avg(recent, (m) => m.deaths).toFixed(1) : '–', before.length ? deltaOf(avg(recent, (m) => m.deaths), avg(before, (m) => m.deaths), { digits: 1, upIsGood: false }) : null),
    tile('GPM, last 10', recent.length ? fmt(avg(recent, (m) => m.gpm)) : '–', before.length ? deltaOf(avg(recent, (m) => m.gpm), avg(before, (m) => m.gpm)) : null),
    tile('Last hits at 10:00, last 10', avg(recent, lh10) !== null ? fmt(avg(recent, lh10)) : '–', before.length ? deltaOf(avg(recent, lh10), avg(before, lh10)) : null),
  );
  const mmr = data.mmr || [];
  if (mmr.length) {
    const last = mmr[mmr.length - 1].mmr, first = mmr[0].mmr;
    tiles.appendChild(tile('Latest MMR', fmt(last), mmr.length > 1 ? { text: tp('{change} since {date}', { change: `${last - first >= 0 ? '+' : '−'}${Math.abs(last - first)}`, date: fmtDate(mmr[0].date) }), tone: last > first ? 'good' : last < first ? 'bad' : '' } : null));
  }

  const mmrLabels = mmr.map((e) => fmtDate(e.date));
  mmrLabels.detail = mmr.map((e) => `${new Date(e.date).toLocaleString(locale())}${e.note ? ' · ' + t(e.note) : ''}`);
  if (mmr.length) {
    lineChart($('c-mmr'), { title: 'MMR over time', xLabels: mmrLabels, series: [{ name: 'MMR', color: 'var(--series-1)', values: mmr.map((e) => e.mmr) }] });
  } else {
    $('c-mmr').innerHTML = '<div class="empty">No MMR logged yet. Log it after each session and watch the trend.</div>';
  }

  const labels = matchLabels(ms);
  const rolling = ms.map((_, i) => (i < 4 ? null : winPct(ms.slice(Math.max(0, i - 9), i + 1))));
  lineChart($('c-winrate'), { title: 'Rolling win rate', xLabels: labels, yMin: 0, yMax: 100, yFormat: (v) => `${Math.round(v)}%`,
    reference: { value: 50, label: '50%' }, series: [{ name: 'Win rate', color: 'var(--series-1)', values: rolling }] });

  const roles = ms.map((m) => m.role).filter((r) => LH_TARGET_10[r]);
  const mainRole = roles.sort((a, b) => roles.filter((r) => r === b).length - roles.filter((r) => r === a).length)[0];
  $('lh10-sub').textContent = mainRole ? tp('Per match. Reference line is the {role} target of {n}.', { role: t(mainRole.replace('_', ' ')), n: LH_TARGET_10[mainRole] }) : t('Per match');
  lineChart($('c-lh10'), { title: 'Last hits at 10 minutes', xLabels: labels, yMin: 0,
    reference: mainRole ? { value: LH_TARGET_10[mainRole], label: tp('Target {n}', { n: LH_TARGET_10[mainRole] }) } : null,
    series: [{ name: 'Last hits', color: 'var(--series-1)', values: ms.map(lh10) }] });
  lineChart($('c-gpm'), { title: 'GPM per match', xLabels: labels, yMin: 0, series: [{ name: 'GPM', color: 'var(--series-1)', values: ms.map((m) => m.gpm) }] });
  columnChart($('c-deaths'), { title: 'Deaths per match', xLabels: labels, values: ms.map((m) => m.deaths) });

  renderCurve(ms);
  renderMistakes(ms, labels);
  renderHeroes(ms);
  renderMatches(ms);
}

function renderCurve(ms) {
  const curve = (group) => {
    const out = [];
    for (let minute = 0; minute <= 60; minute++) {
      const vals = group.map((m) => (data.last_hits_by_minute[m.match_id] || [])[minute]).filter((v) => v !== undefined && v >= 0);
      out.push(vals.length >= Math.max(1, Math.ceil(group.length / 2)) ? vals.reduce((a, b) => a + b, 0) / vals.length : null);
    }
    return out;
  };
  const recent = curve(ms.slice(-10)), before = curve(ms.slice(-20, -10));
  const lastIdx = Math.max(recent.findLastIndex((v) => v !== null), before.findLastIndex((v) => v !== null));
  const legend = $('curve-legend');
  legend.replaceChildren();
  if (lastIdx < 1) { $('c-curve').innerHTML = '<div class="empty">Needs matches recorded with per-minute samples.</div>'; return; }
  const series = [{ name: 'Latest 10', color: 'var(--series-1)', values: recent.slice(0, lastIdx + 1) }];
  if (before.some((v) => v !== null)) series.push({ name: 'Previous 10', color: 'var(--series-2)', values: before.slice(0, lastIdx + 1) });
  if (series.length > 1) {
    for (const s of series) {
      const span = document.createElement('span');
      const i = document.createElement('i'); i.style.background = s.color;
      span.append(i, document.createTextNode(s.name));
      legend.appendChild(span);
    }
  }
  const xl = Array.from({ length: lastIdx + 1 }, (_, i) => `${i}m`);
  xl.detail = xl.map((_, i) => `Minute ${i}`);
  lineChart($('c-curve'), { title: 'Average last hits by minute', xLabels: xl, yMin: 0, series, endLabels: series.length > 1, height: 240 });
}

function renderMistakes(ms, labels) {
  const box = $('c-mistakes');
  box.replaceChildren();
  const totals = data.habits.map((h) => ({ ...h, total: ms.reduce((a, m) => a + ((m.tip_counts || {})[h.id] || 0), 0) }))
    .filter((h) => h.total > 0).sort((a, b) => b.total - a.total).slice(0, 4);
  if (!totals.length) { box.innerHTML = '<div class="empty">No coaching warnings recorded in this range.</div>'; return; }
  const yMax = Math.max(...totals.flatMap((h) => ms.map((m) => (m.tip_counts || {})[h.id] || 0)));
  const charts = totals.map((h) => {
    const wrap = document.createElement('div');
    const title = document.createElement('h3');
    title.textContent = `${h.label} · ${(h.total / ms.length).toFixed(1)} per match`;
    const chart = document.createElement('div');
    chart.className = 'chart';
    wrap.append(title, chart);
    box.appendChild(wrap);
    return [h, chart];
  });
  // Measure only after every panel is in the grid, or the first ones get the full row width.
  for (const [h, chart] of charts) {
    columnChart(chart, { title: h.label, xLabels: labels, values: ms.map((m) => (m.tip_counts || {})[h.id] || 0), yMax, height: 130 });
  }
}

function cell(tr, text, cls) {
  const td = document.createElement('td');
  td.textContent = text;
  if (cls) td.className = cls;
  tr.appendChild(td);
}

function table(container, headers, rows) {
  container.replaceChildren();
  if (!rows.length) { container.innerHTML = '<div class="empty">No matches in this range.</div>'; return; }
  const t = document.createElement('table');
  const head = t.createTHead().insertRow();
  for (const [label, num] of headers) { const th = document.createElement('th'); th.textContent = label; if (num) th.className = 'num'; head.appendChild(th); }
  const body = t.createTBody();
  for (const r of rows) {
    const tr = body.insertRow();
    r.forEach(([text, cls]) => cell(tr, text, cls));
  }
  container.appendChild(t);
}

function renderHeroes(ms) {
  const byHero = new Map();
  for (const m of ms) { if (!byHero.has(m.hero)) byHero.set(m.hero, []); byHero.get(m.hero).push(m); }
  const rows = [...byHero.entries()].sort((a, b) => b[1].length - a[1].length).map(([hero, g]) => {
    const w = winPct(g), l = avg(g, lh10);
    return [[hero], [String(g.length), 'num'], [w === null ? '–' : `${Math.round(w)}%`, 'num'],
      [`${avg(g, (m) => m.kills).toFixed(1)} / ${avg(g, (m) => m.deaths).toFixed(1)} / ${avg(g, (m) => m.assists).toFixed(1)}`, 'num'],
      [fmt(avg(g, (m) => m.gpm)), 'num'], [l === null ? '–' : fmt(l), 'num'],
      [(avg(g, (m) => Object.values(m.tip_counts || {}).reduce((a, b) => a + b, 0))).toFixed(1), 'num']];
  });
  table($('t-heroes'), [['Hero'], ['Games', 1], ['Win rate', 1], ['K / D / A', 1], ['GPM', 1], ['LH @ 10', 1], ['Warnings', 1]], rows);
}

function renderMatches(ms) {
  const rows = ms.slice().reverse().map((m) => [[fmtDate(m.ended_at)], [m.hero + (m.simulated ? ` (${t('sim')})` : m.source === 'practice' ? ` (${t('practice')})` : '')], [t(m.role.replace('_', ' '))],
    [m.result, m.result], [fmtClock(m.duration_sec), 'num'], [`${m.kills}/${m.deaths}/${m.assists}`, 'num'], [String(m.last_hits), 'num'],
    [lh10(m) === null ? '–' : String(lh10(m)), 'num'], [String(m.gpm), 'num'], [String(m.xpm), 'num'],
    [String(Object.values(m.tip_counts || {}).reduce((a, b) => a + b, 0)), 'num']]);
  table($('t-matches'), [['Date'], ['Hero'], ['Role'], ['Result'], ['Length', 1], ['K/D/A', 1], ['LH', 1], ['LH @ 10', 1], ['GPM', 1], ['XPM', 1], ['Warnings', 1]], rows);
  $('csv-dir').textContent = data.dir;
  const files = $('csv-files');
  files.replaceChildren();
  for (const f of data.files || []) {
    const a = document.createElement('a');
    a.className = 'btn';
    a.href = `/api/stats/files/${encodeURIComponent(f.name)}`;
    a.textContent = `${f.name} (${Math.max(1, Math.round(f.size / 1024))} KB)`;
    files.appendChild(a);
  }
}

async function loadGoals() {
  let g;
  try { g = await api('/api/goals'); } catch (e) { return; }
  const list = g.progress || [];
  const history = (g.history || []).filter((x) => x.week !== g.week);
  $('goals').innerHTML = (list.length
    ? `<div class="goals">${list.map((p) => `<div class="goal-card ${p.done ? 'done' : ''}">
        <div class="top"><b>${esc(p.label)}</b><span class="note">${Math.min(p.met, g.done_after)}/${g.done_after}</span></div>
        <div class="dots">${Array.from({ length: g.done_after }, (_, i) => `<i class="${i < p.met ? 'on' : ''}"></i>`).join('')}</div>
        <small>${tp(p.tried === 1 ? '{n} match measured' : '{n} matches measured', { n: p.tried })}${p.streak > 1 ? ` · ${tp('{n} in a row', { n: p.streak })}` : ''}</small>
      </div>`).join('')}</div>`
    : '<div class="empty">No goals this week yet. The next match review sets them.</div>')
    + (history.length ? `<details style="margin-top:10px"><summary>Earlier goals</summary><div class="table-wrap"><table><thead><tr><th>Week</th><th>Goal</th></tr></thead><tbody>${history.map((x) => `<tr><td>${esc(x.week)}</td><td>${esc(x.label)}</td></tr>`).join('')}</tbody></table></div></details>` : '');
}

async function loadReviews() {
  const box = $('reviews');
  const reviews = await (await fetch('/api/reviews')).json();
  box.replaceChildren();
  if (!reviews || !reviews.length) { box.innerHTML = '<div class="empty">No reviews yet. They appear after each match when the Claude coach is on.</div>'; return; }
  for (const r of reviews) {
    const el = document.createElement('div');
    el.className = 'review';
    const head = document.createElement('div');
    head.className = 'head';
    for (const text of [fmtDate(r.date), r.hero, t(r.result)]) { const s = document.createElement('span'); s.textContent = text; head.appendChild(s); }
    const focus = document.createElement('div'); focus.className = 'focus'; focus.textContent = r.next_game_focus;
    const summary = document.createElement('p'); summary.textContent = r.summary;
    const list = document.createElement('ul');
    for (const item of r.improve || []) { const li = document.createElement('li'); li.textContent = item; list.appendChild(li); }
    const details = document.createElement('details');
    const sum = document.createElement('summary'); sum.textContent = 'Summary and goals';
    details.append(sum, summary, list);
    el.append(head, focus, details);
    box.appendChild(el);
  }
}

async function load() {
  loadReviews();
  loadGoals();
  const res = await fetch('/api/stats');
  data = await res.json();
  data.matches = data.matches || [];
  const heroes = [...new Set(data.matches.map((m) => m.hero))].sort();
  const sel = $('f-hero'), current = sel.value;
  sel.replaceChildren(new Option('All heroes', ''));
  for (const h of heroes) sel.appendChild(new Option(h, h));
  sel.value = heroes.includes(current) ? current : '';
  if (!data.matches.some(isReal) && data.matches.length) $('f-sim').checked = true;
  render();
}

for (const id of ['f-range', 'f-hero', 'f-sim']) $(id).addEventListener('change', render);
let resizeTimer;
window.addEventListener('resize', () => { clearTimeout(resizeTimer); resizeTimer = setTimeout(render, 150); });
$('mmr-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const res = await fetch('/api/mmr', { method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mmr: Number($('mmr-value').value), note: $('mmr-note').value }) });
  if (!res.ok) { alert(await res.text()); return; }
  $('mmr-value').value = ''; $('mmr-note').value = '';
  load();
});
events.on('match', () => load());
// The first load waits for the settings, which bring the language, so charts aren't labelled in English.
let loaded = false;
onSettings(() => { if (!loaded) { loaded = true; load(); } });
events.start();
