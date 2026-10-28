// Recent conversions (the home page's list, and the whole history on its
// own page): open, show in folder, undo, retry the failed files.
import { html, useState, useEffect, Card, Button, Icon, IconButton, Empty, guard, notify } from '../ui.js';
import { store, refresh, api, openPath } from '../api.js';
import { t, baseName, extOf, fileSize } from '../i18n.js';

const imageExt = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'avif', 'heic', 'tif', 'tiff', 'ico', 'svg'];

function when(d) {
  const date = new Date(d);
  const now = new Date();
  const same = date.toDateString() === now.toDateString();
  return same ? date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : date.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
}

function Thumb({ entry }) {
  const out = entry.outputs.find((o) => entry.existing[o]);
  const [failed, setFailed] = useState(false);
  const ext = extOf(out || entry.sources[0] || '');
  const ok = entry.failures.length === 0;
  return html`<div class="thumb" style="position:relative">
    ${out && imageExt.includes(ext) && !failed
      ? html`<img src=${'api/history/thumb?path=' + encodeURIComponent(out)} alt="" style="width:40px;height:40px;object-fit:cover;border-radius:9px" onError=${() => setFailed(true)} />`
      : html`<span>${ext || '…'}</span>`}
    <span style=${`position:absolute;right:-3px;bottom:-3px;width:15px;height:15px;border-radius:50%;display:grid;place-items:center;background:var(--surface);color:var(${ok ? '--success' : '--danger'})`}>
      <${Icon} name=${ok ? 'Check' : 'Alert'} size=${13} /></span>
  </div>`;
}

function Entry({ e }) {
  const [showFailures, setShowFailures] = useState(false);
  const [undoError, setUndoError] = useState('');
  const outs = e.outputs.filter((o) => e.existing[o]);
  const title = e.outputs.length === 1 ? baseName(e.outputs[0]) : e.sources.length === 1 ? baseName(e.sources[0]) : t('home.files', e.sources.length);
  const size = outs.reduce((n, o) => n + (e.sizes[o] || 0), 0);
  const undo = async () => {
    const r = await guard(() => api.post(`api/history/${e.jobId}/undo`));
    if (r && !r.ok) setUndoError(r.reason);
  };
  const retry = async () => { if (await guard(() => api.post(`api/history/${e.jobId}/retry`))) notify(t('toast.converting', e.failures.length, e.presetName)); };
  return html`<div class="history-item">
    <${Thumb} entry=${e} />
    <div class="grow">
      <div class="title" title=${e.outputs.join('\n')}>${title}</div>
      <div class="caption">${e.presetName}${size ? ' · ' + fileSize(size) : ''} · ${when(e.finishedAt)}${e.failures.length ? html` · <span class="danger">${t('home.failed_files', e.failures.length)}</span>` : ''}</div>
      ${e.undone && html`<div class="caption">${t('home.undone')}</div>`}
      ${undoError && html`<div class="caption danger">${undoError}</div>`}
      ${e.failures.length > 0 && html`<div class="row tight" style="margin-top:4px">
        <${Button} kind="small" onClick=${retry}>${t('common.retry')}</${Button}>
        <${Button} kind="ghost small" onClick=${() => setShowFailures(!showFailures)}>${showFailures ? '▾' : '▸'} ${t('home.failed_files', e.failures.length)}</${Button}></div>`}
      ${showFailures && html`<ul class="list" style="margin-top:4px">${e.failures.map((f) => html`<li><div class="grow selectable">
        <div class="title">${baseName(f.source)}</div><div class="caption danger">${f.message}</div></div></li>`)}</ul>`}
    </div>
    <div class="row tight" style="flex:none">
      ${outs.length === 1 && html`<${Button} kind="ghost" icon="File" onClick=${() => openPath(outs[0])}>${t('home.open_file')}</${Button}>`}
      ${outs.length > 0 && html`<${Button} kind="ghost" icon="Folder" onClick=${() => openPath(outs[0], true)}>${t('home.open_folder')}</${Button}>`}
      ${e.canUndo && !e.undone && html`<${Button} kind="ghost" icon="Undo" title=${t('home.undo_hint')} onClick=${undo}>${t('home.undo')}</${Button}>`}
    </div>
  </div>`;
}

export function HistoryList({ limit, title = t('home.recent') }) {
  const [q, setQ] = useState('');
  useEffect(() => { refresh('history'); }, []);
  const all = store.history || [];
  const query = q.trim().toLowerCase();
  const shown = all.filter((e) => !query || e.presetName.toLowerCase().includes(query)
    || [...e.sources, ...e.outputs].some((p) => baseName(p).toLowerCase().includes(query))).slice(0, limit || undefined);
  return html`<${Card} title=${title} actions=${html`
      <input class="input" style="width:200px" placeholder=${t('common.search')} value=${q} onInput=${(e) => setQ(e.currentTarget.value)} />
      <${Button} onClick=${() => guard(() => api.del('api/history'))} disabled=${!all.length}>${t('home.clear_history')}</${Button}>`}>
    ${!all.length ? html`<${Empty} icon="Undo">${t('home.recent_empty')}</${Empty}>`
      : !shown.length ? html`<${Empty}>${t('home.no_matches')}</${Empty}>`
      : html`<div>${shown.map((e) => html`<${Entry} key=${e.jobId} e=${e} />`)}</div>`}
  </${Card}>`;
}

export function HistoryPage() {
  return html`<div class="stack"><h1>${t('home.recent')}</h1><${HistoryList} /></div>`;
}
