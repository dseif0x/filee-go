// Edit PDF pages (PdfPageEditorViewModel): every page of the PDFs as a
// thumbnail; reorder by dragging (or Alt+arrows), turn, delete, add pages of
// another PDF, extract the selected pages as a file of their own, save.
import { html, useState, useEffect, Button, IconButton, Icon, Spinner, pickFiles, guard, notify } from '../ui.js';
import { api, openPath } from '../api.js';
import { t } from '../i18n.js';
import { useTool, ToolHead, ToolError } from './common.js';

function Thumb({ job, page }) {
  const [src, setSrc] = useState(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let url = '';
    let live = true;
    fetch(`api/tools/${job.id}/files/${page.file}/preview?page=${page.page}&max=260`).then(async (r) => {
      if (!r.ok) { if (live) setFailed(true); return; }
      url = URL.createObjectURL(await r.blob());
      if (live) setSrc(url);
    }).catch(() => live && setFailed(true));
    return () => { live = false; if (url) URL.revokeObjectURL(url); };
  }, [job.id, page.file, page.page]);
  const signed = page.rotation > 180 ? page.rotation - 360 : page.rotation;
  return html`<div class="thumb">${src ? html`<img src=${src} alt="" style=${`transform:rotate(${signed}deg)`} />`
    : failed ? html`<span class="ph" style=${`transform:rotate(${signed}deg)`}>${page.page + 1}</span>` : html`<${Spinner} />`}</div>`;
}

export function PdfPagesTool({ job: id }) {
  const [job, setJob, error] = useTool(id);
  const [pages, setPages] = useState(null);
  const [sel, setSel] = useState([]);
  const [drag, setDrag] = useState(null);
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(0);
  useEffect(() => {
    if (!job || loaded >= job.files.length) return;
    (async () => {
      setStatus(t('pdf_editor.loading'));
      const add = [];
      for (let f = loaded; f < job.files.length; f++) {
        const info = await api.get(`api/tools/${id}/files/${f}/info`).catch(() => ({}));
        for (let p = 0; p < (info.pages || 0); p++) add.push({ key: f + ':' + p + ':' + Math.random(), file: f, page: p, rotation: 0 });
        if (!info.pages) notify(job.files[f].name + ': ' + t('error.conversion_failed'), 'danger');
      }
      setPages((old) => [...(old || []), ...add]);
      setLoaded(job.files.length);
      setStatus('');
    })();
  }, [job?.files.length]);
  if (error) return html`<${ToolError} error=${error} />`;
  if (!job) return html`<div class="tool-missing"><${Spinner} size=${28} /></div>`;
  const list = pages || [];
  const turn = (deg) => setPages(list.map((p, k) => (sel.includes(k) ? { ...p, rotation: (((p.rotation + deg) % 360) + 360) % 360 } : p)));
  const del = () => { setPages(list.filter((_, k) => !sel.includes(k))); setSel([]); };
  const move = (dir) => {
    if (sel.length !== 1) return;
    const k = sel[0];
    const j = k + dir;
    if (j < 0 || j >= list.length) return;
    const n = [...list];
    [n[k], n[j]] = [n[j], n[k]];
    setPages(n); setSel([j]);
  };
  const dropAt = (to) => {
    if (drag === null || drag === to) return;
    const n = [...list];
    const [x] = n.splice(drag, 1);
    n.splice(to > drag ? to - 1 : to, 0, x);
    setPages(n); setSel([n.indexOf(x)]);
  };
  const click = (e, k) => {
    if (e.shiftKey && sel.length) {
      const a = Math.min(sel[sel.length - 1], k), b = Math.max(sel[sel.length - 1], k);
      setSel([...new Set([...sel, ...Array.from({ length: b - a + 1 }, (_, n) => a + n)])]);
    } else if (e.ctrlKey || e.metaKey) setSel(sel.includes(k) ? sel.filter((x) => x !== k) : [...sel, k]);
    else setSel([k]);
  };
  const write = async (which) => {
    if (busy || !which.length) return;
    setBusy(true);
    setStatus(t('pdf_editor.saving'));
    const r = await guard(() => api.post(`api/tools/${id}/pdf/save`, { pages: which.map(({ file, page, rotation }) => ({ file, page, rotation })) }));
    setBusy(false);
    setStatus(r ? r.message : '');
    if (r?.path) notify(r.message);
    return r;
  };
  const addPdf = async () => {
    const paths = await pickFiles({ multiple: true, title: t('pdf_editor.add'), filter: (n) => n.toLowerCase().endsWith('.pdf') });
    if (!paths?.length) return;
    const j = await guard(() => api.post(`api/tools/${id}/pdf/add`, { paths }));
    if (j) setJob(j);
  };
  const lastSaved = status && status.startsWith(t('pdf_editor.saved', '').trim()) ? status : '';
  return html`<div class="tool">
    <${ToolHead} title=${t('pdf_editor.title')} job=${job} index=${-1}>
      <span class="caption">${job.files.map((f) => f.name).join(', ')}</span>
    </${ToolHead}>
    <div class="tool-body" style="flex-direction:column"
        onKeyDown=${(e) => {
          if (e.target.closest?.('input')) return;
          if (e.key === 'Delete' || e.key === 'Backspace') del();
          else if (e.altKey && e.key === 'ArrowLeft') move(-1);
          else if (e.altKey && e.key === 'ArrowRight') move(1);
          else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'a') { e.preventDefault(); setSel(list.map((_, k) => k)); }
        }} tabindex="0">
      <div class="row" style="padding:10px 16px;background:var(--surface);border-bottom:1px solid var(--border)">
        <${Button} icon="Plus" onClick=${addPdf}>${t('pdf_editor.add')}</${Button}>
        <${IconButton} icon="ArrowUp" title=${t('group.move_up')} disabled=${sel.length !== 1 || sel[0] === 0} onClick=${() => move(-1)} />
        <${IconButton} icon="ArrowDown" title=${t('group.move_down')} disabled=${sel.length !== 1 || sel[0] === list.length - 1} onClick=${() => move(1)} />
        <${Button} disabled=${!sel.length} onClick=${() => turn(-90)} >⟲ ${t('preset.rotate_left.short')}</${Button}>
        <${Button} disabled=${!sel.length} onClick=${() => turn(90)} >⟳ ${t('preset.rotate_right.short')}</${Button}>
        <${Button} kind="danger" icon="Delete" disabled=${!sel.length} onClick=${del}>${t('pdf_editor.delete')}</${Button}>
        <span class="spacer"></span>
        <span class="caption">${t('pdf_editor.hint')}</span>
      </div>
      ${pages === null ? html`<div class="tool-stage"><${Spinner} size=${28} /></div>` : html`<div class="pages-grid">
        ${list.map((p, k) => html`<div key=${p.key} class=${'page-card' + (sel.includes(k) ? ' on' : '') + (drag === k ? ' dragging' : '')} draggable="true"
            onClick=${(e) => click(e, k)} onDragStart=${() => setDrag(k)} onDragOver=${(e) => e.preventDefault()}
            onDrop=${(e) => { e.preventDefault(); dropAt(k); setDrag(null); }} onDragEnd=${() => setDrag(null)}>
          <${Thumb} job=${job} page=${p} />
          <div class="meta">${t('pdf_editor.page', p.page + 1)}${p.rotation ? ' · ' + t('pdf_editor.rotation', p.rotation > 180 ? p.rotation - 360 : p.rotation) : ''}</div>
          ${job.files.length > 1 && html`<div class="meta">${job.files[p.file].name}</div>`}
        </div>`)}
      </div>`}
    </div>
    <footer class="tool-foot">
      <span class="muted">${status}</span>
      <span class="spacer"></span>
      <${Button} disabled=${busy || !sel.length} onClick=${() => write(sel.map((k) => list[k]))}>${t('preset.pdf_extract.short')}</${Button}>
      <${Button} kind="accent" disabled=${busy || !list.length} onClick=${() => write(list)}>${t('common.save')}</${Button}>
    </footer>
  </div>`;
}
