// The helpers every page needs, including pages that don't load the dashboard shell.

export const $ = (id) => document.getElementById(id);
export const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
export const clockStr = (sec) => { const s = Math.abs(sec); return `${sec < 0 ? '-' : ''}${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`; };
export const imgURL = (p) => `/img${String(p).split('?')[0]}`;
// api calls the trainer and returns parsed JSON, throwing the server's message on failure.
export async function api(path, { method = 'GET', body } = {}) {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text.trim() || res.statusText);
  return text ? JSON.parse(text) : null;
}
