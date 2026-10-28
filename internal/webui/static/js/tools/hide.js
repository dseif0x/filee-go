// Pixelate / blur (HideWindowViewModel, HideCanvas, HideAreaSet): drag
// frames over what to hide, pixelate or blur at a strength, Hide & Save /
// Next, Skip. Tab picks a frame, arrows move it, Del removes it, Ctrl+Z undoes.
import { html, useState, useEffect, useRef, Button, Segmented, Slider, Spinner, guard } from '../ui.js';
import { api } from '../api.js';
import { t } from '../i18n.js';
import { useTool, currentIndex, ToolHead, Queue, Summary, finished, usePreview, useFit, ToolError } from './common.js';

const clamp01 = (v) => Math.max(0, Math.min(1, v));

function useHistory(initial) {
  const [s, setS] = useState({ list: initial, undo: [], redo: [] });
  const set = (list, join) => setS((o) => ({ list, undo: join ? o.undo : [...o.undo, o.list].slice(-100), redo: [] }));
  const undo = () => setS((o) => (o.undo.length ? { list: o.undo[o.undo.length - 1], undo: o.undo.slice(0, -1), redo: [o.list, ...o.redo] } : o));
  const redo = () => setS((o) => (o.redo.length ? { list: o.redo[0], undo: [...o.undo, o.list], redo: o.redo.slice(1) } : o));
  const reset = (list) => setS({ list, undo: [], redo: [] });
  return { areas: s.list, set, undo, redo, reset, canUndo: s.undo.length > 0, canRedo: s.redo.length > 0 };
}

export function HideTool({ job: id }) {
  const [job, , error] = useTool(id);
  const i = job ? currentIndex(job) : -1;
  const preview = usePreview(job, i);
  const stage = useRef();
  const box = useRef();
  const h = useHistory([]);
  const [sel, setSel] = useState(-1);
  const [style, setStyle] = useState(null);
  const [strength, setStrength] = useState(null);
  const [busy, setBusy] = useState(false);
  const drag = useRef(null);
  const fit = useFit(stage, preview?.width, preview?.height, 48);
  useEffect(() => { h.reset([]); setSel(-1); }, [i]);
  useEffect(() => {
    if (job && style === null) { setStyle(job.preset.image.hideStyle || 'Pixelate'); setStrength(job.preset.image.hideStrength || 5); }
  }, [job]);

  const pos = (e) => {
    if (!box.current) return [0, 0];
    const b = box.current.getBoundingClientRect();
    return [clamp01((e.clientX - b.left) / b.width), clamp01((e.clientY - b.top) / b.height)];
  };
  const down = (e, k, mode) => {
    e.preventDefault(); e.stopPropagation();
    const [x, y] = pos(e);
    if (k === undefined) {
      drag.current = { mode: 'new', x0: x, y0: y, before: h.areas, k: h.areas.length };
      h.set([...h.areas, { x, y, width: 0, height: 0 }]);
      setSel(h.areas.length);
    } else {
      setSel(k);
      drag.current = { mode, x0: x, y0: y, k, a: { ...h.areas[k] }, first: true };
    }
    box.current.setPointerCapture?.(e.pointerId);
  };
  const move = (e) => {
    const d = drag.current;
    if (!d || !box.current) return;
    const [x, y] = pos(e);
    let a;
    if (d.mode === 'new') a = { x: Math.min(d.x0, x), y: Math.min(d.y0, y), width: Math.abs(x - d.x0), height: Math.abs(y - d.y0) };
    else if (d.mode === 'move') a = { ...d.a, x: clamp01(Math.min(1 - d.a.width, d.a.x + x - d.x0)), y: clamp01(Math.min(1 - d.a.height, d.a.y + y - d.y0)) };
    else {
      let l = d.a.x, tp = d.a.y, r = d.a.x + d.a.width, b = d.a.y + d.a.height;
      if (d.mode.includes('w')) l = Math.min(r - 0.004, x); if (d.mode.includes('e')) r = Math.max(l + 0.004, x);
      if (d.mode.includes('n')) tp = Math.min(b - 0.004, y); if (d.mode.includes('s')) b = Math.max(tp + 0.004, y);
      a = { x: l, y: tp, width: r - l, height: b - tp };
    }
    const list = h.areas.map((o, k) => (k === d.k ? a : o));
    h.set(list, !d.first);
    d.first = false;
  };
  const up = () => {
    const d = drag.current;
    drag.current = null;
    if (d?.mode === 'new') {
      const a = h.areas[d.k];
      // A click, not a drag: no frame (MinAreaSide).
      if (!a || a.width < 0.004 || a.height < 0.004) { h.reset(d.before); setSel(-1); }
    }
  };
  const remove = () => { if (sel >= 0) { h.set(h.areas.filter((_, k) => k !== sel)); setSel(-1); } };
  const save = async () => {
    if (busy || !h.areas.length) return;
    setBusy(true);
    await guard(() => api.post(`api/tools/${id}/files/${i}/hide`, { areas: h.areas, style, strength }));
    setBusy(false);
  };
  useEffect(() => {
    const on = (e) => {
      if (e.target.closest?.('input,select,textarea') || document.querySelector('.modal-backdrop')) return;
      const mod = e.ctrlKey || e.metaKey;
      if (mod && e.key.toLowerCase() === 'z') { e.preventDefault(); e.shiftKey ? h.redo() : h.undo(); }
      else if (mod && e.key.toLowerCase() === 'y') { e.preventDefault(); h.redo(); }
      else if (e.key === 'Delete' || e.key === 'Backspace') remove();
      else if (e.key === 'Tab' && h.areas.length) { e.preventDefault(); setSel((sel + (e.shiftKey ? -1 : 1) + h.areas.length) % h.areas.length); }
      else if (e.key === 'Enter') save();
      else if (e.key === 'p' || e.key === 'P') setStyle('Pixelate');
      else if (e.key === 'b' || e.key === 'B') setStyle('Blur');
      else if (e.key === '+' || e.key === '=') setStrength(Math.min(10, strength + 1));
      else if (e.key === '-') setStrength(Math.max(1, strength - 1));
      else if (sel >= 0 && e.key.startsWith('Arrow')) {
        e.preventDefault();
        const s = (e.shiftKey ? 10 : 1) / Math.max(1, preview?.width || 1000);
        const dx = e.key === 'ArrowLeft' ? -s : e.key === 'ArrowRight' ? s : 0;
        const dy = e.key === 'ArrowUp' ? -s : e.key === 'ArrowDown' ? s : 0;
        h.set(h.areas.map((a, k) => (k === sel ? { ...a, x: clamp01(Math.min(1 - a.width, a.x + dx)), y: clamp01(Math.min(1 - a.height, a.y + dy)) } : a)));
      }
    };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  });
  if (error) return html`<${ToolError} error=${error} />`;
  if (!job) return html`<div class="tool-missing"><${Spinner} size=${28} /></div>`;
  const file = job.files[i];
  const next = job.files.slice(i + 1).some((f) => f.state === 'waiting');
  return html`<div class="tool">
    <${ToolHead} title=${file?.name || job.name} job=${job} index=${i} />
    ${finished(job) ? html`<div class="tool-body"><div class="tool-stage"><${Summary} job=${job} doneKey="hide.state_done" /></div></div>` : html`
    <div class="tool-body">
      <div class="tool-stage" ref=${stage}>
        ${!preview ? html`<${Spinner} size=${28} />` : preview.error ? html`<p class="danger" style="padding:20px">${preview.error}</p>` : html`
        <div class="canvas-wrap" ref=${box} style=${`width:${fit.width}px;height:${fit.height}px;cursor:crosshair`}
            onPointerDown=${(e) => down(e)} onPointerMove=${move} onPointerUp=${up}>
          <img src=${preview.url} width=${fit.width} height=${fit.height} alt="" draggable="false" />
          ${h.areas.map((a, k) => html`<div class=${'hide-area ' + (style === 'Blur' ? 'blur' : 'pixelate') + (k === sel ? ' on' : '')}
              style=${`left:${a.x * 100}%;top:${a.y * 100}%;width:${a.width * 100}%;height:${a.height * 100}%`}
              onPointerDown=${(e) => down(e, k, 'move')}>
            ${k === sel && ['nw', 'ne', 'se', 'sw'].map((hd) => html`<span class=${'handle ' + hd} onPointerDown=${(e) => down(e, k, hd)}></span>`)}
          </div>`)}
        </div>`}
        ${!h.areas.length && preview && !preview.error && html`<span class="stage-hint">${t('hide.empty_hint')}</span>`}
      </div>
      <aside class="tool-side">
        <${Segmented} value=${style} onChange=${setStyle} options=${[
          { value: 'Pixelate', label: t('hide.pixelate'), title: t('hide.pixelate_hint'), icon: 'Pixelate' },
          { value: 'Blur', label: t('hide.blur'), title: t('hide.blur_hint') }]} />
        <${Slider} label=${t('hide.strength')} min=${1} max=${10} value=${strength || 5} valueText=${(v) => v} onChange=${setStrength} />
        <p class="caption">${t('hide.strength_value', strength || 5)}</p>
        <div class="row tight">
          <${Button} icon="Undo" disabled=${!h.canUndo} onClick=${h.undo} title=${t('clip.undo')} />
          <${Button} disabled=${!h.canRedo} onClick=${h.redo} title=${t('clip.redo')}>↻</${Button}>
          <${Button} disabled=${sel < 0} onClick=${remove} title=${t('hide.remove_hint')}>${t('hide.remove')}</${Button}>
          <${Button} kind="ghost" disabled=${!h.areas.length} onClick=${() => { h.set([]); setSel(-1); }}>${t('hide.clear')}</${Button}>
        </div>
        <p class="caption">${t('hide.hint_keys')}</p>
        ${job.notice && html`<p class="caption">${job.notice}</p>`}
      </aside>
    </div>
    <${Queue} job=${job} doneKey="hide.state_done" />
    <footer class="tool-foot">
      <span class="spacer"></span>
      <${Button} onClick=${() => guard(() => api.post(`api/tools/${id}/files/${i}/skip`))}>${t('crop.skip')}</${Button}>
      <${Button} kind="accent" icon="Pixelate" disabled=${busy || !h.areas.length} onClick=${save}>${t(next ? 'hide.save_next' : 'hide.save')}</${Button}>
    </footer>`}
  </div>`;
}
