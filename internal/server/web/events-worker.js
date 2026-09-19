// One event stream from the trainer for every dashboard page open in this browser. A browser
// allows six connections to a server and every stream keeps one for good, so with six pages
// open a new page got its stream but its requests queued forever: it came up in English, with
// empty cards.
const pages = new Map(); // port -> the event types that page listens to
const listening = new Set();
let es = null, open = false;
// What the trainer sends when a stream opens, kept for pages that join an open stream.
let snapshot = null, hud = null, tips = [];

function send(type, data) {
  for (const [port, types] of pages) if (types.has(type)) port.postMessage({ type, data });
}

function listen(type) {
  if (!es || listening.has(type)) return;
  listening.add(type);
  es.addEventListener(type, (e) => {
    if (type === 'snapshot') snapshot = e.data;
    if (type === 'hud') hud = e.data;
    if (type === 'tips') tips = JSON.parse(e.data) || [];
    if (type === 'tip') {
      tips.push(JSON.parse(e.data));
      if (tips.length > 50) tips.shift();
    }
    send(type, e.data);
  });
}

function connect() {
  es = new EventSource('/events');
  listening.clear();
  for (const types of pages.values()) for (const type of types) listen(type);
  // The trainer's first events after a (re)connect are its state, so they're tracked too.
  for (const type of ['snapshot', 'tips', 'tip', 'hud']) listen(type);
  es.onopen = () => {
    open = true;
    for (const port of pages.keys()) port.postMessage({ open: true });
  };
  es.onerror = () => {
    open = false;
    for (const port of pages.keys()) port.postMessage({ offline: true });
  };
}

// replay tells a page that joined an open stream what a fresh stream would have: that it's
// open, then the trainer's state.
function replay(port, types) {
  port.postMessage({ open: true });
  const state = [['snapshot', snapshot], ['tips', JSON.stringify(tips)], ['hud', hud]];
  for (const [type, data] of state) if (data !== null && types.has(type)) port.postMessage({ type, data });
}

setInterval(() => { for (const port of pages.keys()) port.postMessage({ alive: true }); }, 5000);

onconnect = (e) => {
  const port = e.ports[0];
  const types = new Set();
  pages.set(port, types);
  port.onmessage = (m) => {
    if (m.data.bye) {
      pages.delete(port);
      if (!pages.size && es) {
        es.close();
        es = null;
        open = false;
      }
      return;
    }
    for (const type of m.data.subscribe || []) {
      types.add(type);
      listen(type);
    }
    if (m.data.hello) {
      if (!es) connect();
      else if (open) replay(port, types);
    }
  };
};
