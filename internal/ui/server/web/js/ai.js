import { $, esc, api, toast, t, events, onSettings, saveSettings, cfg } from './app.js';

const STATE_TEXT = { ready: 'Ready', missing: 'Not installed', login: 'Needs login', key: 'Needs an API key', error: 'Problem', '': 'Checking…' };
let providers = [], results = {}, setup = { running: false };

const byID = (id) => providers.find((p) => p.id === id);

function jobChoice(job) {
  const ai = cfg.settings.ai;
  return job === 'live' ? ai.live : job === 'reviews' ? ai.reviews : ai.fallback;
}

function renderJobs() {
  if (!cfg || !providers.length) return;
  const ai = cfg.settings.ai;
  $('live-on').checked = ai.enabled;
  $('review-on').checked = ai.review;
  $('draft-on').checked = ai.draft;
  if (![...$('interval').options].some((o) => Number(o.value) === ai.interval)) $('interval').add(new Option(`every ${ai.interval} s`, ai.interval));
  $('interval').value = String(ai.interval);
  for (const row of document.querySelectorAll('.row[data-job]')) {
    const job = row.dataset.job, c = jobChoice(job), p = byID(c.provider);
    if (row.contains(document.activeElement)) continue;
    const options = providers.map((x) => `<option value="${x.id}" ${x.id === c.provider ? 'selected' : ''}>${esc(t(x.name))}${x.status.state && x.status.state !== 'ready' ? ` (${t(STATE_TEXT[x.status.state]).toLowerCase()})` : ''}</option>`).join('');
    row.innerHTML = `
      <select data-field="provider" aria-label="Provider">${job === 'fallback' ? `<option value="" ${c.provider ? '' : 'selected'}>No fallback</option>` : ''}${options}</select>
      ${modelPicker(job, p, c)}
      <select data-field="effort" aria-label="Thinking effort" ${p && p.efforts && p.efforts.length ? '' : 'disabled'}>
        <option value="">Default effort</option>${(p && p.efforts || []).map((e) => `<option value="${e}" ${e === c.effort ? 'selected' : ''}>${e} effort</option>`).join('')}
      </select>`;
    if (p) loadModels(job, p);
  }
}

// A list of models, not a datalist: a datalist hides every option that doesn't match what
// is already typed, which left the picker showing one model.
function modelPicker(job, p, c) {
  if (!p) return '<select disabled aria-label="Model"></select>';
  const models = modelCache[p.id] || p.models || [];
  if (!models.length && p.kind === 'api' && p.status.state !== 'ready') {
    return `<select disabled aria-label="Model"><option>${t('Connect it to see its models')}</option></select>`;
  }
  const rec = p.recommended ? p.recommended[job === 'reviews' ? 'review' : 'live'] : '';
  const known = models.some((m) => (m.id || '') === (c.model || ''));
  const label = (m) => `${m.name || m.id || t('Default model')}${m.id && m.id === rec ? ` · ${t('recommended')}` : ''}`;
  return `<select data-field="model" aria-label="Model">
    ${models.map((m) => `<option value="${esc(m.id)}" ${(m.id || '') === (c.model || '') ? 'selected' : ''}>${esc(label(m))}</option>`).join('')}
    ${known || !c.model ? '' : `<option value="${esc(c.model)}" selected>${esc(c.model)}</option>`}
    <option value="__custom">${t('Another model…')}</option>
  </select>`;
}

const modelCache = {};
async function loadModels(job, p) {
  if (modelCache[p.id] || p.status.state !== 'ready') return;
  try {
    modelCache[p.id] = await api(`/api/ai/providers/${p.id}/models`);
    renderJobs();
  } catch (e) { /* the provider's own list stays */ }
}

document.querySelectorAll('.row[data-job]').forEach((row) => row.addEventListener('change', (e) => {
  const job = row.dataset.job, field = e.target.dataset.field;
  let value = e.target.value;
  if (field === 'model' && value === '__custom') {
    value = (prompt(t('Model name, as the provider writes it:'), jobChoice(job).model || '') || '').trim();
    if (!value) { renderJobs(); return; }
  }
  const next = { ...jobChoice(job), [field]: value };
  if (field === 'provider') {
    const p = byID(next.provider);
    // The trainer picks the model from what this provider offers.
    next.model = '';
    next.effort = p && p.efforts && p.efforts.includes('low') ? (job === 'reviews' ? 'medium' : 'low') : '';
  }
  saveSettings({ ai: { [job]: next } });
}));

function uses(id) {
  const ai = cfg ? cfg.settings.ai : {};
  return [[ai.live, 'live tips'], [ai.reviews, 'reviews'], [ai.fallback, 'fallback']].filter(([c]) => c && c.provider === id).map(([, n]) => n);
}

function providerCard(p) {
  const st = p.status.state || '';
  const inUse = uses(p.id);
  let actions = '';
  if (st === 'missing' && p.can_install) actions += `<button class="btn primary" data-act="install" data-id="${p.id}" ${setup.running ? 'disabled' : ''}>Install</button>`;
  if (st === 'login') actions += `<button class="btn primary" data-act="login" data-id="${p.id}">${t('Log in')}</button>`;
  if (st === 'ready' && p.kind === 'cli') actions += `<button class="btn small" data-act="switch" data-id="${p.id}" title="${t('Sign out and sign in as another account')}">${t('Switch account')}</button>`;
  if (p.kind === 'api') {
    if (p.custom_url) actions += `<input type="text" data-url="${p.id}" placeholder="http://localhost:11434/v1" value="${esc(cfg ? cfg.settings.ai.custom_url || '' : '')}">`;
    actions += `<input type="password" data-key="${p.id}" placeholder="${p.key_masked ? `Key ${esc(p.key_masked)} saved · paste a new one` : p.custom_url ? 'API key (if the server needs one)' : 'Paste your API key'}" autocomplete="off">
      <button class="btn" data-act="save-key" data-id="${p.id}">Save</button>
      ${p.key_masked ? `<button class="btn small" data-act="remove-key" data-id="${p.id}">Remove key</button>` : ''}
      ${p.key_url ? `<a class="btn small" href="${esc(p.key_url)}" target="_blank" rel="noopener">Get a key</a>` : ''}`;
  }
  if (st === 'ready') actions += `<button class="btn" data-act="test" data-id="${p.id}">Test</button>`;
  actions += `<button class="btn small" data-act="check" data-id="${p.id}">Check</button>`;
  const r = results[p.id];
  return `<div class="provider" id="p-${p.id}">
    <div class="top"><span class="dot ${st}"></span><span class="name">${esc(p.name)}</span><span class="badge">${esc(p.billing)}</span>
      ${inUse.map((u) => `<span class="badge use">${u}</span>`).join('')}<span class="spacer"></span><span class="muted">${STATE_TEXT[st] || st}</span></div>
    <div class="detail">${esc(p.problem && p.problem.message ? p.problem.message : p.status.detail || '')}</div>
    <div class="actions">${actions}</div>
    ${r ? `<div class="result ${r.bad ? 'bad' : ''}">${esc(r.text)}</div>` : ''}
  </div>`;
}

function renderSetup() {
  const box = $('setup');
  const need = providers.filter((p) => p.can_install && p.status.state && p.status.state !== 'ready');
  if (setup.running) {
    box.innerHTML = `<div class="setup">
      <div class="step"><span class="spin"></span><span>${esc(setup.step || 'Working…')}</span><span class="spacer"></span>
        <button class="btn small" data-act="stop-setup">Stop</button></div>
      ${setup.log && setup.log.length ? `<pre id="setup-log">${esc(setup.log.slice(-12).join('\n'))}</pre>` : ''}
    </div>`;
    const log = $('setup-log');
    if (log) log.scrollTop = log.scrollHeight;
    return;
  }
  if (!need.length) {
    box.innerHTML = setup.error ? `<div class="setup"><div class="step">Setup stopped</div><div class="muted">${esc(setup.error)}</div></div>` : '';
    return;
  }
  box.innerHTML = `<div class="setup">
    <div class="step"><span>Set up ${need.map((p) => esc(p.name)).join(' and ')}</span></div>
    <div class="muted" style="margin:4px 0 8px">The trainer downloads each one, checks it and opens its login. Nothing else on your PC changes.</div>
    ${setup.error ? `<div class="result bad" style="margin-bottom:8px">${esc(setup.error)}</div>` : ''}
    <button class="btn primary" data-act="setup-all">Install and connect</button>
  </div>`;
}

function renderProviders() {
  const inUse = providers.filter((p) => uses(p.id).length || p.status.state === 'ready');
  const rest = providers.filter((p) => !inUse.includes(p));
  $('providers').innerHTML = inUse.map(providerCard).join('') +
    (rest.length ? `<details class="group" ${inUse.length ? '' : 'open'}><summary>More providers (${rest.length})</summary><div class="providers">${rest.map(providerCard).join('')}</div></details>` : '');
  renderSetup();
}

$('setup').addEventListener('click', async (e) => {
  const act = e.target.dataset.act;
  if (!act) return;
  e.target.disabled = true;
  try {
    if (act === 'setup-all') setup = await api('/api/ai/setup', { method: 'POST', body: {} });
    if (act === 'stop-setup') setup = await api('/api/ai/setup', { method: 'DELETE' });
    renderSetup();
  } catch (err) { toast(err.message, true); e.target.disabled = false; }
});

events.on('ai_setup', (st) => {
  const wasRunning = setup.running;
  setup = st;
  renderSetup();
  if (wasRunning && !st.running) loadProviders(true);
});

function replace(view) {
  providers = providers.map((p) => (p.id === view.id ? view : p));
  renderProviders();
  renderJobs();
}

async function loadProviders(fresh) {
  try {
    providers = await api(`/api/ai/providers${fresh ? '?fresh=1' : ''}`);
    renderProviders();
    renderJobs();
  } catch (e) { toast(e.message, true); }
}

$('providers').addEventListener('click', async (e) => {
  const act = e.target.dataset.act, id = e.target.dataset.id;
  if (!act) return;
  const btn = e.target;
  btn.disabled = true;
  try {
    if (act === 'check') replace(await api(`/api/ai/providers/${id}/check`, { method: 'POST' }));
    if (act === 'login') toast((await api(`/api/ai/providers/${id}/login`, { method: 'POST' })).status);
    if (act === 'switch') toast((await api(`/api/ai/providers/${id}/switch`, { method: 'POST' })).status);
    if (act === 'install') { setup = await api(`/api/ai/providers/${id}/install`, { method: 'POST' }); renderSetup(); }
    if (act === 'save-key' || act === 'remove-key') {
      const url = document.querySelector(`[data-url="${id}"]`);
      if (url && act === 'save-key') await saveSettings({ ai: { custom_url: url.value.trim() } });
      const key = act === 'remove-key' ? '' : document.querySelector(`[data-key="${id}"]`).value.trim();
      if (act === 'remove-key' || key) replace(await api(`/api/ai/providers/${id}/key`, { method: 'PUT', body: { key } }));
      else replace(await api(`/api/ai/providers/${id}/check`, { method: 'POST' }));
    }
    if (act === 'test') {
      results[id] = { text: 'Asking…' };
      renderProviders();
      const c = uses(id).length ? [cfg.settings.ai.live, cfg.settings.ai.reviews, cfg.settings.ai.fallback].find((x) => x.provider === id) : { model: (byID(id).models[0] || {}).id || '' };
      try {
        const r = await api(`/api/ai/providers/${id}/test`, { method: 'POST', body: { model: c.model, effort: c.effort || '' } });
        results[id] = { text: `Answered in ${(r.took_ms / 1000).toFixed(1)} s: “${r.tips.join(' ')}”` };
      } catch (err) { results[id] = { text: err.message, bad: true }; }
      renderProviders();
    }
  } catch (err) { toast(err.message, true); }
  btn.disabled = false;
});

onSettings((c) => {
  const ai = c.settings.ai;
  if (document.activeElement !== $('ai-profile')) $('ai-profile').value = ai.profile || '';
  if (document.activeElement !== $('ai-instructions')) $('ai-instructions').value = ai.instructions || '';
  renderJobs();
  if (providers.length) renderProviders();
});
$('live-on').addEventListener('change', (e) => saveSettings({ ai: { enabled: e.target.checked } }));
$('review-on').addEventListener('change', (e) => saveSettings({ ai: { review: e.target.checked } }));
$('draft-on').addEventListener('change', (e) => saveSettings({ ai: { draft: e.target.checked } }));
$('interval').addEventListener('change', (e) => saveSettings({ ai: { interval: Number(e.target.value) } }));
$('ai-save').addEventListener('click', async () => {
  if (await saveSettings({ ai: { profile: $('ai-profile').value, instructions: $('ai-instructions').value } })) {
    $('ai-save-note').textContent = 'Saved';
    setTimeout(() => { $('ai-save-note').textContent = ''; }, 2000);
  }
});
$('refresh').addEventListener('click', () => loadProviders(true));
events.on('ai_health', () => loadProviders(false));
events.onOpen(async () => {
  try { setup = await api('/api/ai/setup'); } catch (e) { /* the panel shows once providers load */ }
  await loadProviders(false);
  loadProviders(true);
});
events.start();
