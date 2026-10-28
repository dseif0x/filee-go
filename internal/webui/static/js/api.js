// The JSON API (relative URLs: the page lives under the run's secret path)
// and a small store the pages share, kept fresh by the server's events.

export class ApiError extends Error {}

async function request(method, path, body, raw) {
  const opt = { method, headers: {} };
  if (body instanceof Blob || body instanceof FormData) {
    opt.body = body;
  } else if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  const res = await fetch(path, opt);
  if (raw) {
    if (!res.ok) throw new ApiError(await errorText(res));
    return res;
  }
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) throw new ApiError((data && data.error) || res.statusText);
  return data;
}

async function errorText(res) {
  try { const d = await res.json(); return d.error || res.statusText; } catch { return res.statusText; }
}

export const api = {
  get: (p) => request('GET', p),
  post: (p, b = {}) => request('POST', p, b),
  put: (p, b) => request('PUT', p, b),
  del: (p) => request('DELETE', p),
  raw: (m, p, b) => request(m, p, b, true),
};

// ---- store ----

const listeners = new Set();
export const store = {
  boot: null, settings: null, library: null, engines: null, history: null, jobs: [], watch: null,
  tools: {}, connected: false,
};

export function emit() { for (const l of listeners) l(store); }
export function subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); }

export async function loadAll() {
  const [boot, settings, library] = await Promise.all([api.get('api/bootstrap'), api.get('api/settings'), api.get('api/library')]);
  Object.assign(store, { boot, settings, library });
  emit();
}

export async function refresh(what) {
  try {
    switch (what) {
      case 'settings': store.settings = await api.get('api/settings'); break;
      case 'library': store.library = await api.get('api/library'); break;
      case 'engines': store.engines = await api.get('api/engines'); break;
      case 'history': store.history = await api.get('api/history'); break;
      case 'jobs': store.jobs = await api.get('api/jobs'); break;
      case 'watch': store.watch = await api.get('api/watch'); break;
      case 'texts': store.boot = { ...store.boot, texts: await api.get('api/texts') }; break;
    }
    emit();
  } catch (e) { console.warn('refresh', what, e); }
}

/** saveSettings changes a copy of the settings and saves it. */
export async function saveSettings(change) {
  const next = structuredClone(store.settings);
  change(next);
  store.settings = next; // shown at once
  emit();
  try {
    store.settings = await api.put('api/settings', next);
  } catch (e) {
    await refresh('settings');
    throw e;
  }
  emit();
  return store.settings;
}

// ---- events ----

const jobWaiters = new Map();

/** onJob calls fn with every update of the job until it is final. */
export function onJob(id, fn) {
  if (!jobWaiters.has(id)) jobWaiters.set(id, new Set());
  jobWaiters.get(id).add(fn);
  return () => jobWaiters.get(id)?.delete(fn);
}

const toolWaiters = new Map();
export function onTool(id, fn) {
  if (!toolWaiters.has(id)) toolWaiters.set(id, new Set());
  toolWaiters.get(id).add(fn);
  return () => toolWaiters.get(id)?.delete(fn);
}

let langBefore = null;

export function connectEvents() {
  const es = new EventSource('api/events');
  es.onopen = () => { store.connected = true; emit(); };
  es.onerror = () => { store.connected = false; emit(); };
  const job = (e) => {
    const ev = JSON.parse(e.data);
    const snap = ev.job;
    if (!snap) return;
    const i = store.jobs.findIndex((j) => j.id === snap.id);
    if (i >= 0) store.jobs[i] = snap; else store.jobs.push(snap);
    for (const fn of jobWaiters.get(snap.id) || []) fn(snap);
    emit();
  };
  es.addEventListener('job.updated', job);
  es.addEventListener('job.finished', job);
  es.addEventListener('settings.changed', async () => {
    await refresh('settings');
    if (langBefore !== null && store.settings.language !== langBefore) await refresh('texts');
    langBefore = store.settings.language;
  });
  es.addEventListener('library.changed', () => refresh('library'));
  es.addEventListener('history.changed', () => refresh('history'));
  es.addEventListener('watch.changed', () => refresh('watch'));
  es.addEventListener('engines.changed', () => refresh('engines'));
  es.addEventListener('tool.updated', (e) => {
    const t = JSON.parse(e.data);
    store.tools[t.id] = t;
    for (const fn of toolWaiters.get(t.id) || []) fn(t);
    emit();
  });
  langBefore = store.settings?.language ?? null;
  return es;
}

/** upload sends dropped files to the server, which saves them and returns their paths. */
export async function upload(files) {
  const fd = new FormData();
  for (const f of files) fd.append('file', f, f.name);
  const r = await api.post('api/upload', fd);
  return r.paths || [];
}

/** open opens (or reveals) a file with the system. */
export function openPath(path, reveal = false) {
  return api.post('api/open', { path, reveal }).catch((e) => console.warn(e));
}

/** closeWindow closes the tool window: the macOS app's web view, or the browser tab. */
export function closeWindow() {
  try { window.webkit?.messageHandlers?.filee?.postMessage({ type: 'close' }); } catch {}
  if (window.opener || history.length <= 1) window.close();
  else location.hash = '#/home';
}
