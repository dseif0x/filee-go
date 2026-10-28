// Edit pictures (ImageEditViewModel): size, format, turn and colour of the
// dropped pictures, chosen right before they are converted. The tool keeps
// the choices for next time; Convert starts ordinary conversions.
import { html, useState, useEffect, Button, Select, NumberInput, Check, Slider, Field, Spinner, Icon, guard } from '../ui.js';
import { api } from '../api.js';
import { t, formatName } from '../i18n.js';
import { useTool, ToolHead, Summary, ToolError, usePreview } from './common.js';

/** sizeAfter is the first picture's size once converted (turned, then resized). */
function sizeAfter(w, h, o) {
  if (!w || !h) return null;
  if (o.rotation === 'Left90' || o.rotation === 'Right90') [w, h] = [h, w];
  let s = 1;
  if (o.resize === 'Percent') s = o.resizePercent / 100;
  else if (o.resize === 'LongEdge') s = Math.min(1, o.longEdge / Math.max(w, h));
  else if (o.resize === 'Width') s = o.width / w;
  return [Math.max(1, Math.round(w * s)), Math.max(1, Math.round(h * s))];
}

export function ImageEditTool({ job: id }) {
  const [job, , error] = useTool(id);
  const [choices, setChoices] = useState(null);
  const [target, setTarget] = useState(null);
  const [img, setImg] = useState(null);
  const [started, setStarted] = useState(false);
  const preview = usePreview(job, job ? 0 : -1, 0, 720);
  useEffect(() => {
    if (!job || img) return;
    setTarget(job.preset.targetFormat || 'same');
    setImg(structuredClone(job.preset.image));
    api.post(`api/tools/${id}/image-edit?choices=1`).then(setChoices).catch(() => setChoices({ targets: [], lossy: ['jpg', 'webp', 'avif', 'heic'], resizeModes: ['None', 'Percent', 'LongEdge', 'Width'] }));
  }, [job]);
  if (error) return html`<${ToolError} error=${error} />`;
  if (!job || !img || !choices) return html`<div class="tool-missing"><${Spinner} size=${28} /></div>`;
  const set = (f) => setImg({ ...img, ...f });
  const showQuality = target === 'same' ? choices.keepsLossy : choices.lossy.includes(target);
  const after = sizeAfter(choices.width, choices.height, img);
  const heading = job.files.length === 1 ? job.files[0].name : t('image_edit.count', job.files.length);
  const convert = async () => {
    const r = await guard(() => api.post(`api/tools/${id}/image-edit`, { target, image: img }));
    if (r) setStarted(true);
  };
  const running = job.files.some((f) => f.state === 'running');
  const opt = (prefix, list) => list.map((x) => ({ value: x, label: t(prefix + x) }));
  return html`<div class="tool">
    <${ToolHead} title=${heading} job=${job} index=${-1} />
    ${started && !running ? html`<div class="tool-body"><div class="tool-stage"><${Summary} job=${job} /></div></div>` : html`
    <div class="tool-body">
      <div class="tool-stage" style="padding:20px">
        ${preview?.url ? html`<img src=${preview.url} alt="" style=${`max-width:100%;max-height:100%;transform:${rotationCss(img)};${img.grayscale ? 'filter:grayscale(1)' : ''};transition:transform .2s`} />`
          : preview?.error ? html`<${Icon} name="File" size=${64} />` : html`<${Spinner} size=${28} />`}
      </div>
      <aside class="tool-side" style="width:340px">
        <p class="caption">${t('image_edit.hint')}</p>
        <${Field} label=${t('presets.target')}>
          <${Select} value=${target} onChange=${setTarget} options=${[{ value: 'same', label: t('presets.same_as_source') },
            ...choices.targets.map((x) => ({ value: x, label: formatName(x) }))]} /></${Field}>
        <${Field} label=${t('presets.resize')}><${Select} value=${img.resize} options=${opt('presets.resize.', choices.resizeModes)} onChange=${(v) => set({ resize: v })} /></${Field}>
        ${img.resize === 'Percent' && html`<${Field} label=${t('presets.percent')}><${NumberInput} value=${img.resizePercent} min=${1} max=${1000} onChange=${(v) => set({ resizePercent: v })} /></${Field}>`}
        ${img.resize === 'LongEdge' && html`<${Field} label=${t('presets.long_edge')}><${NumberInput} value=${img.longEdge} min=${16} max=${20000} onChange=${(v) => set({ longEdge: v })} /></${Field}>`}
        ${img.resize === 'Width' && html`<${Field} label=${t('presets.width')}><${NumberInput} value=${img.width} min=${16} max=${20000} onChange=${(v) => set({ width: v })} /></${Field}>`}
        <div class="grid2">
          <${Field} label=${t('presets.rotation')}><${Select} value=${img.rotation || 'None'} options=${opt('presets.rotation.', ['None', 'Right90', 'Left90', 'Rotate180'])} onChange=${(v) => set({ rotation: v })} /></${Field}>
          <${Field} label=${t('presets.flip')}><${Select} value=${img.flip || 'None'} options=${opt('presets.flip.', ['None', 'Horizontal', 'Vertical'])} onChange=${(v) => set({ flip: v })} /></${Field}>
        </div>
        <${Check} label=${t('presets.grayscale')} checked=${img.grayscale} onChange=${(v) => set({ grayscale: v })} />
        ${showQuality && html`<${Slider} label=${t('presets.quality')} min=${1} max=${100} value=${img.quality} onChange=${(v) => set({ quality: v })} />`}
        ${after && html`<p class="muted time">${choices.width} × ${choices.height} → <strong>${after[0]} × ${after[1]}</strong></p>`}
      </aside>
    </div>
    <footer class="tool-foot"><span class="spacer"></span>
      <${Button} kind="accent" disabled=${started} onClick=${convert}>${started ? '…' : t('image_edit.convert')}</${Button}></footer>`}
  </div>`;
}

function rotationCss(o) {
  const r = { Right90: 90, Left90: -90, Rotate180: 180 }[o.rotation] || 0;
  const f = o.flip === 'Horizontal' ? ' scaleX(-1)' : o.flip === 'Vertical' ? ' scaleY(-1)' : '';
  return `rotate(${r}deg)${f}`;
}
