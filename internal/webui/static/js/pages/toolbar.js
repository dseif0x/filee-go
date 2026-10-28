// Donut toolbar: 1. the profiles (which donut opens for which files, in
// order), 2. the donut of the chosen profile, front and back, arranged by
// dragging chips from the palette, and its appearance (ToolbarPage.axaml).
import { html, useState, useEffect, useRef, Card, Button, IconButton, Toggle, Check, Slider, TagEditor, Icon, Field, TextInput,
  confirmDialog, guard, notify } from '../ui.js';
import { store, api, saveSettings } from '../api.js';
import { t, presetName, donutName, profileName, formatName, categoryOf, format } from '../i18n.js';
import { editPresetDialog, editGroupDialog, newPresetDialog } from './preset-editor.js';

const tabDefs = [
  ['all', 'toolbar.tab.all', []],
  ['image', 'format.category.Image', ['Image', 'Vector']],
  ['document', 'format.category.Document', ['Pdf', 'Document', 'Spreadsheet', 'Presentation', 'Hwp', 'Text', 'Ebook', 'Data']],
  ['media', 'toolbar.tab.media', ['Video', 'Audio', 'Subtitle']],
  ['edit', 'toolbar.tab.edit', []],
  ['cad', 'toolbar.tab.model3d_cad', ['Model3D', 'Cad']],
  ['map', 'format.category.Geo', ['Geo']],
  ['archive', 'format.category.Archive', ['Archive']],
  ['font', 'format.category.Font', ['Font']],
];

function tabOf(p) {
  if (p.kind === 'Tool' || p.targetFormat === 'same') return 'edit';
  const c = categoryOf(p.targetFormat);
  return (tabDefs.find((d) => d[2].includes(c)) || tabDefs[0])[0];
}

export function caption(p) {
  if (p.kind === 'Group') return t('donut.group_caption', (p.subPresetIds || []).length);
  if (p.targetFormat === 'same') return t('donut.same_format');
  return formatName(p.targetFormat);
}

const findPreset = (id) => store.library?.presets.find((p) => p.id === id);

// ---- the donut ----

function polar(cx, cy, r, a) { return [cx + r * Math.sin(a), cy - r * Math.cos(a)]; }

function slicePath(cx, cy, r0, r1, a0, a1) {
  const large = a1 - a0 > Math.PI ? 1 : 0;
  const [x0, y0] = polar(cx, cy, r1, a0);
  const [x1, y1] = polar(cx, cy, r1, a1);
  const [x2, y2] = polar(cx, cy, r0, a1);
  const [x3, y3] = polar(cx, cy, r0, a0);
  if (a1 - a0 >= 2 * Math.PI - 1e-6) {
    return `M${cx} ${cy - r1}A${r1} ${r1} 0 1 1 ${cx - 0.01} ${cy - r1}ZM${cx} ${cy - r0}A${r0} ${r0} 0 1 0 ${cx + 0.01} ${cy - r0}Z`;
  }
  return `M${x0} ${y0}A${r1} ${r1} 0 ${large} 1 ${x1} ${y1}L${x2} ${y2}A${r0} ${r0} 0 ${large} 0 ${x3} ${y3}Z`;
}

/** Donut draws slices (clockwise from 12 o'clock) and reports drops and clicks by slice index. */
export function Donut({ items, size = 340, hole, opacity = 1, selected, onSelect, onDropAt, onDragSlice, onEdit, dropHint, centerText }) {
  const [dropAt, setDropAt] = useState(-1);
  const svg = useRef();
  const cx = size / 2;
  const r1 = size / 2 - 4;
  const r0 = r1 * (hole ?? store.settings?.donut?.holeRatio ?? 0.42);
  const n = Math.max(1, items.length);
  const step = (2 * Math.PI) / n;
  const indexAt = (e) => {
    const box = svg.current.getBoundingClientRect();
    const x = (e.clientX - box.left) * (size / box.width) - cx;
    const y = (e.clientY - box.top) * (size / box.height) - cx;
    let a = Math.atan2(x, -y);
    if (a < 0) a += 2 * Math.PI;
    return items.length ? Math.min(items.length, Math.floor((a + step / 2) / step) % (items.length + 1)) : 0;
  };
  return html`<svg ref=${svg} class="donut-svg" viewBox=${`0 0 ${size} ${size}`} style=${`width:${size}px;height:${size}px;opacity:${opacity}`}
      onDragOver=${(e) => { if (onDropAt) { e.preventDefault(); setDropAt(indexAt(e)); } }} onDragLeave=${() => setDropAt(-1)}
      onDrop=${(e) => { e.preventDefault(); const i = indexAt(e); setDropAt(-1); onDropAt?.(e.dataTransfer.getData('text/plain'), i); }}>
    ${items.length === 0 && html`<path class=${'slice' + (dropAt >= 0 ? ' drop' : '')} d=${slicePath(cx, cx, r0, r1, 0, 2 * Math.PI)} fill-rule="evenodd" />`}
    ${items.map((it, i) => {
      const a0 = i * step - step / 2 + 0.012;
      const a1 = (i + 1) * step - step / 2 - 0.012;
      const [tx, ty] = polar(cx, cx, (r0 + r1) / 2, i * step);
      const p = it.preset;
      return html`<g>
        <path class=${'slice' + (p?.kind === 'Group' ? ' group' : '') + (selected === i ? ' on' : '') + (dropAt === i ? ' drop' : '')}
          d=${slicePath(cx, cx, r0, r1, items.length === 1 ? 0 : a0, items.length === 1 ? 2 * Math.PI : a1)} fill-rule="evenodd"
          draggable=${onDragSlice ? 'true' : undefined} tabindex="0" role="button" aria-label=${it.label}
          onClick=${() => onSelect?.(i)} onDblClick=${() => onEdit?.(i)} onContextMenu=${(e) => { if (onEdit) { e.preventDefault(); onEdit(i); } }}
          onDragStart=${(e) => { e.dataTransfer.setData('text/plain', 'slice:' + i); e.dataTransfer.effectAllowed = 'move'; onDragSlice?.(i); }}>
          <title>${it.label}${it.caption ? ' · ' + it.caption : ''}</title></path>
        <text x=${tx} y=${ty - (it.caption ? 3 : -4)}>${trim(it.label, n > 8 ? 9 : 13)}</text>
        ${it.caption && html`<text class="sub" x=${tx} y=${ty + 10}>${trim(it.caption, 12)}</text>`}
      </g>`;
    })}
    <circle class="center" cx=${cx} cy=${cx} r=${r0 - 6} />
    <text x=${cx} y=${cx + 4} class="sub">${centerText ?? (dropHint && items.length === 0 ? t('donut.empty') : '')}</text>
  </svg>`;
}

function trim(s, n) { return s && s.length > n ? s.slice(0, n - 1) + '…' : s; }

/** DonutPreview shows the first profile's front, at the settings' opacity (Theme page). */
export function DonutPreview({ size = 220 }) {
  const prof = store.library?.profiles?.[0];
  const items = (prof?.presetIds || []).map(findPreset).filter(Boolean).map((p) => ({ preset: p, label: donutName(p), caption: caption(p) }));
  return html`<${Donut} items=${items} size=${size} opacity=${store.settings.donut.opacity} />`;
}

// ---- profile details ----

function ExtensionEditor({ profile, save, profiles }) {
  const [error, setError] = useState('');
  const known = (store.boot.formats || []).flatMap((f) => f.extensions);
  const notes = [];
  const index = profiles.indexOf(profile);
  for (const e of profile.extensions) {
    if (!known.includes(e.toLowerCase())) notes.push({ warn: true, text: t('toolbar.ext_unknown', e) });
    for (const [j, other] of profiles.entries()) {
      if (other === profile || other.isFallback || !other.extensions.some((x) => x.toLowerCase() === e.toLowerCase())) continue;
      notes.push({ warn: j < index, text: t(j < index ? 'toolbar.ext_claimed_first' : 'toolbar.ext_claimed_later', e, profileName(other)) });
    }
  }
  const normalize = (v) => {
    const x = v.trim().replace(/^\*?\./, '').toLowerCase();
    if (!x) return '';
    if (!/^[a-z0-9][a-z0-9._+-]*$/.test(x)) { setError(t('toolbar.ext_invalid', v)); return ''; }
    setError('');
    return x;
  };
  return html`<div class="stack tight">
    <span class="label">${t('toolbar.extensions')}</span>
    <${TagEditor} tags=${profile.extensions} placeholder=${t('toolbar.extensions_hint')} normalize=${normalize}
      suggestions=${known} onChange=${(tags) => save((p) => { p.extensions = tags; })} />
    ${error && html`<p class="caption danger">${error}</p>`}
    ${profile.extensions.length === 0 && html`<p class="caption">${t('toolbar.ext_none')}</p>`}
    ${notes.slice(0, 8).map((n) => html`<p class=${'caption' + (n.warn ? ' warning' : '')} style="white-space:pre-line">${n.text}</p>`)}
  </div>`;
}

function MixedEditor({ profile, save, profiles }) {
  const [open, setOpen] = useState({});
  const groups = profiles.filter((p) => !p.isFallback).map((p) => ({ id: p.id, name: profileName(p), exts: p.extensions }));
  const grouped = new Set(groups.flatMap((g) => g.exts.map((e) => e.toLowerCase())));
  const others = profile.extensions.filter((e) => !grouped.has(e.toLowerCase()));
  if (others.length) groups.push({ id: 'other', name: t('toolbar.mixed_other_group'), exts: others });
  const has = (e) => profile.extensions.some((x) => x.toLowerCase() === e.toLowerCase());
  const set = (exts, on) => save((p) => {
    const cur = p.extensions.filter((x) => !exts.some((e) => e.toLowerCase() === x.toLowerCase()));
    p.extensions = on ? [...cur, ...exts] : cur;
  });
  const fallbacks = profiles.filter((p) => p.isFallback);
  const catchAll = fallbacks.find((p) => p.extensions.length === 0);
  let hint = '';
  let warn = false;
  if (profile.extensions.length === 0) {
    if (catchAll && catchAll !== profile) { hint = t('toolbar.mixed_shadowed', profileName(catchAll)); warn = true; } else hint = t('toolbar.mixed_catch_all');
  } else if (catchAll) hint = t('toolbar.mixed_others', profileName(catchAll));
  else hint = t('toolbar.mixed_others_self');
  return html`<div class="stack tight">
    <span class="label">${t('toolbar.mixed_title')}</span>
    <ul class="list">${groups.map((g) => {
      const n = g.exts.filter(has).length;
      const state = n === 0 ? false : n === g.exts.length ? true : null;
      return html`<li style="flex-direction:column;align-items:stretch">
        <div class="row">
          <${IconButton} icon="ChevronRight" title=${t('toolbar.mixed_expand')} onClick=${() => setOpen({ ...open, [g.id]: !open[g.id] })} />
          <label class="check grow"><input type="checkbox" checked=${state === true} indeterminate=${state === null}
            onChange=${() => set(g.exts, state !== true)} /><span>${g.name}</span></label>
          <span class="caption">${n} / ${g.exts.length}</span>
        </div>
        ${open[g.id] && html`<div class="tag-list" style="margin:4px 0 4px 38px">${g.exts.map((e) => html`<button type="button"
          class=${'pill' + (has(e) ? ' accent' : '')} onClick=${() => set([e], !has(e))}>${e}</button>`)}</div>`}
      </li>`;
    })}</ul>
    <p class=${'caption' + (warn ? ' warning' : '')} style="white-space:pre-line">${hint}</p>
  </div>`;
}

// ---- the page ----

export function ToolbarPage({ route }) {
  const lib = store.library;
  const profiles = lib?.profiles || [];
  const [selId, setSelId] = useState(route?.sub?.[0] || profiles[0]?.id);
  const [back, setBack] = useState(false);
  const [search, setSearch] = useState('');
  const [paletteSearch, setPaletteSearch] = useState('');
  const [tab, setTab] = useState('all');
  const [selSlice, setSelSlice] = useState(-1);
  const [usable, setUsable] = useState({});
  const profile = profiles.find((p) => p.id === selId) || profiles[0];
  useEffect(() => { if (profile && profile.id !== selId) setSelId(profile.id); }, [profile?.id]);
  const extKey = profile ? profile.extensions.join(',') : '';
  useEffect(() => {
    if (!profile) return;
    api.get('api/profiles/' + profile.id + '/usable').then(setUsable).catch(() => setUsable({}));
  }, [profile?.id, extKey, lib?.presets?.length]);
  if (!lib || !profile) return html`<div class="stack"><h1>${t('toolbar.title')}</h1></div>`;

  const saveProfile = (f) => guard(async () => {
    const p = structuredClone(profile);
    f(p);
    const i = lib.profiles.findIndex((x) => x.id === p.id);
    lib.profiles[i] = p; // shown at once
    await api.put('api/profiles/' + p.id, p);
  });
  const ids = back ? profile.backPresetIds || [] : profile.presetIds;
  const setIds = (next) => {
    if (next.length > store.boot.limits.maxSlices) { notify(t('toolbar.max_reached', store.boot.limits.maxSlices), 'danger'); return; }
    saveProfile((p) => { if (back) p.backPresetIds = next; else p.presetIds = next; });
  };
  const items = ids.map(findPreset).filter(Boolean).map((p) => ({ preset: p, label: donutName(p), caption: caption(p) }));
  const placed = new Set([...profile.presetIds, ...(profile.backPresetIds || [])]);
  const q = paletteSearch.trim().toLowerCase();
  const candidates = lib.presets.filter((p) => !placed.has(p.id))
    .sort((a, b) => (b.kind === 'Group') - (a.kind === 'Group'))
    .map((p) => ({ p, label: presetName(p), cap: caption(p), tab: p.kind === 'Group' ? 'all' : tabOf(p), ok: p.kind === 'Group' || profile.extensions.length === 0 || usable[p.id] !== false }))
    .filter((c) => (q ? c.label.toLowerCase().includes(q) || (c.cap || '').toLowerCase().includes(q) : true));
  const activeTabs = new Set(candidates.filter((c) => c.p.kind !== 'Group').map((c) => c.tab));
  const tabs = tabDefs.filter((d) => d[0] === 'all' || activeTabs.has(d[0]));
  const shown = candidates.filter((c) => tab === 'all' || c.tab === tab || !tabs.some((d) => d[0] === tab));
  const shownProfiles = profiles.filter((p) => !search.trim() || profileName(p).toLowerCase().includes(search.trim().toLowerCase())
    || p.extensions.some((e) => e.includes(search.trim().toLowerCase())));
  const flip = store.settings.donut;
  const faceHint = flip.flipKey || flip.flipWithWheel
    ? t('toolbar.face_hint', [flip.flipKey, flip.flipWithWheel ? t('toolbar.face_hint_wheel') : ''].filter(Boolean).join(' / '))
    : t('toolbar.face_hint_off');
  const mixedCount = profiles.filter((p) => p.isFallback).length;

  const onDropAt = (data, at) => {
    if (data.startsWith('slice:')) {
      const from = +data.slice(6);
      const next = [...ids];
      const [x] = next.splice(from, 1);
      next.splice(at > from ? at - 1 : at, 0, x);
      setIds(next);
    } else if (data.startsWith('preset:')) {
      const id = data.slice(7);
      if (!ids.includes(id)) { const next = [...ids]; next.splice(at, 0, id); setIds(next); }
    }
  };
  const addProfile = async () => {
    const p = await guard(() => api.post('api/profiles', { name: t('toolbar.profile_new'), extensions: [], presetIds: [], backPresetIds: [] }));
    if (p) setSelId(p.id);
  };
  const editSlice = async (i) => {
    const p = findPreset(ids[i]);
    if (!p) return;
    if (p.kind === 'Group') await editGroupDialog(p); else await editPresetDialog(p);
  };
  const newPreset = async () => {
    const p = await newPresetDialog();
    if (p) setIds([...ids, p.id]);
  };
  const newGroup = async () => {
    const g = await guard(() => api.post('api/presets?new=1', { name: t('group.editor_title'), kind: 'Group', targetFormat: 'group', subPresetIds: [] }));
    if (g) { setIds([...ids, g.id]); await editGroupDialog(g); }
  };

  return html`<div class="stack">
    <h1>${t('toolbar.title')}</h1>
    <div class="toolbar-grid">
      <${Card} title=${t('toolbar.step_profile')}>
        <input class="input" placeholder=${t('common.search')} value=${search} onInput=${(e) => setSearch(e.currentTarget.value)} />
        <ul class="select-list" style="max-height:420px;padding:0">${shownProfiles.map((p) => html`<li><button type="button" class=${p.id === profile.id ? 'on' : ''}
            onClick=${() => { setSelId(p.id); setSelSlice(-1); }}
            onKeyDown=${(e) => { if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) { e.preventDefault(); guard(() => api.post(`api/profiles/${p.id}/move`, { direction: e.key === 'ArrowUp' ? -1 : 1 })); } }}>
          <span class="grow" style="flex:1">${profileName(p)}</span>
          ${p.isFallback && html`<span class="pill" title=${t('toolbar.mixed_badge')}>∗</span>`}
          <span class="sub">${p.presetIds.length}</span></button></li>`)}</ul>
        <div class="row tight">
          <${IconButton} icon="ArrowUp" title=${t('toolbar.profile_up')} onClick=${() => guard(() => api.post(`api/profiles/${profile.id}/move`, { direction: -1 }))} />
          <${IconButton} icon="ArrowDown" title=${t('toolbar.profile_down')} onClick=${() => guard(() => api.post(`api/profiles/${profile.id}/move`, { direction: 1 }))} />
          <span class="spacer"></span>
          <${Button} icon="Plus" onClick=${addProfile}>${t('toolbar.add_profile')}</${Button}>
        </div>
        <p class="caption">${t('toolbar.profile_order_hint')}</p>
      </${Card}>

      <div class="stack">
        <${Card} title=${t('toolbar.step_arrange')}>
          <div class="donut-wrap">
            <div class="segmented">
              <button type="button" class=${!back ? 'on' : ''} onClick=${() => { setBack(false); setSelSlice(-1); }}>${t('toolbar.face_front')} <span class="caption">${profile.presetIds.length}</span></button>
              <button type="button" class=${back ? 'on' : ''} onClick=${() => { setBack(true); setSelSlice(-1); }}>${t('toolbar.face_back')} <span class="caption">${(profile.backPresetIds || []).length}</span></button>
            </div>
            <p class="caption">${faceHint}</p>
            <${Donut} items=${items} size=${340} selected=${selSlice} onSelect=${setSelSlice} onDropAt=${onDropAt} onEdit=${editSlice} dropHint=${true}
              onDragSlice=${() => {}} centerText=${items.length ? `${items.length} / ${store.boot.limits.maxSlices}` : t('donut.empty')} />
            ${selSlice >= 0 && selSlice < items.length && html`<div class="row">
              <strong>${presetName(items[selSlice].preset)}</strong>
              <${IconButton} icon="ArrowUp" title=${t('group.move_up')} disabled=${selSlice === 0} onClick=${() => { const n = [...ids]; [n[selSlice - 1], n[selSlice]] = [n[selSlice], n[selSlice - 1]]; setIds(n); setSelSlice(selSlice - 1); }} />
              <${IconButton} icon="ArrowDown" title=${t('group.move_down')} disabled=${selSlice === items.length - 1} onClick=${() => { const n = [...ids]; [n[selSlice + 1], n[selSlice]] = [n[selSlice], n[selSlice + 1]]; setIds(n); setSelSlice(selSlice + 1); }} />
              <${IconButton} icon="Pencil" title=${t('donut.edit')} onClick=${() => editSlice(selSlice)} />
              <${IconButton} icon="Close" title=${t('tags.remove')} onClick=${() => { setIds(ids.filter((_, k) => k !== selSlice)); setSelSlice(-1); }} />
            </div>`}
          </div>
          <div class="grid2">
            <${Field} label=${t('toolbar.name')}><${TextInput} value=${profileName(profile)} onChange=${(v) => saveProfile((p) => { p.name = v.trim(); })} /></${Field}>
            <div>
              <${Toggle} label=${t('toolbar.mixed_toggle')} checked=${profile.isFallback} disabled=${profile.isFallback && mixedCount <= 1}
                onChange=${(v) => saveProfile((p) => { p.isFallback = v; })} />
              ${profile.isFallback && mixedCount <= 1 && html`<p class="caption">${t('toolbar.mixed_locked')}</p>`}
            </div>
          </div>
          ${profile.isFallback ? html`<${MixedEditor} profile=${profile} save=${saveProfile} profiles=${profiles} />`
            : html`<${ExtensionEditor} profile=${profile} save=${saveProfile} profiles=${profiles} />`}
          <div class="row">
            ${lib.canReset[profile.id] && html`<${Button} onClick=${async () => {
              if (await confirmDialog(t('toolbar.reset_profile_confirm', profileName(profile)))) guard(() => api.post(`api/profiles/${profile.id}/reset`));
            }}>${t('toolbar.reset_profile')}</${Button}>`}
            <${Button} kind="danger" disabled=${profile.isFallback && mixedCount <= 1} onClick=${async () => {
              if (await confirmDialog(t('toolbar.delete_profile') + ': ' + profileName(profile) + '?', { danger: true, ok: t('common.delete') })) {
                await guard(() => api.del('api/profiles/' + profile.id));
                setSelId(profiles.find((p) => p.id !== profile.id)?.id);
              }
            }}>${t('toolbar.delete_profile')}</${Button}>
          </div>
        </${Card}>

        <${Card} title=${t('toolbar.available')} actions=${html`
            <${Button} icon="Plus" onClick=${newPreset}>${t('toolbar.new_preset')}</${Button}>
            <${Button} icon="Shapes" onClick=${newGroup}>${t('toolbar.new_group')}</${Button}>`}>
          <input class="input" placeholder=${t('common.search')} value=${paletteSearch} onInput=${(e) => setPaletteSearch(e.currentTarget.value)} />
          ${tabs.length > 2 && html`<div class="tag-list">${tabs.map((d) => html`<button type="button" class=${'pill' + (tab === d[0] ? ' accent' : '')}
            onClick=${() => setTab(d[0])}>${t(d[1])}</button>`)}</div>`}
          <div class="palette" onDragOver=${(e) => e.preventDefault()}
              onDrop=${(e) => { const d = e.dataTransfer.getData('text/plain'); if (d.startsWith('slice:')) setIds(ids.filter((_, k) => k !== +d.slice(6))); }}>
            ${shown.map((c) => html`<button type="button" class=${'chip' + (c.ok ? '' : ' used')} draggable="true"
                title=${c.ok ? presetName(c.p) : presetName(c.p) + ' · ' + t('error.no_route')}
                onDragStart=${(e) => { e.dataTransfer.setData('text/plain', 'preset:' + c.p.id); e.dataTransfer.effectAllowed = 'copy'; }}
                onClick=${() => setIds([...ids, c.p.id])} onContextMenu=${(e) => { e.preventDefault(); c.p.kind === 'Group' ? editGroupDialog(c.p) : editPresetDialog(c.p); }}>
              ${c.p.kind === 'Group' && html`<${Icon} name="Shapes" size=${12} />`}${c.label}
              ${c.cap && !c.label.toLowerCase().includes(c.cap.toLowerCase()) && html`<span class="fmt">${c.cap}</span>`}</button>`)}
          </div>
          <div class="caption">${t('toolbar.hint_add')} · ${t('toolbar.hint_remove')} · ${t('toolbar.hint_reorder')} · ${t('toolbar.hint_edit')}</div>
        </${Card}>

        <${Card} title=${t('toolbar.appearance')}>
          <div class="grid3">
            <${Slider} label=${t('toolbar.size')} min=${110} max=${220} value=${store.settings.donut.outerRadius} onChange=${(v) => guard(() => saveSettings((x) => { x.donut.outerRadius = v; }))} />
            <${Slider} label=${t('toolbar.hole')} min=${25} max=${65} value=${Math.round(store.settings.donut.holeRatio * 100)} valueText=${(v) => v + ' %'}
              onChange=${(v) => guard(() => saveSettings((x) => { x.donut.holeRatio = v / 100; }))} />
            <${Slider} label=${t('toolbar.opacity')} min=${10} max=${100} value=${Math.round(store.settings.donut.opacity * 100)} valueText=${(v) => v + ' %'}
              onChange=${(v) => guard(() => saveSettings((x) => { x.donut.opacity = v / 100; }))} />
          </div>
        </${Card}>
      </div>
    </div>
  </div>`;
}
