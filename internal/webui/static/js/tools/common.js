// What the tool windows share: the tool job (kept fresh by the server's
// events), the strip of files with their state, the closing summary and
// loading a preview with the picture's own size.
import { html, useState, useEffect, Button, Icon, IconButton } from '../ui.js';
import { api, onTool, store, closeWindow, openPath } from '../api.js';
import { t } from '../i18n.js';

/** toolParams are the query of the window's address: job, preset, files (JSON). */
function toolParams() {
  const h = location.hash;
  const q = new URLSearchParams(h.includes('?') ? h.slice(h.indexOf('?') + 1) : '');
  const tool = (h.match(/#\/tool\/([^?]+)/) || [])[1] || '';
  let files = [];
  try { files = JSON.parse(q.get('files') || '[]'); } catch { files = []; }
  return { tool, preset: q.get('preset') || '', files };
}

export function useTool(id) {
  const [job, setJob] = useState(store.tools[id] || null);
  const [error, setError] = useState('');
  useEffect(() => {
    const got = (j) => { store.tools[id] = j; setJob(j); };
    const p = toolParams();
    // A window opened with its files in the address opens the tool under the
    // address's token (the same window again after a reload).
    const ready = p.files.length ? api.post('api/tools', { id, tool: p.tool, presetId: p.preset, paths: p.files }) : Promise.resolve();
    ready.then(() => api.get('api/tools/' + id)).then(got).catch((e) => setError(e.message));
    return onTool(id, setJob);
  }, [id]);
  return [job, setJob, error];
}

export function currentIndex(job) {
  const i = job.files.findIndex((f) => f.state === 'current');
  return i;
}

export function ToolHead({ title, job, index, children, onClose }) {
  const close = async () => {
    if (onClose) await onClose();
    await api.del('api/tools/' + job.id).catch(() => {});
    closeWindow();
  };
  useEffect(() => {
    const on = (e) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop')) close(); };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  }, [job?.id]);
  return html`<header class="tool-head">
    <h1 title=${index >= 0 ? job.files[index]?.path : ''}>${title}</h1>
    ${job.files.length > 1 && index >= 0 && html`<span class="counter">${t('crop.page_counter', index + 1, job.files.length)}</span>`}
    <span class="spacer"></span>
    ${children}
    <${IconButton} icon="Close" title=${t('crop.close_hint')} onClick=${close} />
  </header>`;
}

const stateKey = { running: 'clip.preparing', current: 'crop.state_current', waiting: 'crop.state_waiting', done: 'crop.state_done', skipped: 'crop.state_skipped', failed: 'crop.state_failed' };

export function Queue({ job, doneKey = 'crop.state_done' }) {
  if (job.files.length < 2) return null;
  return html`<div class="queue">${job.files.map((f, i) => html`<div class=${'queue-item ' + f.state}
      title=${`${f.name}: ${t(f.state === 'done' ? doneKey : stateKey[f.state])}${f.message ? ' · ' + f.message : ''}`}>
    <span class="num">${f.state === 'done' ? '✓' : f.state === 'failed' ? '!' : i + 1}</span><span class="name">${f.name}</span></div>`)}</div>`;
}

/** Summary is shown when every file was handled. */
export function Summary({ job, doneKey = 'crop.state_done' }) {
  const done = job.files.filter((f) => f.state === 'done');
  const failed = job.files.filter((f) => f.state === 'failed');
  const outs = done.flatMap((f) => f.outputs || []);
  return html`<div class="summary">
    <${Icon} name=${failed.length ? 'Alert' : 'Check'} size=${42} cls=${failed.length ? 'danger' : 'success'} />
    <h2>${failed.length ? t('toast.done_errors', failed.length, job.files.length) : t('toast.done')}</h2>
    ${failed.map((f) => html`<p class="caption danger selectable">${f.name}: ${f.message}</p>`)}
    <div class="row">
      ${outs.length > 0 && html`<${Button} icon="Folder" onClick=${() => openPath(outs[0], true)}>${t('toast.open_folder')}</${Button}>`}
      <${Button} kind="accent" onClick=${async () => { await api.del('api/tools/' + job.id).catch(() => {}); closeWindow(); }}>${t('common.close')}</${Button}>
    </div>
  </div>`;
}

export const finished = (job) => job.files.every((f) => !['current', 'waiting', 'running'].includes(f.state));

/** usePreview loads the preview of file i (page p): its URL and the picture's own size. */
export function usePreview(job, i, page = 0, max = 2560) {
  const [p, setP] = useState(null);
  useEffect(() => {
    if (!job || i < 0) return undefined;
    let url = '';
    let live = true;
    setP(null);
    fetch(`api/tools/${job.id}/files/${i}/preview?page=${page}&max=${max}`).then(async (r) => {
      if (!r.ok) { const e = await r.json().catch(() => ({})); if (live) setP({ error: e.error || r.statusText }); return; }
      const blob = await r.blob();
      url = URL.createObjectURL(blob);
      if (live) setP({ url, width: +r.headers.get('X-Width'), height: +r.headers.get('X-Height'), pages: +r.headers.get('X-Pages') });
    }).catch((e) => live && setP({ error: e.message }));
    return () => { live = false; if (url) URL.revokeObjectURL(url); };
  }, [job?.id, i, page, max]);
  return p;
}

/** useFit is the largest size of a w × h picture inside the element's box. */
export function useFit(ref, w, h, pad = 32) {
  const [box, setBox] = useState({ width: 0, height: 0 });
  useEffect(() => {
    if (!ref.current) return undefined;
    const el = ref.current;
    const ro = new ResizeObserver(() => {
      const r = el.getBoundingClientRect();
      setBox({ width: r.width, height: r.height });
    });
    ro.observe(ref.current);
    return () => ro.disconnect();
  }, [ref.current]);
  if (!w || !h || !box.width) return { width: 0, height: 0 };
  const s = Math.min((box.width - pad) / w, (box.height - pad) / h);
  return { width: Math.max(1, Math.floor(w * s)), height: Math.max(1, Math.floor(h * s)) };
}

export function ToolError({ error }) {
  return html`<div class="tool-missing"><div class="stack" style="align-items:center"><${Icon} name="Alert" size=${36} cls="danger" />
    <p>${error}</p><${Button} onClick=${closeWindow}>${t('common.close')}</${Button}></div></div>`;
}
