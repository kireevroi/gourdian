// Not a module and no imports: this page is opened inside the Steam overlay's browser, where
// the HUD is a Linux player's only view, so it depends on nothing but itself.
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function render(p) {
  const v = p && p.live;
  const el = document.getElementById('view');
  if (!v || (!v.alert && !(v.rows || []).length)) {
    el.innerHTML = '<div class="idle">Nothing to show yet. Tips appear here during a match.</div>';
    return;
  }
  const more = v.more ? ` <span class="muted">(+${v.more})</span>` : '';
  el.innerHTML = (v.alert ? `<div class="alert ${esc(v.alert.kind)}">${esc(v.alert.text)}${more}</div>` : '') +
    (v.rows || []).map((r) => `<div class="row ${esc(r.kind)}">${esc(r.text)}</div>`).join('');
}

async function load() {
  try { render(await (await fetch('/api/hud')).json()); } catch (e) { setTimeout(load, 2000); }
}

function connect() {
  const es = new EventSource('/events');
  es.addEventListener('hud', (e) => render(JSON.parse(e.data)));
  es.onopen = load;
  es.onerror = () => { document.getElementById('view').innerHTML = '<div class="idle">The trainer isn\'t running. Reconnecting…</div>'; };
}

// The Steam overlay browser remembers nothing between sessions, so the size lives in the URL too.
const params = new URLSearchParams(location.search);
let size = params.get('size') || '';
try { size = size || localStorage.getItem('overlay-size') || ''; } catch (e) { /* private window */ }
document.body.className = size;
document.querySelector('.size').addEventListener('click', (e) => {
  if (e.target.dataset.size === undefined) return;
  size = e.target.dataset.size;
  document.body.className = size;
  try { localStorage.setItem('overlay-size', size); } catch (err) { /* private window */ }
});

load();
connect();
