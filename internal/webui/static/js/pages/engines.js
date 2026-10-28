// Engines: the optional engines (use your own copy, LibreOffice switches) and
// every engine in priority order, drag or arrows to change it (EnginesPage.axaml).
import { html, useState, useEffect, Card, Button, IconButton, Toggle, Icon, Spinner, Progress, pickFiles, confirmDialog, guard } from '../ui.js';
import { store, refresh, api, saveSettings } from '../api.js';
import { t, has, fileSize } from '../i18n.js';

const packages = ['libreoffice', 'pandoc', 'ghostscript', 'tesseract', 'ffmpeg', 'calibre'];

function engineName(e) { return has(`engine.${e.id}.name`) ? t(`engine.${e.id}.name`) : e.name; }

function guidance(pkg) {
  const os = store.boot.os === 'darwin' ? 'macos' : store.boot.os === 'linux' ? 'linux' : 'windows';
  const name = t(`engines.package.${pkg}.name`);
  return has(`engines.guidance.${os}.${pkg}`) ? t(`engines.guidance.${os}.${pkg}`) : t(`engines.guidance.${os}`, name);
}

function Package({ id, engines, pkg, reload }) {
  const s = store.settings;
  const own = s.engineOwnCopies?.[id];
  const engine = engines.find((e) => e.id === id);
  const ready = engine?.available;
  const [open, setOpen] = useState(false);
  const useOwn = async () => {
    const p = await pickFiles({ multiple: false, title: t('engines.use_own_title', t(`engines.package.${id}.name`)) });
    if (!p) return;
    await guard(() => saveSettings((x) => { x.engineOwnCopies = { ...(x.engineOwnCopies || {}), [id]: p }; }));
    await guard(() => api.post('api/engines/refresh'));
  };
  const stopOwn = async () => {
    await guard(() => saveSettings((x) => { delete x.engineOwnCopies[id]; }));
    await guard(() => api.post('api/engines/refresh'));
  };
  const save = (f) => guard(() => saveSettings(f));
  const act = async (what) => {
    if (what === 'remove' && !(await confirmDialog(t('engines.remove_confirm', t(`engines.package.${id}.name`)), { danger: true, ok: t('engines.remove') }))) return;
    await guard(() => api.post(`api/packages/${id}/${what}`));
    reload();
  };
  const busy = pkg?.state === 'downloading' || pkg?.state === 'unpacking';
  const stateText = busy ? (pkg.state === 'unpacking' ? t('engines.state.unpacking') : t('engines.state.downloading', Math.round(pkg.fraction * 100)))
    : pkg?.state === 'failed' ? t('engines.state.failed', pkg.error) : own ? t('engines.state.own_copy') : pkg?.installed ? t('engines.state.installed')
    : ready ? t('engines.state.system_copy') : t('engines.state.not_installed');
  return html`<div class="engine">
    <span class=${'status-dot' + (ready ? ' ok' : '')}></span>
    <div class="grow">
      <div class="row tight"><span class="name">${t(`engines.package.${id}.name`)}</span>
        <span class=${'pill ' + (ready || pkg?.installed ? 'ok' : pkg?.state === 'failed' ? 'bad' : '')}>${stateText}</span>
        ${engine?.version && html`<span class="caption">${engine.version}</span>`}</div>
      <p class="caption">${t(`engines.package.${id}.description`)}</p>
      ${pkg?.canDownload && !pkg.installed && html`<p class="caption">${t('engines.package.size', fileSize(pkg.downloadSize), fileSize(pkg.installedSize))}</p>`}
      ${!ready && !own && !pkg?.canDownload && html`<p class="caption">${guidance(id)}</p>`}
      ${busy && html`<${Progress} value=${pkg.fraction} />`}
      ${own && html`<p class="path">${own}</p>`}
      ${id === 'libreoffice' && open && html`<div class="stack tight" style="margin-top:8px">
        <strong>${t('engines.lo_title')}</strong><p class="caption">${t('engines.lo_description')}</p>
        <${Toggle} label=${t('engines.lo_slides')} hint=${t('engines.lo_slides_hint')} checked=${s.libreOfficeForSlides} onChange=${(v) => save((x) => { x.libreOfficeForSlides = v; })} />
        <${Toggle} label=${t('engines.lo_documents')} hint=${t('engines.lo_documents_hint')} checked=${s.libreOfficeForDocuments} onChange=${(v) => save((x) => { x.libreOfficeForDocuments = v; })} />
        <${Toggle} label=${t('engines.lo_sheets')} hint=${t('engines.lo_sheets_hint')} checked=${s.libreOfficeForSheets} onChange=${(v) => save((x) => { x.libreOfficeForSheets = v; })} />
        <${Toggle} label=${t('engines.lo_hwp')} hint=${t('engines.lo_hwp_hint')} checked=${s.libreOfficeForHwp} onChange=${(v) => save((x) => { x.libreOfficeForHwp = v; })} />
      </div>`}
    </div>
    <div class="row tight" style="align-self:flex-start">
      ${id === 'libreoffice' && html`<${Button} kind="ghost" icon="Settings" onClick=${() => setOpen(!open)}>${t('engines.settings')}</${Button}>`}
      ${busy ? html`<${Button} onClick=${() => act('cancel')}>${t('engines.cancel')}</${Button}>`
        : pkg?.installed ? html`${!pkg.upToDate && html`<${Button} kind="accent" onClick=${() => act('install')}>${t('engines.update')}</${Button}>`}<${Button} kind="ghost danger" onClick=${() => act('remove')}>${t('engines.remove')}</${Button}>`
        : pkg?.canDownload && !own && html`<${Button} kind="accent" icon="Download" onClick=${() => act('install')}>${t('engines.install')}</${Button}>`}
      ${own ? html`<${Button} onClick=${stopOwn}>${t('engines.stop_own')}</${Button}>`
        : html`<${Button} onClick=${useOwn} title=${t('engines.use_own_hint')}>${t('engines.use_own')}</${Button}>`}
    </div>
  </div>`;
}

export function EnginesPage() {
  const engines = store.engines;
  const [busy, setBusy] = useState(false);
  const [drag, setDrag] = useState(null);
  const [over, setOver] = useState(null);
  const [pkgs, setPkgs] = useState([]);
  const loadPkgs = () => api.get('api/packages').then(setPkgs).catch(() => {});
  useEffect(() => { refresh('engines'); loadPkgs(); }, []);
  useEffect(() => {
    if (!pkgs.some((p) => p.state === 'downloading' || p.state === 'unpacking')) return undefined;
    const h = setTimeout(loadPkgs, 700);
    return () => clearTimeout(h);
  }, [pkgs]);
  const recheck = async () => { setBusy(true); await guard(() => api.post('api/engines/refresh')); await refresh('engines'); setBusy(false); };
  const order = (engines || []).map((e) => e.id);
  const move = async (from, to) => {
    if (to < 0 || to >= order.length || from === to) return;
    const ids = [...order];
    const [x] = ids.splice(from, 1);
    ids.splice(to, 0, x);
    store.engines = ids.map((id) => engines.find((e) => e.id === id));
    await guard(() => saveSettings((s) => { s.enginePriority = [...ids, ...(s.enginePriority || []).filter((x) => !ids.includes(x))]; }));
    refresh('engines');
  };
  return html`<div class="stack">
    <div class="row between"><div><h1 style="margin-bottom:4px">${t('engines.title')}</h1><p class="muted">${t('engines.description')}</p></div>
      <${Button} icon="Refresh" onClick=${recheck} disabled=${busy}>${t('engines.recheck')}</${Button}></div>
    <${Card} title=${t('engines.optional_title')} hint=${t('engines.optional_description')}>
      <div>${packages.map((p) => html`<${Package} id=${p} engines=${engines || []} pkg=${pkgs.find((x) => x.id === p)} reload=${loadPkgs} />`)}</div>
    </${Card}>
    <${Card} title=${t('engines.all_title')} hint=${t('engines.all_description')}>
      ${!engines ? html`<${Spinner} />` : engines.length === 0 ? html`<p class="muted">—</p>` : html`<div>
        ${engines.map((e, i) => html`<div class=${'engine' + (drag === i ? ' dragging' : '') + (over === i && drag !== null && drag !== i ? ' drop-before' : '')}
            draggable="true" onDragStart=${(ev) => { setDrag(i); ev.dataTransfer.effectAllowed = 'move'; }}
            onDragOver=${(ev) => { ev.preventDefault(); setOver(i); }} onDragEnd=${() => { setDrag(null); setOver(null); }}
            onDrop=${(ev) => { ev.preventDefault(); if (drag !== null) move(drag, i); setDrag(null); setOver(null); }}>
          <span class="drag-handle"><${Icon} name="DragVertical" size=${16} /></span>
          <span class=${'status-dot' + (e.available ? ' ok' : '')} title=${e.available ? t('engines.available') : t('engines.unavailable')}></span>
          <div class="grow">
            <div class="row tight"><span class="name">${engineName(e)}</span>
              <span class=${'pill ' + (e.available ? 'ok' : 'bad')}>${e.available ? t('engines.available') : e.reason || t('engines.unavailable')}</span>
              ${e.version && html`<span class="caption">${e.version}</span>`}
              <span class="caption">${t('engines.conversions', e.edges)}</span></div>
            ${has(`engine.${e.id}.description`) && html`<p class="caption">${t(`engine.${e.id}.description`)}</p>`}
            ${e.detail && !e.available && html`<p class="caption">${e.detail}</p>`}
          </div>
          <${IconButton} icon="ArrowUp" title=${t('engines.move_up')} disabled=${i === 0} onClick=${() => move(i, i - 1)} />
          <${IconButton} icon="ArrowDown" title=${t('engines.move_down')} disabled=${i === engines.length - 1} onClick=${() => move(i, i + 1)} />
        </div>`)}
      </div>`}
    </${Card}>
  </div>`;
}
