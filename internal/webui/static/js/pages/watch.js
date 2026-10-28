// Watch folders: each folder, its preset, where results go, what happens to
// originals, which files, how they are named, a preview and recent results
// (WatchFoldersPage.axaml, RenameToolWindow).
import { html, useState, useEffect, Card, Button, IconButton, Toggle, Check, Select, Field, FolderField, TagEditor, TextInput,
  Icon, showDialog, guard } from '../ui.js';
import { store, saveSettings, refresh, api } from '../api.js';
import { t, presetName, baseName } from '../i18n.js';

const kinds = ['Replace', 'Remove', 'Prefix', 'Suffix', 'SpacesToUnderscores', 'RemoveCopyNumber', 'RemoveBrackets', 'RemoveLeadingNumber',
  'RemoveSymbols', 'Lowercase', 'Uppercase', 'Regex'];
const recipes = [
  ['watch.recipe.camera_to_photo', { kind: 'Replace', find: 'IMG_', replace: 'Photo_' }, 'IMG_0412'],
  ['watch.recipe.drop_camera_prefix', { kind: 'Remove', find: 'IMG_' }, 'IMG_0412'],
  ['watch.recipe.spaces', { kind: 'SpacesToUnderscores' }, 'trip to busan'],
  ['watch.recipe.copy_number', { kind: 'RemoveCopyNumber' }, 'report (2)'],
  ['watch.recipe.brackets', { kind: 'RemoveBrackets' }, 'song [remix] (live)'],
  ['watch.recipe.leading_number', { kind: 'RemoveLeadingNumber' }, '01 - intro'],
  ['watch.recipe.symbols', { kind: 'RemoveSymbols' }, 'invoice#12@acme!'],
  ['watch.recipe.lowercase', { kind: 'Lowercase' }, 'Report_FINAL'],
  ['watch.recipe.prefix', { kind: 'Prefix', replace: 'new_' }, 'report'],
  ['watch.recipe.suffix', { kind: 'Suffix', replace: '_final' }, 'report'],
  ['watch.recipe.date_dashes', { kind: 'Regex', find: '(\\d{4})(\\d{2})(\\d{2})', replace: '$1-$2-$3' }, 'scan_20261011'],
];
const usesFind = (k) => ['Replace', 'Remove', 'Regex'].includes(k);
const usesReplace = (k) => ['Replace', 'Prefix', 'Suffix', 'Regex'].includes(k);

function useRename(sample, pattern, steps, preset) {
  const [r, setR] = useState({ name: '', errors: [] });
  useEffect(() => {
    let live = true;
    api.post('api/rename/check', { sample, pattern, steps, preset }).then((x) => live && setR(x)).catch(() => {});
    return () => { live = false; };
  }, [sample, pattern, JSON.stringify(steps), preset]);
  return r;
}

function RenameDialog({ rule, preset, close }) {
  const [pattern, setPattern] = useState(rule.fileNamePattern || '');
  const [steps, setSteps] = useState(structuredClone(rule.renames || []));
  const [sample, setSample] = useState(t('watch.sample_file'));
  const effective = pattern.trim() || preset?.output?.fileNamePattern || '{name}';
  const r = useRename(sample, effective, steps, preset ? presetName(preset) : '');
  const setStep = (i, f) => setSteps(steps.map((s, k) => (k === i ? { ...s, ...f } : s)));
  return html`<div class="stack">
    <h2>${t('watch.rename_title')}</h2>
    <p class="caption" style="white-space:pre-line">${t('watch.rename_intro')}</p>
    <${Field} label=${t('watch.name')} hint=${t('watch.name_hint')}>
      <input class="input" value=${pattern} placeholder=${t('watch.name_placeholder', preset?.output?.fileNamePattern || '{name}')} onInput=${(e) => setPattern(e.currentTarget.value)} />
    </${Field}>
    <div class="tag-list">${['name', 'date', 'time', 'index', 'preset'].map((tok) => html`<button type="button" class="pill"
      onClick=${() => setPattern((pattern || '') + '{' + tok + '}')}>+ ${t('watch.token.' + tok)}</button>`)}</div>
    <div class="grid2">
      <div class="stack tight">
        <span class="label">${t('watch.rename_steps')}</span>
        ${steps.length === 0 && html`<p class="caption">${t('watch.rename_no_steps')}</p>`}
        ${steps.map((s, i) => html`<div class="card flat" style="padding:10px;gap:6px">
          <div class="row tight">
            <select class="input" style="flex:1" value=${s.kind} onChange=${(e) => setStep(i, { kind: e.currentTarget.value })}>
              ${kinds.map((k) => html`<option value=${k} selected=${k === s.kind}>${t('watch.kind.' + k)}</option>`)}</select>
            <${IconButton} icon="ArrowUp" title=${t('watch.step_up')} disabled=${i === 0} onClick=${() => { const n = [...steps]; [n[i - 1], n[i]] = [n[i], n[i - 1]]; setSteps(n); }} />
            <${IconButton} icon="Close" title=${t('watch.remove_step')} onClick=${() => setSteps(steps.filter((_, k) => k !== i))} />
          </div>
          ${usesFind(s.kind) && html`<input class="input" value=${s.find || ''} placeholder=${t(s.kind === 'Regex' ? 'watch.rename_find' : s.kind === 'Remove' ? 'watch.ph.remove' : 'watch.ph.find')}
            onInput=${(e) => setStep(i, { find: e.currentTarget.value })} />`}
          ${usesReplace(s.kind) && html`<input class="input" value=${s.replace || ''} placeholder=${t(s.kind === 'Regex' ? 'watch.rename_replace' : s.kind === 'Prefix' ? 'watch.ph.prefix' : s.kind === 'Suffix' ? 'watch.ph.suffix' : 'watch.ph.replace')}
            onInput=${(e) => setStep(i, { replace: e.currentTarget.value })} />`}
          ${r.errors?.[i] && html`<p class="caption danger">${t('watch.invalid_regex', r.errors[i])}</p>`}
        </div>`)}
        <${Button} icon="Plus" onClick=${() => setSteps([...steps, { kind: 'Replace', find: '', replace: '' }])}>${t('watch.add_step')}</${Button}>
      </div>
      <div class="stack tight">
        <span class="label">${t('watch.recipes')}</span>
        <p class="caption">${t('watch.recipes_hint')}</p>
        <ul class="select-list" style="padding:0;max-height:300px">${recipes.map(([key, step, ex]) => html`<li><button type="button"
            onDblClick=${() => setSteps([...steps, { ...step }])} title=${t('watch.recipe_add')}>
          <span style="flex:1">${t(key)}<br /><span class="caption">${ex}</span></span>
          <span class="btn icon-only ghost" onClick=${(e) => { e.stopPropagation(); setSteps([...steps, { ...step }]); }}><${Icon} name="Plus" size=${14} /></span></button></li>`)}</ul>
      </div>
    </div>
    <${Field} label=${t('watch.rename_try')}><input class="input" value=${sample} placeholder=${t('watch.rename_try_placeholder')} onInput=${(e) => setSample(e.currentTarget.value)} /></${Field}>
    <div class="row"><span class="label" style="margin:0">${t('watch.preview')}</span><code class="mono selectable">${sample} → ${r.name}${preset && preset.targetFormat !== 'same' ? '.' + preset.targetFormat : ''}</code></div>
    <div class="dialog-buttons">
      <${Button} onClick=${() => close(null)}>${t('common.cancel')}</${Button}>
      <${Button} kind="accent" onClick=${() => close({ pattern: pattern.trim(), steps })}>${t('common.save')}</${Button}>
    </div>
  </div>`;
}

function Rule({ rule, status, presets, update, remove }) {
  const [open, setOpen] = useState(false);
  const [includeError, setIncludeError] = useState('');
  const preset = presets.find((p) => p.id === rule.presetId);
  const r = useRename(t('watch.sample_file'), (rule.fileNamePattern || '').trim() || preset?.output?.fileNamePattern || '{name}', rule.renames || [], preset ? presetName(preset) : '');
  const st = !rule.folder ? t('watch.status_no_folder') : status?.error ? t('watch.status_error', status.error)
    : !rule.enabled ? t('watch.status_off') : status?.running ? t('watch.status_watching') : t('watch.status_starting');
  const ok = rule.enabled && status?.running && !status?.error;
  const recent = status?.recent;
  const failures = recent?.Failures || [];
  const out = rule.outputFolder || (rule.folder ? rule.folder.replace(/[\\/]$/, '') + (rule.folder.includes('\\') ? '\\' : '/') + 'converted' : '');
  const ext = preset && preset.targetFormat !== 'same' ? preset.targetFormat : 'jpg';
  return html`<${Card}>
    <div class="row between">
      <div class="row"><${Toggle} label="" checked=${rule.enabled} onChange=${(v) => update((x) => { x.enabled = v; })} />
        <span class=${ok ? 'success' : status?.error ? 'danger' : 'muted'}>${st}</span></div>
      <${Button} kind="ghost danger" icon="Delete" onClick=${remove}>${t('watch.remove')}</${Button}>
    </div>
    <${Field} label=${t('watch.folder')}><${FolderField} value=${rule.folder} placeholder=${t('watch.folder_placeholder')} onChange=${(v) => update((x) => { x.folder = v; })} /></${Field}>
    <div class="grid2">
      <${Field} label=${t('watch.preset')}><${Select} value=${rule.presetId} options=${presets.map((p) => ({ value: p.id, label: presetName(p) }))}
        onChange=${(v) => update((x) => { x.presetId = v; })} /></${Field}>
      <${Field} label=${t('watch.originals')} hint=${rule.originals === 'MoveToOriginals' ? t('watch.originals_hint') : ''}>
        <${Select} value=${rule.originals} options=${[{ value: 'Keep', label: t('watch.originals_keep') }, { value: 'MoveToOriginals', label: t('watch.originals_move') }]}
          onChange=${(v) => update((x) => { x.originals = v; })} /></${Field}>
    </div>
    <${Field} label=${t('watch.output')}><${FolderField} value=${rule.outputFolder} placeholder=${t('watch.output_placeholder')} onChange=${(v) => update((x) => { x.outputFolder = v; })} /></${Field}>
    <${Check} label=${t('watch.subfolders')} checked=${rule.includeSubfolders} onChange=${(v) => update((x) => { x.includeSubfolders = v; })} />
    <div><${Button} kind="ghost" onClick=${() => setOpen(!open)}>${open ? '▾' : '▸'} ${t('watch.options')}</${Button}></div>
    ${open && html`<div class="stack tight" style="margin-left:12px">
      <span class="label">${t('watch.include')}</span>
      <${TagEditor} tags=${rule.include || []} placeholder=${t('watch.include_placeholder')}
        onChange=${async (tags) => {
          const norm = [];
          for (const tag of tags) {
            const res = await api.post('api/watch/pattern', { typed: tag });
            if (res.error) { setIncludeError(res.error); continue; }
            if (res.pattern && !norm.includes(res.pattern)) norm.push(res.pattern);
          }
          if (norm.length === tags.length) setIncludeError('');
          update((x) => { x.include = norm; });
        }} />
      ${includeError && html`<p class="caption danger">${t('watch.invalid_regex', includeError)}</p>`}
      <p class="caption" style="white-space:pre-line">${t('watch.include_hint')}</p>
      <span class="label">${t('watch.rename')}</span>
      <p class="caption">${rule.fileNamePattern ? t('watch.name_summary', rule.fileNamePattern) : t('watch.name_summary_preset', preset?.output?.fileNamePattern || '{name}')}</p>
      ${(rule.renames || []).map((s) => html`<p class="caption">· ${t('watch.kind.' + s.kind)}${s.find ? ': ' + s.find : ''}${s.replace ? ' → ' + s.replace : ''}</p>`)}
      <div><${Button} icon="Pencil" onClick=${async () => {
        const res = await showDialog((close) => html`<${RenameDialog} rule=${rule} preset=${preset} close=${close} />`, { wide: true });
        if (res) update((x) => { x.fileNamePattern = res.pattern; x.renames = res.steps; });
      }}>${t('watch.rename_edit')}</${Button}></div>
      <span class="label">${t('watch.preview')}</span>
      <code class="mono selectable">${out ? out + (out.includes('\\') ? '\\' : '/') : ''}${r.name}.${ext}</code>
      <p class="caption">${rule.originals === 'MoveToOriginals' ? t('watch.preview_move') : t('watch.preview_keep')}</p>
    </div>`}
    ${recent && (recent.Converted > 0 || failures.length > 0) && html`<div class="row between">
      <div><span class="label" style="margin:0">${t('watch.results')}</span>
        <span class=${failures.length ? 'caption danger' : 'caption'}>${failures.length ? t('watch.results_failed', recent.Converted, failures.length) : t('watch.results_ok', recent.Converted)}</span></div>
    </div>
    ${failures.length > 0 && html`<ul class="list">${failures.slice(0, 20).map((f) => html`<li><div class="grow selectable"><div class="title">${baseName(f.Failure.source)}</div>
      <div class="caption danger">${t(f.Failure.errorKey || 'error.conversion_failed')}${f.Failure.errorDetail ? ' (' + f.Failure.errorDetail + ')' : ''}</div></div></li>`)}</ul>`}`}
  </${Card}>`;
}

export function WatchPage() {
  const s = store.settings;
  const presets = (store.library?.presets || []).filter((p) => p.kind === 'Convert');
  useEffect(() => { refresh('watch'); }, []);
  const save = (f) => guard(async () => { await saveSettings(f); await refresh('watch'); });
  const statuses = store.watch || [];
  const add = () => save((x) => {
    x.watchFolders.push({ id: Math.random().toString(16).slice(2, 10), enabled: true, folder: '', presetId: presets[0]?.id || '', outputFolder: '',
      originals: 'Keep', include: [], fileNamePattern: '', renames: [] });
  });
  return html`<div class="stack">
    <div class="row between"><div><h1 style="margin-bottom:4px">${t('watch.title')}</h1><p class="muted" style="white-space:pre-line">${t('watch.description')}</p></div>
      <${Button} kind="accent" icon="Plus" onClick=${add}>${t('watch.add')}</${Button}></div>
    ${s.watchFolders.length === 0 && html`<${Card}><p class="muted">${t('watch.empty')}</p></${Card}>`}
    ${s.watchFolders.map((r, i) => html`<${Rule} key=${r.id} rule=${r} status=${statuses.find((x) => x.rule.id === r.id)} presets=${presets}
      update=${(f) => save((x) => f(x.watchFolders[i]))} remove=${() => save((x) => { x.watchFolders.splice(i, 1); })} />`)}
    <p class="caption mono">${t('watch.cli_hint')}</p>
  </div>`;
}
