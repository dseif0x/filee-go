// Presets: the library on the left (new, ready-made for a place, duplicate,
// delete, search, drag to reorder), the editor of the selected one on the
// right with Save and Revert (PresetsPage.axaml).
import { html, useState, useEffect, Card, Button, IconButton, Icon, Empty, confirmDialog, guard, showDialog } from '../ui.js';
import { store, api } from '../api.js';
import { t, presetName, formatName } from '../i18n.js';
import { PresetEditor, saveEdited, editGroupDialog } from './preset-editor.js';
import { caption } from './toolbar.js';

function AddMenu({ onAdd }) {
  const [open, setOpen] = useState(false);
  const [place, setPlace] = useState(null);
  useEffect(() => { if (open && !place) api.get('api/presets/factory').then((f) => setPlace(f.place)).catch(() => setPlace([])); }, [open]);
  return html`<div style="position:relative">
    <${Button} kind="accent" icon="Plus" onClick=${() => setOpen(!open)}>${t('presets.add')}</${Button}>
    ${open && html`<div class="card menu" style="position:absolute;z-index:10;top:36px;left:0;width:260px;padding:6px;gap:2px" onMouseLeave=${() => setOpen(false)}>
      <button type="button" class="menu-item" onClick=${() => { setOpen(false); onAdd(null); }}>${t('presets.add_empty')}</button>
      <div class="caption" style="padding:6px 10px 2px">${t('presets.add_ready')}</div>
      ${(place || []).map((p) => html`<button type="button" class="menu-item" onClick=${() => { setOpen(false); onAdd(p); }}>${presetName(p)}
        <span class="caption"> · ${formatName(p.targetFormat)}</span></button>`)}
    </div>`}
  </div>`;
}

export function PresetsPage({ route }) {
  const lib = store.library;
  const [selId, setSelId] = useState(route?.sub?.[0] || null);
  const [search, setSearch] = useState('');
  const [state, setState] = useState(null);
  const [rev, setRev] = useState(0);
  const [drag, setDrag] = useState(null);
  if (!lib) return null;
  const sel = lib.presets.find((p) => p.id === selId);
  const q = search.trim().toLowerCase();
  const shown = lib.presets.filter((p) => !q || presetName(p).toLowerCase().includes(q) || formatName(p.targetFormat).toLowerCase().includes(q));
  const add = async (from) => {
    const body = from ? { ...structuredClone(from), id: '' } : { name: t('presets.add_empty'), targetFormat: 'png', kind: 'Convert' };
    const p = await guard(() => api.post('api/presets?new=1', body));
    if (p) setSelId(p.id);
  };
  const duplicate = async () => {
    if (!sel) return;
    const p = await guard(() => api.post('api/presets?new=1', { ...structuredClone(sel), id: '', name: presetName(sel) + ' (2)', nameKey: '' }));
    if (p) setSelId(p.id);
  };
  const remove = async () => {
    if (!sel) return;
    if (!(await confirmDialog(t('presets.delete_confirm', presetName(sel)), { danger: true, ok: t('presets.delete') }))) return;
    const i = lib.presets.indexOf(sel);
    await guard(() => api.del('api/presets/' + sel.id));
    setSelId(lib.presets[i + 1]?.id || lib.presets[i - 1]?.id || null);
  };
  const reorder = async (from, to) => {
    const ids = lib.presets.map((p) => p.id);
    const [x] = ids.splice(from, 1);
    ids.splice(to, 0, x);
    await guard(() => api.post('api/presets/reorder', { ids }));
  };
  const save = async () => {
    if (!state || !sel) return;
    await saveEdited(state.p, sel, state.ui);
  };
  return html`<div class="stack">
    <h1>${t('presets.title')}</h1>
    <div class="presets-grid">
      <div class="card presets-list">
        <div class="row tight">
          <${AddMenu} onAdd=${add} />
          <${IconButton} icon="Copy" title=${t('presets.duplicate')} disabled=${!sel} onClick=${duplicate} />
          <${IconButton} icon="Delete" title=${t('presets.delete')} disabled=${!sel} onClick=${remove} />
        </div>
        <input class="input" placeholder=${t('common.search')} value=${search} onInput=${(e) => setSearch(e.currentTarget.value)} />
        <ul class="select-list">${shown.map((p) => {
          const i = lib.presets.indexOf(p);
          return html`<li draggable=${!q ? 'true' : undefined} onDragStart=${() => setDrag(i)} onDragOver=${(e) => e.preventDefault()}
              onDrop=${(e) => { e.preventDefault(); if (drag !== null && drag !== i) reorder(drag, i); setDrag(null); }}>
            <button type="button" class=${p.id === selId ? 'on' : ''} onClick=${() => { if (p.kind === 'Group') { setSelId(p.id); editGroupDialog(p); } else setSelId(p.id); }}>
              ${p.kind === 'Group' && html`<${Icon} name="Shapes" size=${13} />`}${p.kind === 'Tool' && html`<${Icon} name="Crop" size=${13} />`}
              <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${presetName(p)}</span>
              <span class="sub">${caption(p)}</span></button></li>`;
        })}</ul>
      </div>
      <div>
        ${!sel || sel.kind === 'Group' ? html`<${Card}><${Empty} icon="Tune">${t('presets.select')}</${Empty}></${Card}>` : html`
          <div class="editor-bar">
            <h2 class="grow" style="flex:1">${presetName(sel)}</h2>
            <${Button} onClick=${() => setRev(rev + 1)}>${t('presets.revert')}</${Button}>
            <${Button} kind="accent" onClick=${save}>${t('presets.save')}</${Button}>
          </div>
          <${PresetEditor} key=${sel.id + ':' + rev} preset=${sel} onState=${(p, ui) => setState({ p, ui })} />`}
      </div>
    </div>
  </div>`;
}
