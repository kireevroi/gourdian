// Shared by every dashboard page: navigation, the event stream, settings and small helpers.

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const clockStr = (sec) => { const s = Math.abs(sec); return `${sec < 0 ? '-' : ''}${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`; };
const imgURL = (p) => `/img${String(p).split('?')[0]}`;
const ROLE_NAMES = { carry: 'Carry (1)', mid: 'Mid (2)', offlane: 'Offlane (3)', soft_support: 'Soft support (4)', hard_support: 'Hard support (5)' };
const CATEGORY_NAMES = { timing: 'Timings', survival: 'Survival', economy: 'Economy', items: 'Items', skills: 'Skills', map: 'Map movement', ai: 'AI coach', system: 'Trainer', focus: 'Your focus' };

const PAGES = [
  ['/', 'Live'], ['/stats.html', 'Stats'], ['/rules.html', 'Rules'], ['/hud.html', 'HUD'], ['/ai.html', 'AI coach'], ['/settings.html', 'Settings'],
];

// api calls the trainer and returns parsed JSON, throwing the server's message on failure.
async function api(path, { method = 'GET', body } = {}) {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text.trim() || res.statusText);
  return text ? JSON.parse(text) : null;
}

function toast(text, bad = false) {
  let box = document.querySelector('.toasts');
  if (!box) { box = document.createElement('div'); box.className = 'toasts'; document.body.appendChild(box); }
  const el = document.createElement('div');
  el.className = `toast ${bad ? 'bad' : ''}`;
  el.textContent = text;
  box.appendChild(el);
  setTimeout(() => el.remove(), bad ? 7000 : 3500);
}

// Language: the pages are written in English and a dictionary swaps the text at runtime, so
// anything the dictionary doesn't know (hero names, your own rules) stays as it is.
const i18n = (() => {
  let code = 'en', strings = {}, patterns = [];
  const attrs = ['placeholder', 'title', 'aria-label'];

  function swap(text) {
    const s = text.trim();
    if (!s) return text;
    const hit = strings[s];
    if (hit) return text.replace(s, hit);
    for (const [re, to] of patterns) if (re.test(s)) return text.replace(s, s.replace(re, to));
    return text;
  }

  function apply(node) {
    if (node.nodeType === Node.TEXT_NODE) {
      if (node.__en === undefined) node.__en = node.nodeValue;
      const next = code === 'en' ? node.__en : swap(node.__en);
      if (next !== node.nodeValue) node.nodeValue = next;
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    if (node.tagName === 'SCRIPT' || node.tagName === 'STYLE') return;
    for (const a of attrs) {
      if (!node.hasAttribute(a)) continue;
      const key = 'en' + a.replace(/(^|-)([a-z])/g, (_, __, c) => c.toUpperCase());
      if (node.dataset[key] === undefined) node.dataset[key] = node.getAttribute(a);
      node.setAttribute(a, code === 'en' ? node.dataset[key] : swap(node.dataset[key]));
    }
    for (const child of node.childNodes) apply(child);
  }

  const observer = new MutationObserver((records) => {
    for (const r of records) for (const n of r.addedNodes) apply(n);
  });

  async function use(next) {
    if (next === code) return;
    if (next !== 'en' && !Object.keys(strings).length) {
      try {
        const file = await (await fetch(`/i18n/${next}.json`)).json();
        strings = file.strings || {};
        patterns = (file.patterns || []).map(([re, to]) => [new RegExp(re), to]);
      } catch (e) { return; }
    }
    code = next;
    document.documentElement.lang = next;
    if (document.title) document.title = code === 'en' ? (document.__enTitle || document.title) : swap(document.__enTitle ||= document.title);
    apply(document.body);
    observer.observe(document.body, { childList: true, subtree: true });
  }

  return { use, t: (text) => (code === 'en' ? text : swap(text)) };
})();
const t = (text) => i18n.t(text);

// sayInBrowser speaks in the trainer's language, preferring the browser's natural voices;
// Edge has Russian ones built in.
function sayInBrowser(text, lang, rate, urgent) {
  if (!('speechSynthesis' in window)) return;
  if (urgent) speechSynthesis.cancel();
  const code = lang === 'ru' ? 'ru' : 'en';
  const u = new SpeechSynthesisUtterance(text);
  u.lang = code === 'ru' ? 'ru-RU' : 'en-US';
  const voices = speechSynthesis.getVoices().filter((v) => v.lang.toLowerCase().startsWith(code));
  u.voice = voices.find((v) => /natural|online/i.test(v.name)) || voices[0] || null;
  u.rate = Math.min(2, Math.max(0.5, 1 + rate * 0.1));
  speechSynthesis.speak(u);
}
// tp translates a sentence that has values in it: tp('{n} of {m} done', { n, m }).
const tp = (template, vars) => t(template).replace(/\{(\w+)\}/g, (_, k) => vars[k]);

// Events: one EventSource per page, shared by the shell and the page.
const events = (() => {
  const handlers = {}, openers = [];
  let es;
  const on = (type, fn) => {
    (handlers[type] ||= []).push(fn);
    if (es) es.addEventListener(type, (e) => fn(JSON.parse(e.data), e.data));
  };
  const start = () => {
    es = new EventSource('/events');
    for (const [type, fns] of Object.entries(handlers)) {
      for (const fn of fns) es.addEventListener(type, (e) => fn(JSON.parse(e.data), e.data));
    }
    // In order, so the shell has the language loaded before a page renders text with t().
    es.onopen = async () => { for (const fn of openers) { try { await fn(); } catch (e) { /* the next one still runs */ } } };
    es.onerror = () => (handlers.offline || []).forEach((fn) => fn());
  };
  return { on, onOpen: (fn) => openers.push(fn), start };
})();

// Settings: cfg is the latest GET /api/settings response; pages subscribe with onSettings.
let cfg = null;
const settingsListeners = [];
let lastSettingsJSON = '';
function onSettings(fn) { settingsListeners.push(fn); if (cfg) fn(cfg); }
function setSettings(next) { cfg = next; settingsListeners.forEach((fn) => fn(cfg)); }
// The language is loaded before anything renders, so the first paint is already translated.
async function loadSettings() {
  const c = await api('/api/settings');
  await i18n.use(c.settings.language || 'en');
  setSettings(c);
}
// saveSettings sends only the changed keys; the server merges them into the current settings.
async function saveSettings(patch) {
  try {
    setSettings(await api('/api/settings', { method: 'PUT', body: patch }));
    return true;
  } catch (e) {
    toast(e.message, true);
    if (cfg) setSettings(cfg);
    return false;
  }
}
async function setRole(role) {
  try { setSettings(await api('/api/role', { method: 'POST', body: { role } })); } catch (e) { toast(e.message, true); }
}

function positionButtons(el) {
  el.addEventListener('click', (e) => { if (e.target.dataset.role) setRole(e.target.dataset.role); });
  onSettings((c) => {
    el.innerHTML = c.roles.map((r, i) => `<button data-role="${r}" class="${r === c.settings.role ? 'on' : ''}" title="${ROLE_NAMES[r] || r}">${i + 1}</button>`).join('');
  });
}

function renderShell() {
  const here = location.pathname === '/index.html' ? '/' : location.pathname;
  const header = document.createElement('header');
  header.className = 'shell';
  header.innerHTML = `
    <a class="brand" href="/" id="brand">Dota <span>Trainer</span></a>
    <nav class="pages">${PAGES.map(([href, name]) => `<a href="${href}" class="${href === here ? 'on' : ''}">${name}</a>`).join('')}</nav>
    <div class="spacer"></div>
    <span class="status down" id="shell-status">Connecting…</span>`;
  const banner = document.createElement('div');
  banner.className = 'banner-wrap';
  banner.innerHTML = `<div class="ai-banner" id="ai-banner" hidden>
      <span id="ai-banner-text"></span>
      <button class="btn primary" id="ai-login" hidden>Log in</button>
      <button class="btn" id="ai-check">Check again</button>
      <a class="btn" href="/ai.html">AI coach page</a>
    </div>`;
  document.body.prepend(header, banner);

  const status = $('shell-status');
  events.on('snapshot', (snap) => {
    if (!snap.connected) { status.className = 'status idle'; status.textContent = 'Waiting for Dota 2'; }
    else if (!snap.in_match) { status.className = 'status idle'; status.textContent = 'Dota 2 connected'; }
    else { status.className = 'status live'; status.textContent = snap.paused ? 'Match paused' : 'In a match'; }
  });
  events.on('offline', () => { status.className = 'status down'; status.textContent = 'Trainer offline'; showReconnecting(); });
  events.onOpen(() => hideReconnecting());
  events.on('settings', (c, raw) => {
    if (raw === lastSettingsJSON) return;
    lastSettingsJSON = raw;
    // Text already on the page was built in the old language, so start over in the new one.
    if (cfg && (cfg.settings.language || 'en') !== (c.settings.language || 'en')) { location.reload(); return; }
    setSettings(c);
  });
  events.on('ai_health', renderAIHealth);
  events.onOpen(() => {
    api('/api/ai/status').then(renderAIHealth).catch(() => {});
    return loadSettings().catch(() => {});
  });
  onSettings((c) => { $('brand').title = `Dota Trainer ${c.version}`; });

  $('ai-login').addEventListener('click', async () => {
    try { $('ai-banner-text').textContent = (await api('/api/ai/login', { method: 'POST' })).status; } catch (e) { $('ai-banner-text').textContent = e.message; }
  });
  $('ai-check').addEventListener('click', async () => {
    $('ai-check').disabled = true;
    try { renderAIHealth(await api('/api/ai/check', { method: 'POST' })); } catch (e) { $('ai-banner-text').textContent = e.message; }
    $('ai-check').disabled = false;
  });
}

function renderAIHealth(h) {
  $('ai-banner').hidden = !h.problem;
  $('ai-banner-text').textContent = h.message || '';
  $('ai-login').hidden = !h.can_login;
}

// While the trainer is away (a restart or an update) the window shows its own overlay and
// keeps trying, instead of the browser's error page. A new version reloads the page.
let reconnectTimer = null, loadedBuild = null;

function showReconnecting() {
  if (reconnectTimer) return;
  let el = $('reconnecting');
  if (!el) {
    el = document.createElement('div');
    el.id = 'reconnecting';
    el.className = 'reconnecting';
    el.innerHTML = '<div class="box"><div class="spin"></div><div><strong>Reconnecting…</strong><div class="muted">The trainer is restarting. This window comes back by itself.</div></div></div>';
    document.body.appendChild(el);
  }
  el.hidden = false;
  reconnectTimer = setInterval(async () => {
    try {
      const c = await api('/api/settings');
      if (loadedBuild && c.build !== loadedBuild) { location.reload(); return; }
      hideReconnecting();
      setSettings(c);
    } catch (e) { /* still down */ }
  }, 1500);
}

function hideReconnecting() {
  clearInterval(reconnectTimer);
  reconnectTimer = null;
  const el = $('reconnecting');
  if (el) el.hidden = true;
}

onSettings((c) => {
  // A dashboard file changed under an open window (an update), so take the new one.
  if (loadedBuild && c.build !== loadedBuild) { location.reload(); return; }
  loadedBuild ||= c.build;
});
if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});

renderShell();
