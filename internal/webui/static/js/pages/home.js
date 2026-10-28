// Home: the drop zone (files dropped on the page are uploaded; files picked
// on this computer convert in place), the donut's presets for them, the
// running conversions and the recent ones (HomePage.axaml).
import { html, useState, useEffect, Card, Button, Icon, IconButton, Progress, pickFiles, guard, notify } from '../ui.js';
import { store, api, upload, openPath } from '../api.js';
import { t, baseName, donutName, presetName, formatName, profileName, modifierLabel } from '../i18n.js';
import { HistoryList } from './history.js';

function DonutChooser({ files, uploaded, onDone }) {
  const [donut, setDonut] = useState(null);
  const [back, setBack] = useState(false);
  const [group, setGroup] = useState(null);
  useEffect(() => { api.post('api/donut', { paths: files }).then(setDonut).catch((e) => notify(e.message, 'danger')); }, [files.join('\n')]);
  if (!donut) return null;
  const presets = store.library?.presets || [];
  const slices = group ? (group.subPresetIds || []).map((id) => presets.find((p) => p.id === id)).filter(Boolean).map((p) => ({ preset: p, name: donutName(p), usable: true }))
    : (back ? donut.back : donut.front) || [];
  const run = async (p) => {
    if (p.kind === 'Group') { setGroup(p); return; }
    if (p.kind === 'Tool') {
      const r = await guard(() => api.post('api/tools', { presetId: p.id, paths: files }));
      if (r) location.hash = r.url;
      return;
    }
    const folder = uploaded ? (store.settings.browserDrops.folder || store.boot.downloads) : '';
    const j = await guard(() => api.post('api/jobs', { paths: files, presetId: p.id, folder }));
    if (j) { notify(t('toast.converting', files.length === 1 ? baseName(files[0]) : t('home.files', files.length), presetName(p))); onDone(); }
  };
  return html`<${Card} title=${profileName(donut.profile)} actions=${html`
      ${donut.back?.length > 0 && !group && html`<${Button} kind="ghost" icon="Refresh" onClick=${() => setBack(!back)}>${t(back ? 'donut.flip_to_front' : 'donut.flip_to_back', '')}</${Button}>`}
      ${group && html`<${Button} kind="ghost" onClick=${() => setGroup(null)}>←</${Button}>`}
      <${IconButton} icon="Close" title=${t('common.close')} onClick=${onDone} />`}>
    <p class="caption">${files.length === 1 ? baseName(files[0]) : t('donut.files_mixed', files.length)}</p>
    ${slices.length === 0 ? html`<p class="muted">${group ? t('donut.group_empty') : t('donut.empty')}</p>` : html`<div class="donut-picker">
      ${slices.map((s) => html`<${Button} kind=${s.preset.kind === 'Group' ? '' : 'accent'} disabled=${!s.usable}
          title=${presetName(s.preset)} onClick=${() => run(s.preset)}>
        ${s.name}${s.preset.kind === 'Convert' && html` <span class="caption" style="color:inherit;opacity:.75">${formatName(s.preset.targetFormat)}</span>`}
        ${s.preset.kind === 'Group' && ' ▸'}</${Button}>`)}
    </div>`}
  </${Card}>`;
}

function Jobs() {
  const jobs = (store.jobs || []).slice(-6).reverse();
  if (!jobs.length) return null;
  return html`<${Card}>
    ${jobs.map((j) => {
      const done = j.files.filter((f) => f.state === 'Done').length;
      const failed = j.files.filter((f) => f.state === 'Failed').length;
      const running = j.state === 'Queued' || j.state === 'Running';
      const outs = j.files.flatMap((f) => f.outputs || []);
      const what = j.files.length === 1 ? baseName(j.files[0].sourcePath) : t('home.files', j.files.length);
      const label = running ? t('toast.converting', what, j.presetName)
        : `${what} · ${j.state === 'Cancelled' ? t('toast.cancelled') : failed ? t('toast.done_errors', failed, j.files.length) : t('toast.done')}`;
      return html`<div class="job">
        <div class="grow" style="flex:1;min-width:0">
          <div class="row between"><span class="title">${label}</span><span class="caption">${j.presetName}</span></div>
          ${running ? html`<${Progress} value=${j.progress} />` : failed > 0 && html`<div class="caption danger">${j.files.filter((f) => f.state === 'Failed').map((f) => t(f.errorKey || 'error.conversion_failed') + (f.errorDetail ? ' (' + f.errorDetail + ')' : '')).join(' · ')}</div>`}
        </div>
        ${running ? html`<${Button} kind="ghost" onClick=${() => api.post(`api/jobs/${j.id}/cancel`)}>${t('toast.cancel')}</${Button}>`
          : outs.length > 0 && html`<${Button} kind="ghost" icon="Folder" onClick=${() => openPath(outs[0], true)}>${t('toast.open_folder')}</${Button}>`}
      </div>`;
    })}
  </${Card}>`;
}

export function HomePage() {
  const [over, setOver] = useState(false);
  const [files, setFiles] = useState(null);
  const [busy, setBusy] = useState(false);
  const os = store.boot.os;
  const drag = (store.settings.triggers || []).find((g) => g.enabled && g.kind === 'Drag');
  const hintKey = os === 'darwin' ? 'home.drop_hint_macos' : os === 'linux' ? 'home.drop_hint_linux' : 'home.drop_hint';
  const onDrop = async (e) => {
    e.preventDefault();
    setOver(false);
    const list = [...(e.dataTransfer?.files || [])];
    if (!list.length) return;
    setBusy(true);
    const paths = await guard(() => upload(list));
    setBusy(false);
    if (paths?.length) setFiles({ paths, uploaded: true });
  };
  const pick = async () => {
    const p = await pickFiles({ multiple: true });
    if (p?.length) setFiles({ paths: p, uploaded: false });
  };
  return html`<div class="stack">
    <h1>${t('home.title')}</h1>
    <div class=${'drop-zone' + (over ? ' over' : '')} onDragOver=${(e) => { e.preventDefault(); setOver(true); }}
        onDragLeave=${() => setOver(false)} onDrop=${onDrop} onDblClick=${pick} role="button" tabindex="0"
        onKeyDown=${(e) => { if (e.key === 'Enter' || e.key === ' ') pick(); }}>
      <${Icon} name="Upload" size=${34} />
      <h2>${busy ? '…' : t('home.drop_title')}</h2>
      <p class="muted">${t('home.drop_select')}</p>
      ${drag && html`<p class="caption">${t(hintKey, modifierLabel(drag.modifiers))}</p>`}
      <${Button} onClick=${pick}>${t('presets.browse')}</${Button}>
    </div>
    ${files && html`<${DonutChooser} files=${files.paths} uploaded=${files.uploaded} onDone=${() => setFiles(null)} />`}
    <${Jobs} />
    <${HistoryList} limit=${40} />
  </div>`;
}
