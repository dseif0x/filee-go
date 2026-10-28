// Crop (CropOverlayViewModel, CropCanvas, AspectDial): a frame over each
// picture or PDF page in turn, an aspect ratio to keep, Crop & Save / Next,
// Skip, Reset. Arrow keys move the frame (Shift × 10), X swaps portrait and
// landscape, Enter saves, Esc closes.
import { html, useState, useEffect, useRef, Button, NumberInput, Icon, Spinner, guard } from '../ui.js';
import { api } from '../api.js';
import { t } from '../i18n.js';
import { useTool, currentIndex, ToolHead, Queue, Summary, finished, usePreview, useFit, ToolError } from './common.js';

export const aspectModes = ['free', 'original', '1:1', '5:4', '4:3', '3:2', '16:9', 'custom'];
const canSwap = (m) => ['5:4', '4:3', '3:2', '16:9', 'custom'].includes(m);

export function ratioOf(mode, portrait, w, h, custom) {
  const r = { original: w > 0 && h > 0 ? w / h : 0, '1:1': 1, '5:4': 5 / 4, '4:3': 4 / 3, '3:2': 3 / 2, '16:9': 16 / 9, custom }[mode] || 0;
  return portrait && canSwap(mode) && r > 0 ? 1 / r : r;
}

/** centered is the largest frame of ratio (pixels w/h) on a w × h picture, in parts of it. */
export function centered(ratio, w, h) {
  if (!(ratio > 0) || !w || !h) return { x: 0, y: 0, width: 1, height: 1 };
  const fw = Math.min(1, (ratio * h) / w);
  const fh = Math.min(1, w / (ratio * h));
  return { x: (1 - fw) / 2, y: (1 - fh) / 2, width: fw, height: fh };
}

const clamp01 = (v) => Math.max(0, Math.min(1, v));

/** RectEditor lets one frame be moved and resized over a box of w × h display pixels. */
export function useRectDrag({ rect, setRect, ratio, pw, ph, boxRef, min = 0.01 }) {
  const drag = useRef(null);
  const start = (e, mode) => {
    e.preventDefault();
    e.stopPropagation();
    const box = boxRef.current.getBoundingClientRect();
    drag.current = { mode, x0: e.clientX, y0: e.clientY, r: { ...rect }, box };
    e.currentTarget.setPointerCapture?.(e.pointerId);
  };
  const move = (e) => {
    const d = drag.current;
    if (!d) return;
    const dx = (e.clientX - d.x0) / d.box.width;
    const dy = (e.clientY - d.y0) / d.box.height;
    let { x, y, width, height } = d.r;
    if (d.mode === 'move') {
      x = Math.max(0, Math.min(1 - width, x + dx));
      y = Math.max(0, Math.min(1 - height, y + dy));
    } else if (d.mode === 'new') {
      const px = clamp01((d.x0 - d.box.left) / d.box.width);
      const py = clamp01((d.y0 - d.box.top) / d.box.height);
      const qx = clamp01((e.clientX - d.box.left) / d.box.width);
      let qy = clamp01((e.clientY - d.box.top) / d.box.height);
      if (ratio > 0) {
        const w = Math.abs(qx - px);
        const hh = (w * pw) / ratio / ph;
        qy = py + Math.sign(qy - py || 1) * hh;
      }
      x = Math.min(px, qx); y = Math.min(py, qy); width = Math.abs(qx - px); height = Math.abs(qy - py);
      if (y < 0 || y + height > 1) { const over = y < 0 ? -y : y + height - 1; height -= over; if (ratio > 0) width = (height * ph * ratio) / pw; y = Math.max(0, y); }
    } else {
      let l = x, tp = y, r = x + width, b = y + height;
      if (d.mode.includes('w')) l = clamp01(Math.min(r - min, l + dx));
      if (d.mode.includes('e')) r = clamp01(Math.max(l + min, r + dx));
      if (d.mode.includes('n')) tp = clamp01(Math.min(b - min, tp + dy));
      if (d.mode.includes('s')) b = clamp01(Math.max(tp + min, b + dy));
      if (ratio > 0) {
        // Keep the ratio: the dragged side leads, the other follows.
        const horizontal = d.mode === 'e' || d.mode === 'w' || (d.mode.length === 2 && Math.abs(dx) >= Math.abs(dy));
        let w = r - l, h = b - tp;
        if (horizontal) h = (w * pw) / ratio / ph; else w = (h * ph * ratio) / pw;
        if (d.mode.includes('n')) tp = b - h; else b = tp + h;
        if (d.mode.includes('w')) l = r - w; else if (d.mode.includes('e') || d.mode === 'n' || d.mode === 's') r = l + w;
        if (l < 0 || r > 1 || tp < 0 || b > 1) return;
      }
      x = l; y = tp; width = r - l; height = b - tp;
    }
    if (width > 0.001 && height > 0.001) setRect({ x, y, width, height });
  };
  const end = () => { drag.current = null; };
  return { start, move, end };
}

const handles = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'];

export function CropTool({ job: id }) {
  const [job, , error] = useTool(id);
  const i = job ? currentIndex(job) : -1;
  const [page] = useState(0);
  const preview = usePreview(job, i, page);
  const stage = useRef();
  const box = useRef();
  const [mode, setMode] = useState('free');
  const [portrait, setPortrait] = useState(false);
  const [cw, setCw] = useState(1080);
  const [ch, setCh] = useState(1350);
  const [rect, setRect] = useState({ x: 0.05, y: 0.05, width: 0.9, height: 0.9 });
  const [busy, setBusy] = useState(false);
  const pw = preview?.width || 0;
  const ph = preview?.height || 0;
  const ratio = ratioOf(mode, portrait, pw, ph, Math.max(1, cw) / Math.max(1, ch));
  const fit = useFit(stage, pw, ph, 48);
  useEffect(() => { if (pw) setRect(ratio > 0 ? centered(ratio, pw, ph) : { x: 0.05, y: 0.05, width: 0.9, height: 0.9 }); }, [pw, ph, i]);
  useEffect(() => { if (pw && ratio > 0) setRect(centered(ratio, pw, ph)); }, [mode, portrait, cw, ch]);
  const d = useRectDrag({ rect, setRect, ratio, pw, ph, boxRef: box });
  const save = async () => {
    if (busy || i < 0) return;
    setBusy(true);
    await guard(() => api.post(`api/tools/${id}/files/${i}/crop`, rect));
    setBusy(false);
  };
  const skip = () => guard(() => api.post(`api/tools/${id}/files/${i}/skip`));
  const reset = () => setRect(ratio > 0 ? centered(ratio, pw, ph) : { x: 0, y: 0, width: 1, height: 1 });
  useEffect(() => {
    const on = (e) => {
      if (e.target.closest?.('input,select,textarea') || document.querySelector('.modal-backdrop')) return;
      const step = (e.shiftKey ? 10 : 1) / Math.max(1, pw);
      const stepY = (e.shiftKey ? 10 : 1) / Math.max(1, ph);
      const mv = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -stepY], ArrowDown: [0, stepY] }[e.key];
      if (mv) { e.preventDefault(); setRect((r) => ({ ...r, x: Math.max(0, Math.min(1 - r.width, r.x + mv[0])), y: Math.max(0, Math.min(1 - r.height, r.y + mv[1])) })); }
      else if (e.key === 'Enter') { e.preventDefault(); save(); }
      else if ((e.key === 'x' || e.key === 'X') && canSwap(mode)) setPortrait(!portrait);
    };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  });
  if (error) return html`<${ToolError} error=${error} />`;
  if (!job) return html`<div class="tool-missing"><${Spinner} size=${28} /></div>`;
  const file = job.files[i];
  const px = Math.max(1, Math.round(rect.width * pw));
  const py = Math.max(1, Math.round(rect.height * ph));
  const label = (m) => (m === 'free' ? t('crop.aspect_free') : m === 'original' ? t('crop.aspect_original') : m === 'custom' ? '…'
    : portrait && canSwap(m) ? m.split(':').reverse().join(':') : m);
  return html`<div class="tool">
    <${ToolHead} title=${file?.name || job.name} job=${job} index=${i} />
    ${finished(job) ? html`<div class="tool-body"><div class="tool-stage"><${Summary} job=${job} /></div></div>` : html`
    <div class="tool-body">
      <div class="tool-stage" ref=${stage}>
        ${!preview ? html`<${Spinner} size=${28} />` : preview.error ? html`<p class="danger" style="padding:20px">${preview.error}</p>` : html`
        <div class="canvas-wrap" ref=${box} style=${`width:${fit.width}px;height:${fit.height}px`}
            onPointerDown=${(e) => d.start(e, 'new')} onPointerMove=${d.move} onPointerUp=${d.end}>
          <img src=${preview.url} width=${fit.width} height=${fit.height} alt="" draggable="false" />
          <div class="crop-frame" style=${`left:${rect.x * 100}%;top:${rect.y * 100}%;width:${rect.width * 100}%;height:${rect.height * 100}%`}
              onPointerDown=${(e) => d.start(e, 'move')}>
            <div class="grid"></div>
            ${handles.map((h) => html`<span class=${'handle ' + h} onPointerDown=${(e) => d.start(e, h)}></span>`)}
          </div>
          <span class="size-chip" style=${`left:${rect.x * 100}%;top:calc(${(rect.y + rect.height) * 100}% + 8px)`}>${px} × ${py}</span>
        </div>`}
        <span class="stage-hint">${t('crop.hint_keys')}</span>
      </div>
      <aside class="tool-side">
        <div><span class="label">${t('crop.aspect_title')}</span>
          <div class="aspect-row">${aspectModes.map((m) => html`<button type="button" class=${'btn' + (mode === m ? ' on' : '')}
            title=${m === 'custom' ? t('crop.aspect_custom') : label(m)} onClick=${() => setMode(m)}>${label(m)}</button>`)}</div></div>
        ${mode === 'custom' && html`<div class="row">
          <label class="field"><span class="label">${t('crop.custom_width')}</span><${NumberInput} value=${cw} min=${1} max=${100000} onChange=${setCw} /></label>
          <label class="field"><span class="label">${t('crop.custom_height')}</span><${NumberInput} value=${ch} min=${1} max=${100000} onChange=${setCh} /></label></div>`}
        ${canSwap(mode) && html`<${Button} icon="Refresh" title=${t('crop.swap')} onClick=${() => setPortrait(!portrait)}>${t(portrait ? 'crop.portrait' : 'crop.landscape')}</${Button}>`}
        <div class="caption">${pw} × ${ph} → <strong>${px} × ${py}</strong></div>
        <div class="spacer"></div>
        ${job.notice && html`<p class="caption">${job.notice}</p>`}
      </aside>
    </div>
    <${Queue} job=${job} />
    <footer class="tool-foot">
      <${Button} onClick=${reset}>${t('crop.reset')}</${Button}>
      <span class="spacer"></span>
      <${Button} onClick=${skip} disabled=${busy}>${t('crop.skip')}</${Button}>
      <${Button} kind="accent" icon="Crop" onClick=${save} disabled=${busy || !preview || preview.error}>
        ${busy ? '…' : t(job.files.slice(i + 1).some((f) => f.state === 'waiting') ? 'crop.save_next' : 'crop.save')}</${Button}>
    </footer>`}
  </div>`;
}
