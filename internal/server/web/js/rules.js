import { $, esc, clockStr, ROLE_NAMES, CATEGORY_NAMES, api, toast, t, tp, events, onSettings, loadSettings, saveSettings, cfg } from './app.js';

const OPS = {
  number: [['lt', 'is below'], ['le', 'is at most'], ['eq', 'is'], ['ne', 'is not'], ['ge', 'is at least'], ['gt', 'is above'], ['between', 'is between']],
  bool: [['true', 'yes'], ['false', 'no']],
  text: [['is', 'is'], ['is_not', 'is not'], ['contains', 'contains']],
};
const SEVERITY = [['info', 'Info (blue)'], ['warn', 'Warning (amber)'], ['urgent', 'Urgent (red, interrupts voice)']];
let data = null, catalog = { items: [], heroes: [] }, filter = 'all', selected = null, draft = null, checkTimer = null, recordings = [];

const fieldOf = (id) => data.fields.find((f) => f.id === id);
const isCustom = (id) => String(id).startsWith('custom-');
let draftBuiltin = null;
let foldersOpen = (() => { try { return localStorage.getItem('rules-folder') === '1'; } catch (e) { return false; } })();
const parseClock = (v) => {
  const m = String(v).trim().match(/^(-)?(\d+):(\d{1,2})$/);
  if (m) return (m[1] ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3]));
  return Number(v) || 0;
};
const itemName = (id) => (catalog.items.find((i) => i.id === id) || {}).name || id;

async function load() {
  data = await api('/api/rules');
  data.custom = data.custom || [];
  data.overrides = data.overrides || {};
  data.fired = data.fired || {};
  $('template').innerHTML = '<option value="">Start from a template…</option>' + data.templates.map((t, i) => `<option value="${i}">${esc(t.name)}</option>`).join('');
  renderFilters();
  renderList();
  if (selected) select(selected, true);
}

function allRules() {
  const disabled = new Set((cfg && cfg.settings.disabled_rules) || []);
  const builtin = data.builtin.map((r) => ({ id: r.id, name: r.label, category: r.category, builtin: true, enabled: !disabled.has(r.id), changed: !!data.overrides[r.id], rule: r }));
  const custom = data.custom.map((r) => ({ id: r.id, name: r.name, category: r.category, builtin: false, enabled: r.enabled, changed: false, rule: r }));
  return [...custom, ...builtin];
}

function renderFilters() {
  $('filters').innerHTML = [['all', 'All'], ['mine', 'Mine'], ['builtin', 'Built-in'], ['changed', 'Changed'], ['off', 'Switched off']]
    .map(([k, n]) => `<button data-filter="${k}" class="${filter === k ? 'on' : ''}">${n}</button>`).join('');
}

function renderList() {
  if (!data) return;
  const q = $('search').value.trim().toLowerCase();
  const rules = allRules().filter((r) => (!q || r.name.toLowerCase().includes(q))
    && (filter === 'all' || filter === 'mine' && !r.builtin || filter === 'builtin' && r.builtin || filter === 'changed' && r.changed || filter === 'off' && !r.enabled));
  const n = data.matches;
  const item = (r) => {
    const fired = data.fired[r.id] || 0;
    const roles = (r.builtin ? (data.overrides[r.id] && data.overrides[r.id].roles) || r.rule.roles : r.rule.roles) || [];
    return `<div class="item ${r.id === selected ? 'on' : ''} ${r.enabled ? '' : 'off'}" data-id="${esc(r.id)}">
      <input type="checkbox" data-toggle="${esc(r.id)}" ${r.enabled ? 'checked' : ''} aria-label="Rule on">
      <div><span class="title">${esc(r.name)}</span><span class="badges">${r.builtin ? '' : '<span class="badge">mine</span>'}${r.changed ? '<span class="badge">changed</span>' : ''}</span>
        <small>${roles.length ? roles.map((x) => t(ROLE_NAMES[x].replace(/ \(\d\)/, ''))).join(', ') : t('All positions')}${n ? ` · ${tp('{fired}× in your last {n} matches', { fired, n })}` : ''}</small></div>
    </div>`;
  };
  const byCategory = (list) => {
    const byCat = {};
    for (const r of list) (byCat[r.category] ||= []).push(r);
    return Object.entries(byCat).map(([cat, rs]) => `<div class="cat">${esc(CATEGORY_NAMES[cat] || cat)}</div>` + rs.map(item).join('')).join('');
  };
  const mine = rules.filter((r) => !r.builtin), builtin = rules.filter((r) => r.builtin);
  let html = '';
  if (mine.length) html += `<div class="cat">My rules</div>` + mine.map(item).join('');
  if (builtin.length) {
    const changed = builtin.filter((r) => r.changed).length;
    const open = foldersOpen || !!q || filter === 'builtin' || builtin.some((r) => r.id === selected);
    html += `<details class="folder" ${open ? 'open' : ''}><summary>Default rules · ${builtin.length}${changed ? ` · ${changed} changed` : ''}</summary>
      <div class="body">${byCategory(builtin)}</div></details>`;
  }
  $('list').innerHTML = html || '<div class="empty">No rules match.</div>';
  const folder = $('list').querySelector('.folder');
  if (folder) folder.addEventListener('toggle', () => { foldersOpen = folder.open; try { localStorage.setItem('rules-folder', folder.open ? '1' : '0'); } catch (e) { /* private window */ } });
}

function select(id, keepDraft) {
  selected = id;
  const r = allRules().find((x) => x.id === id);
  renderList();
  clearInterval(checkTimer);
  if (!r) { $('editor').innerHTML = '<div class="placeholder">Pick a rule to change it, or make a new one.</div>'; return; }
  draftBuiltin = r.builtin ? r.id : null;
  if (!keepDraft || !draft || draft.id !== id) {
    const o = data.overrides[id] || {};
    draft = structuredClone(r.builtin ? (o.spec || r.rule.spec) : r.rule);
    draft.if ||= [];
  }
  renderCustom();
}

function newRule(from) {
  draftBuiltin = null;
  const base = from ? structuredClone(from) : { name: 'New rule', category: 'economy', match: 'all', when: { type: 'state', for: 3 }, if: [{ field: 'gold', op: 'ge', num: 2000 }], then: { text: 'You have {gold} gold. Buy something', severity: 'info' }, cooldown: 60 };
  base.id = '';
  base.enabled = true;
  draft = base;
  selected = null;
  renderList();
  renderCustom();
}

function fieldOptions(selectedID) {
  const groups = {};
  for (const f of data.fields) (groups[f.group] ||= []).push(f);
  return Object.entries(groups).map(([g, list]) => `<optgroup label="${esc(g)}">${list.map((f) => `<option value="${f.id}" ${f.id === selectedID ? 'selected' : ''}>${esc(f.label)}</option>`).join('')}</optgroup>`).join('');
}

function valueInput(c, i, f) {
  if (f.type === 'bool') return '<span></span>';
  if (f.type === 'text') return `<input data-i="${i}" data-k="text" value="${esc(c.text || '')}" placeholder="${f.id === 'role' ? 'mid' : 'text'}">`;
  const show = (v) => (f.unit === 'clock' ? clockStr(v || 0) : (v ?? 0));
  if (c.op === 'between') return `<span class="between"><input data-i="${i}" data-k="num" value="${show(c.num)}"><input data-i="${i}" data-k="num2" value="${show(c.num2)}"></span>`;
  return `<input data-i="${i}" data-k="num" value="${show(c.num)}" placeholder="${f.unit === 'clock' ? 'mm:ss' : '0'}">`;
}

function renderCustom() {
  const r = draft, w = r.when;
  r.if ||= [];
  const eventInfo = data.events.find((e) => e.id === w.event) || {};
  const builtin = draftBuiltin, changed = builtin && !!data.overrides[builtin];
  const on = builtin ? !(cfg.settings.disabled_rules || []).includes(builtin) : r.enabled;
  $('editor').innerHTML = `
    ${builtin ? `<div class="line" style="margin-bottom:6px"><span class="badge">built-in</span>${changed ? '<span class="badge">changed</span>' : ''}
      <span class="muted">Change anything here. "Reset to default" puts the trainer's version back.</span></div>` : ''}
    <div class="line" style="justify-content:space-between">
      <input id="c-name" value="${esc(r.name)}" style="flex:1;font-size:17px;font-weight:600" aria-label="Rule name">
      <select id="c-cat" aria-label="Category">${data.categories.map((c) => `<option value="${c}" ${c === r.category ? 'selected' : ''}>${esc(CATEGORY_NAMES[c] || c)}</option>`).join('')}</select>
      <label class="line" style="margin:0"><input type="checkbox" id="c-enabled" ${on ? 'checked' : ''}> On</label>
    </div>

    <div class="block"><div class="label">WHEN</div>
      <div class="line">
        <select id="c-when">
          <option value="state" ${w.type === 'state' ? 'selected' : ''}>While these are true</option>
          <option value="change" ${w.type === 'change' ? 'selected' : ''}>The moment these become true</option>
          <option value="event" ${w.type === 'event' ? 'selected' : ''}>When something happens</option>
          <option value="schedule" ${w.type === 'schedule' ? 'selected' : ''}>At game times</option>
          <option value="after" ${w.type === 'after' ? 'selected' : ''}>A while after something happens</option>
        </select>
        ${w.type === 'state' ? `<label>for <input class="short" type="number" min="0" id="c-for" value="${w.for || 0}"> seconds</label>` : ''}
        ${w.type === 'after' ? `<select id="c-event">${data.events.map((e) => `<option value="${e.id}" ${e.id === w.event ? 'selected' : ''}>${esc(e.label)}</option>`).join('')}</select>
          <label>then wait <input class="short" type="number" min="0" id="c-after" value="${w.after || 0}"> seconds</label>` : ''}
        ${w.type === 'event' ? `<select id="c-event">${data.events.map((e) => `<option value="${e.id}" ${e.id === w.event ? 'selected' : ''}>${esc(e.label)}</option>`).join('')}</select>
          ${eventInfo.arg === 'item' ? `<input id="c-event-arg" list="item-list" value="${esc(w.arg ? itemName(w.arg) : '')}" placeholder="any item">` : ''}` : ''}
        ${w.type === 'schedule' ? `<label>first at <input class="short" id="c-first" value="${clockStr(w.first || 0)}"></label>
          <label>then every <input class="short" id="c-every" value="${clockStr(w.every || 0)}"></label>
          <label>until <input class="short" id="c-until" value="${w.until ? clockStr(w.until) : ''}" placeholder="end"></label>
          <label>warn <input class="short" type="number" min="0" id="c-lead" value="${w.lead || 0}"> s early</label>` : ''}
      </div>
      <div class="line"><label>Positions</label><span class="roles">${cfg.roles.map((x) => `<label><input type="checkbox" data-crole="${x}" ${(r.roles || []).includes(x) ? 'checked' : ''}>${ROLE_NAMES[x]}</label>`).join('')}</span><span class="muted">none ticked = all</span></div>
      <div class="line"><label>Heroes</label><input id="c-heroes" list="hero-list" placeholder="add a hero (empty = all)" style="flex:1">
        <span class="roles">${(r.heroes || []).map((h) => `<label>${esc((catalog.heroes.find((x) => Number(x.id) === h) || {}).name || h)} <button class="btn small" data-rmhero="${h}">×</button></label>`).join('')}</span></div>
    </div>

    <div class="block"><div class="label">IF</div>
      ${r.if.length > 1 ? `<div class="line"><select id="c-match"><option value="all" ${r.match === 'all' ? 'selected' : ''}>All of these</option><option value="any" ${r.match === 'any' ? 'selected' : ''}>Any of these</option></select></div>` : ''}
      ${r.if.map((c, i) => {
        const f = fieldOf(c.field) || data.fields[0];
        return `<div class="cond">
          <select data-i="${i}" data-k="field">${fieldOptions(c.field)}</select>
          ${f.arg ? `<input data-i="${i}" data-k="arg" list="${f.arg === 'item' ? 'item-list' : ''}" value="${esc(f.arg === 'item' && c.arg ? itemName(c.arg) : c.arg || '')}" placeholder="${f.arg === 'item' ? 'which item' : 'ability name'}">` : '<span></span>'}
          <select data-i="${i}" data-k="op">${OPS[f.type].map(([v, n]) => `<option value="${v}" ${v === c.op ? 'selected' : ''}>${n}</option>`).join('')}</select>
          ${valueInput(c, i, f)}
          <span class="live" id="live-${i}"></span>
          <button class="btn small" data-rm="${i}" aria-label="Remove condition">×</button>
        </div>`;
      }).join('')}
      <div class="line"><button class="btn small" id="c-add">+ Add condition</button>${r.if.length ? '' : '<span class="muted">No conditions: the rule fires on its trigger alone.</span>'}<span class="spacer"></span><span class="muted" id="live-summary"></span></div>
    </div>

    <div class="block"><div class="label">THEN</div>
      <div class="line"><label>Show</label><input id="c-text" value="${esc(r.then.text)}" style="flex:1">
        <select id="c-insert" aria-label="Insert a value"><option value="">Insert value…</option>${data.fields.filter((f) => !f.arg).map((f) => `<option value="${f.id}">${esc(f.label)}</option>`).join('')}${w.type === 'schedule' ? '<option value="at">Scheduled time</option><option value="in">Seconds until it</option>' : ''}${w.type === 'after' ? '<option value="at">The time it fires</option><option value="since">Seconds since the event</option>' : ''}</select></div>
      <div class="line"><label>Say</label><input id="c-speech" value="${esc(r.then.speech || '')}" placeholder="same as the text" style="flex:1" ${r.then.silent ? 'disabled' : ''}>
        <label><input type="checkbox" id="c-silent" ${r.then.silent ? 'checked' : ''}> don't speak</label></div>
      <div class="line"><label>As</label><select id="c-sev">${SEVERITY.map(([v, n]) => `<option value="${v}" ${v === r.then.severity ? 'selected' : ''}>${n}</option>`).join('')}</select>
        <label><input type="checkbox" id="c-mistake" ${r.then.mistake ? 'checked' : ''}> count as a mistake</label>
        ${r.then.mistake ? `<input id="c-advice" value="${esc(r.then.advice || '')}" placeholder="advice shown in your habits" style="flex:1">` : ''}</div>
      ${w.type === 'state' || w.type === 'change' ? `<div class="line"><label>Repeat</label>
        <label><input type="checkbox" id="c-once" ${r.once ? 'checked' : ''}> once per match</label>
        ${r.once ? '' : `<label>at most every <input class="short" type="number" min="0" id="c-cooldown" value="${r.cooldown || 0}"> seconds</label>`}</div>` : ''}
      <div class="line"><label>Stop after</label><input class="short" type="number" min="0" id="c-max" value="${r.max || 0}">
        <span class="muted">alerts in one match (0 = no limit)</span></div>
    </div>

    <div class="block"><div class="label">TRY IT</div>
      <div class="line"><select id="c-rec" style="flex:1">${recordings.length ? recordings.map((x) => `<option>${esc(x.name)}</option>`).join('') : '<option value="">No recordings yet: matches are recorded automatically</option>'}</select>
        <button class="btn" id="c-test" ${recordings.length ? '' : 'disabled'}>Run on recording</button></div>
      <div id="c-fires"></div>
    </div>

    <div class="actions">
      <button class="btn primary" id="c-save">${builtin || r.id ? 'Save' : 'Create rule'}</button>
      ${builtin ? `<button class="btn" id="c-dup">Copy to my rules</button>${changed ? '<button class="btn" id="c-reset">Reset to default</button>' : ''}`
        : r.id ? '<button class="btn" id="c-dup">Duplicate</button><button class="btn danger" id="c-del">Delete</button>' : ''}
    </div>`;
  bindCustom();
  clearInterval(checkTimer);
  liveCheck();
  checkTimer = setInterval(liveCheck, 2000);
}

function itemID(text) {
  const t = String(text).trim();
  const hit = catalog.items.find((i) => i.name.toLowerCase() === t.toLowerCase() || i.id === t);
  return hit ? hit.id : t.toLowerCase().replace(/^item_/, '').replace(/\s+/g, '_');
}

function bindCustom() {
  const r = draft, ed = $('editor');
  const rerender = () => renderCustom();
  $('c-name').addEventListener('input', (e) => { r.name = e.target.value; });
  $('c-cat').addEventListener('change', (e) => { r.category = e.target.value; });
  $('c-enabled').addEventListener('change', async (e) => {
    if (!draftBuiltin) { r.enabled = e.target.checked; return; }
    const disabled = new Set(cfg.settings.disabled_rules || []);
    e.target.checked ? disabled.delete(draftBuiltin) : disabled.add(draftBuiltin);
    await saveSettings({ disabled_rules: [...disabled] });
    renderList();
  });
  $('c-when').addEventListener('change', (e) => {
    r.when = { type: e.target.value };
    if (r.when.type === 'event' || r.when.type === 'after') r.when.event = 'died';
    if (r.when.type === 'schedule') Object.assign(r.when, { first: 300, every: 120, lead: 15 });
    if (r.when.type === 'state' && !r.cooldown && !r.once && !r.each) r.cooldown = 60;
    rerender();
  });
  const num = (id, key, clock) => $(id) && $(id).addEventListener('change', (e) => { r.when[key] = clock ? parseClock(e.target.value) : Number(e.target.value); });
  num('c-for', 'for'); num('c-first', 'first', true); num('c-every', 'every', true); num('c-until', 'until', true); num('c-lead', 'lead'); num('c-after', 'after');
  if ($('c-event')) $('c-event').addEventListener('change', (e) => { r.when.event = e.target.value; r.when.arg = ''; rerender(); });
  if ($('c-event-arg')) $('c-event-arg').addEventListener('change', (e) => { r.when.arg = e.target.value ? itemID(e.target.value) : ''; });
  ed.querySelectorAll('[data-crole]').forEach((el) => el.addEventListener('change', () => {
    r.roles = [...ed.querySelectorAll('[data-crole]:checked')].map((x) => x.dataset.crole);
  }));
  $('c-heroes').addEventListener('change', (e) => {
    const hero = catalog.heroes.find((h) => h.name.toLowerCase() === e.target.value.trim().toLowerCase());
    if (hero) { r.heroes = [...new Set([...(r.heroes || []), Number(hero.id)])]; rerender(); }
  });
  ed.querySelectorAll('[data-rmhero]').forEach((el) => el.addEventListener('click', () => { r.heroes = r.heroes.filter((h) => h !== Number(el.dataset.rmhero)); rerender(); }));
  if ($('c-match')) $('c-match').addEventListener('change', (e) => { r.match = e.target.value; });
  ed.querySelectorAll('.cond [data-k]').forEach((el) => el.addEventListener('change', (e) => {
    const c = r.if[Number(el.dataset.i)], k = el.dataset.k;
    if (k === 'field') {
      const f = fieldOf(e.target.value);
      r.if[Number(el.dataset.i)] = { field: f.id, op: OPS[f.type][f.type === 'number' ? 4 : 0][0] };
      rerender();
    } else if (k === 'op') { c.op = e.target.value; rerender(); }
    else if (k === 'arg') c.arg = fieldOf(c.field).arg === 'item' ? itemID(e.target.value) : e.target.value.trim();
    else if (k === 'text') c.text = e.target.value;
    else c[k] = fieldOf(c.field).unit === 'clock' ? parseClock(e.target.value) : Number(e.target.value);
  }));
  ed.querySelectorAll('[data-rm]').forEach((el) => el.addEventListener('click', () => { r.if.splice(Number(el.dataset.rm), 1); rerender(); }));
  $('c-add').addEventListener('click', () => { r.if.push({ field: 'hp_pct', op: 'le', num: 30 }); rerender(); });
  $('c-text').addEventListener('input', (e) => { r.then.text = e.target.value; });
  $('c-insert').addEventListener('change', (e) => {
    if (!e.target.value) return;
    r.then.text = `${r.then.text}{${e.target.value}}`;
    $('c-text').value = r.then.text;
    e.target.value = '';
  });
  $('c-speech').addEventListener('input', (e) => { r.then.speech = e.target.value; });
  $('c-silent').addEventListener('change', (e) => { r.then.silent = e.target.checked; rerender(); });
  $('c-sev').addEventListener('change', (e) => { r.then.severity = e.target.value; });
  $('c-mistake').addEventListener('change', (e) => { r.then.mistake = e.target.checked; rerender(); });
  if ($('c-advice')) $('c-advice').addEventListener('input', (e) => { r.then.advice = e.target.value; });
  if ($('c-once')) $('c-once').addEventListener('change', (e) => { r.once = e.target.checked; rerender(); });
  if ($('c-cooldown')) $('c-cooldown').addEventListener('change', (e) => { r.cooldown = Number(e.target.value); });
  $('c-max').addEventListener('change', (e) => { r.max = Number(e.target.value); });

  $('c-save').addEventListener('click', async () => {
    if (draftBuiltin) {
      try {
        await api(`/api/rules/builtin/${draftBuiltin}`, { method: 'PUT', body: { spec: r } });
        toast('Saved');
        await load();
        select(draftBuiltin, true);
      } catch (e) { toast(e.message, true); }
      return;
    }
    try {
      await api('/api/rules/custom', { method: 'PUT', body: r });
      toast('Saved');
      const before = new Set(data.custom.map((x) => x.id));
      await load();
      const saved = data.custom.find((x) => (r.id ? x.id === r.id : !before.has(x.id)));
      if (saved) select(saved.id);
    } catch (e) { toast(e.message, true); }
  });
  if ($('c-reset')) $('c-reset').addEventListener('click', async () => {
    try { await api(`/api/rules/builtin/${draftBuiltin}`, { method: 'DELETE' }); toast('Back to the default'); draft = null; await load(); select(draftBuiltin); } catch (e) { toast(e.message, true); }
  });
  if ($('c-dup')) $('c-dup').addEventListener('click', () => newRule({ ...r, name: `${r.name} copy` }));
  if ($('c-del')) $('c-del').addEventListener('click', async () => {
    if (!confirm(`Delete "${r.name}"?`)) return;
    try { await api(`/api/rules/custom/${r.id}`, { method: 'DELETE' }); selected = null; draft = null; await load(); select(null); } catch (e) { toast(e.message, true); }
  });
  $('c-test').addEventListener('click', async () => {
    $('c-fires').innerHTML = '<div class="muted">Replaying…</div>';
    try {
      const fires = await api('/api/rules/test', { method: 'POST', body: { rule: r, recording: $('c-rec').value } });
      $('c-fires').innerHTML = fires.length
        ? `<div class="muted" style="margin-top:6px">${tp('Fired {n} times', { n: fires.length })}${fires.length >= 200 ? ` ${t('(showing the first 200)')}` : ''}:</div><ul class="fires">${fires.map((f) => `<li><span class="num muted">${clockStr(f.clock)}</span><span>${esc(f.text)}</span></li>`).join('')}</ul>`
        : '<div class="muted" style="margin-top:6px">It never fired in that match.</div>';
    } catch (e) { $('c-fires').innerHTML = `<div class="result bad">${esc(e.message)}</div>`; }
  });
}

async function liveCheck() {
  if (!draft || !$('live-summary')) return;
  try {
    const res = await api('/api/rules/check', { method: 'POST', body: draft });
    if (res.error) { $('live-summary').textContent = res.error; draft.if.forEach((_, i) => { if ($(`live-${i}`)) $(`live-${i}`).textContent = ''; }); return; }
    $('live-summary').textContent = res.match ? 'Conditions hold right now' : 'Conditions don\'t hold right now';
    res.conditions.forEach((c, i) => {
      const el = $(`live-${i}`);
      if (el) { el.className = `live ${c.ok ? 'ok' : 'no'}`; el.textContent = `${c.ok ? '✓' : '✗'} now ${c.value}`; }
    });
  } catch (e) { /* rule incomplete while typing */ }
}

$('list').addEventListener('click', async (e) => {
  const toggle = e.target.dataset.toggle;
  if (toggle) {
    e.stopPropagation();
    if (isCustom(toggle)) {
      const spec = { ...data.custom.find((x) => x.id === toggle), enabled: e.target.checked };
      try { await api('/api/rules/custom', { method: 'PUT', body: spec }); await load(); } catch (err) { toast(err.message, true); }
    } else {
      const disabled = new Set(cfg.settings.disabled_rules || []);
      e.target.checked ? disabled.delete(toggle) : disabled.add(toggle);
      await saveSettings({ disabled_rules: [...disabled] });
      renderList();
    }
    return;
  }
  const item = e.target.closest('.item');
  if (item) select(item.dataset.id);
});
$('filters').addEventListener('click', (e) => { if (e.target.dataset.filter) { filter = e.target.dataset.filter; renderFilters(); renderList(); } });
$('search').addEventListener('input', renderList);
$('new').addEventListener('click', () => newRule());
$('template').addEventListener('change', (e) => { if (e.target.value !== '') newRule(data.templates[Number(e.target.value)]); e.target.value = ''; });
$('import').addEventListener('change', async (e) => {
  const file = e.target.files[0];
  if (!file) return;
  try {
    const res = await api('/api/rules/import', { method: 'POST', body: JSON.parse(await file.text()) });
    toast(`Imported ${res.added} ${res.added === 1 ? 'rule' : 'rules'}${res.skipped ? `. Skipped: ${res.skipped}` : ''}`, !!res.skipped);
    await load();
  } catch (err) { toast(err.message, true); }
  e.target.value = '';
});

onSettings(() => renderList());
events.on('rules', () => load());
events.onOpen(async () => {
  try {
    [catalog, recordings] = await Promise.all([api('/api/catalog'), api('/api/recordings')]);
    recordings = recordings || [];
    catalog.items = catalog.items || [];
    catalog.heroes = catalog.heroes || [];
    $('item-list').innerHTML = catalog.items.map((i) => `<option value="${esc(i.name)}">`).join('');
    $('hero-list').innerHTML = catalog.heroes.map((h) => `<option value="${esc(h.name)}">`).join('');
  } catch (e) { /* pickers stay empty */ }
  await loadSettings().catch(() => {});
  await load();
  const [kind, value] = location.hash.slice(1).split('=');
  if (kind === 'rule') select(decodeURIComponent(value));
  if (kind === 'template' && data.templates[Number(value)]) newRule(data.templates[Number(value)]);
});
events.start();
