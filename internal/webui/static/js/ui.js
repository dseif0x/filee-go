// Controls shared by the pages: cards, fields, switches, choices, folder
// picker, dialogs and notices, styled like the original (Styles/Soft.axaml).
import { html, useState, useEffect, useRef, render } from '../vendor/preact-htm.js';
import { icons } from './icons.js';
import { api } from './api.js';
import { t, baseName } from './i18n.js';

export { html, useState, useEffect, useRef };

export function Icon({ name, size = 16, cls = '', title }) {
  const i = icons[name];
  if (!i) return null;
  return html`<svg class=${'icon ' + cls} width=${size} height=${size} viewBox="0 0 24 24" aria-hidden=${title ? undefined : 'true'}>
    ${title && html`<title>${title}</title>`}<path d=${i.d} fill-rule=${i.e ? 'evenodd' : undefined} /></svg>`;
}

export function Card({ title, hint, children, cls = '', actions }) {
  return html`<section class=${'card ' + cls}>
    ${(title || actions) && html`<div class="card-head">${title && html`<h2>${title}</h2>`}${actions && html`<div class="card-actions">${actions}</div>`}</div>`}
    ${hint && html`<p class="caption">${hint}</p>`}
    ${children}
  </section>`;
}

export function Field({ label, hint, error, children, cls = '' }) {
  return html`<label class=${'field ' + cls}>
    ${label && html`<span class="label">${label}</span>`}
    ${children}
    ${error ? html`<span class="caption danger">${error}</span>` : hint && html`<span class="caption">${hint}</span>`}
  </label>`;
}

export function Row({ children, cls = '' }) {
  return html`<div class=${'row ' + cls}>${children}</div>`;
}

export function Toggle({ label, checked, onChange, disabled, hint }) {
  return html`<div class="toggle-wrap"><label class=${'toggle' + (disabled ? ' disabled' : '')}>
    <input type="checkbox" role="switch" checked=${!!checked} disabled=${disabled} onChange=${(e) => onChange(e.currentTarget.checked)} />
    <span class="switch"><span class="knob"></span></span>
    <span class="toggle-label">${label}</span>
  </label>${hint && html`<p class="caption toggle-hint">${hint}</p>`}</div>`;
}

export function Check({ label, checked, onChange, disabled, hint }) {
  return html`<div class="check-wrap"><label class=${'check' + (disabled ? ' disabled' : '')}>
    <input type="checkbox" checked=${!!checked} disabled=${disabled} onChange=${(e) => onChange(e.currentTarget.checked)} />
    <span>${label}</span>
  </label>${hint && html`<p class="caption check-hint">${hint}</p>`}</div>`;
}

/** Select: options are {value, label, detail?} (value may be any JSON value). */
export function Select({ value, options, onChange, disabled, cls = '', label }) {
  const idx = options.findIndex((o) => o.value === value);
  return html`<div class=${'select ' + cls}><select aria-label=${label} disabled=${disabled} value=${String(Math.max(0, idx))}
      onChange=${(e) => onChange(options[+e.currentTarget.value].value)}>
    ${options.map((o, i) => html`<option value=${String(i)} selected=${i === idx}>${o.detail ? `${o.label}  ·  ${o.detail}` : o.label}</option>`)}
  </select></div>`;
}

/** TextInput commits on blur and Enter, so a save isn't sent per key. */
export function TextInput({ value, onChange, placeholder, type = 'text', cls = '', live, disabled, label, maxLength }) {
  const [v, setV] = useState(value ?? '');
  useEffect(() => setV(value ?? ''), [value]);
  const commit = () => { if ((value ?? '') !== v) onChange(v); };
  return html`<input class=${'input ' + cls} type=${type} value=${v} placeholder=${placeholder} disabled=${disabled}
    aria-label=${label} maxLength=${maxLength}
    onInput=${(e) => { setV(e.currentTarget.value); if (live) onChange(e.currentTarget.value); }}
    onBlur=${commit} onKeyDown=${(e) => { if (e.key === 'Enter') { commit(); } }} />`;
}

export function NumberInput({ value, onChange, min, max, step = 1, cls = '', disabled, label, allowEmpty }) {
  const [v, setV] = useState(value ?? '');
  useEffect(() => setV(value ?? ''), [value]);
  const commit = (raw) => {
    if (raw === '' && allowEmpty) { onChange(null); return; }
    let n = Number(raw);
    if (!Number.isFinite(n)) { setV(value ?? ''); return; }
    if (min !== undefined) n = Math.max(min, n);
    if (max !== undefined) n = Math.min(max, n);
    setV(n);
    if (n !== value) onChange(n);
  };
  return html`<span class=${'number ' + cls}>
    <input class="input" type="number" value=${v} min=${min} max=${max} step=${step} disabled=${disabled} aria-label=${label}
      onInput=${(e) => setV(e.currentTarget.value)} onBlur=${(e) => commit(e.currentTarget.value)}
      onKeyDown=${(e) => { if (e.key === 'Enter') commit(e.currentTarget.value); }} />
  </span>`;
}

export function Slider({ value, onChange, min = 0, max = 100, step = 1, label, valueText, disabled }) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  return html`<div class="slider">
    <div class="slider-head"><span class="label">${label}</span><span class="slider-value">${valueText ? valueText(v) : v}</span></div>
    <input type="range" min=${min} max=${max} step=${step} value=${v} disabled=${disabled} aria-label=${label}
      onInput=${(e) => setV(+e.currentTarget.value)} onChange=${(e) => onChange(+e.currentTarget.value)} />
  </div>`;
}

export function Button({ kind = '', onClick, icon, children, disabled, title, cls = '', type = 'button' }) {
  return html`<button type=${type} class=${'btn ' + kind + ' ' + cls} onClick=${onClick} disabled=${disabled} title=${title}>
    ${icon && html`<${Icon} name=${icon} size=${14} />`}${children && html`<span>${children}</span>`}
  </button>`;
}

export function IconButton({ icon, onClick, title, disabled, kind = 'ghost', size = 15 }) {
  return html`<button type="button" class=${'btn icon-only ' + kind} onClick=${onClick} title=${title} aria-label=${title} disabled=${disabled}>
    <${Icon} name=${icon} size=${size} /></button>`;
}

export function Segmented({ value, options, onChange }) {
  return html`<div class="segmented" role="radiogroup">
    ${options.map((o) => html`<button type="button" role="radio" aria-checked=${o.value === value} class=${o.value === value ? 'on' : ''}
      title=${o.title} onClick=${() => onChange(o.value)}>${o.icon && html`<${Icon} name=${o.icon} size=${14} />`}${o.label}</button>`)}
  </div>`;
}

/** FolderField is a path box with a Browse… button that opens the folder picker. */
export function FolderField({ value, onChange, placeholder, label, files }) {
  return html`<div class="folder-field">
    <${TextInput} value=${value} onChange=${onChange} placeholder=${placeholder} label=${label} />
    <${Button} onClick=${async () => {
      const p = files ? await pickFiles({ start: value, multiple: false }) : await pickFolder(value || placeholder);
      if (p) onChange(Array.isArray(p) ? p[0] : p);
    }}>${t('presets.browse')}</${Button}>
  </div>`;
}

export function Empty({ children, icon }) {
  return html`<div class="empty">${icon && html`<${Icon} name=${icon} size=${28} />`}<p>${children}</p></div>`;
}

export function Spinner({ size = 16 }) {
  return html`<span class="spinner" style=${`width:${size}px;height:${size}px`}></span>`;
}

export function Progress({ value }) {
  return html`<div class="progress"><div class="bar" style=${`width:${Math.round(Math.max(0, Math.min(1, value)) * 100)}%`}></div></div>`;
}

// ---- dialogs ----

let dialogHost = null;
function host() {
  if (!dialogHost) {
    dialogHost = document.createElement('div');
    dialogHost.className = 'dialog-host';
    document.body.appendChild(dialogHost);
  }
  return dialogHost;
}

/** showDialog renders body(close) in a modal and resolves with what close is given. */
export function showDialog(body, { wide } = {}) {
  return new Promise((resolve) => {
    const el = document.createElement('div');
    host().appendChild(el);
    const close = (v) => { render(null, el); el.remove(); resolve(v); };
    const onKey = (e) => { if (e.key === 'Escape') { e.stopPropagation(); document.removeEventListener('keydown', onKey, true); close(undefined); } };
    document.addEventListener('keydown', onKey, true);
    const done = (v) => { document.removeEventListener('keydown', onKey, true); close(v); };
    render(html`<div class="modal-backdrop" onMouseDown=${(e) => { if (e.target === e.currentTarget) done(undefined); }}>
      <div class=${'modal' + (wide ? ' wide' : '')} role="dialog" aria-modal="true">${body(done)}</div></div>`, el);
    setTimeout(() => el.querySelector('input,select,button.accent,button')?.focus(), 20);
  });
}

export function confirmDialog(text, { ok = t('common.ok'), danger, title } = {}) {
  return showDialog((close) => html`
    ${title && html`<h2>${title}</h2>`}
    <p class="dialog-text">${text}</p>
    <div class="dialog-buttons">
      <${Button} onClick=${() => close(false)}>${t('common.cancel')}</${Button}>
      <${Button} kind=${danger ? 'danger' : 'accent'} onClick=${() => close(true)}>${ok}</${Button}>
    </div>`);
}

export function promptDialog(title, value = '', { placeholder } = {}) {
  let v = value;
  return showDialog((close) => html`
    <h2>${title}</h2>
    <input class="input wide" value=${value} placeholder=${placeholder} onInput=${(e) => { v = e.currentTarget.value; }}
      onKeyDown=${(e) => { if (e.key === 'Enter') close(v); }} />
    <div class="dialog-buttons">
      <${Button} onClick=${() => close(undefined)}>${t('common.cancel')}</${Button}>
      <${Button} kind="accent" onClick=${() => close(v)}>${t('common.ok')}</${Button}>
    </div>`);
}

function Browser({ start, files, multiple, close, title, filter }) {
  const [listing, setListing] = useState(null);
  const [error, setError] = useState('');
  const [picked, setPicked] = useState([]);
  const [path, setPath] = useState(start || '');
  const go = async (p) => {
    try {
      const l = await api.get('api/fs?files=' + (files ? 1 : 0) + '&path=' + encodeURIComponent(p || ''));
      setListing(l); setPath(l.path); setError(''); setPicked([]);
    } catch (e) {
      if (p) go(''); else setError(e.message);
    }
  };
  useEffect(() => { go(start || ''); }, []);
  const sep = listing?.path?.includes('\\') ? '\\' : '/';
  const join = (name) => (listing.path.endsWith(sep) ? listing.path + name : listing.path + sep + name);
  const shown = (listing?.files || []).filter((f) => !filter || filter(f));
  return html`<div class="browser">
    <h2>${title}</h2>
    <div class="browser-path">
      <${IconButton} icon="ArrowUp" title="Up" disabled=${!listing?.parent} onClick=${() => go(listing.parent)} />
      <input class="input" value=${path} onInput=${(e) => setPath(e.currentTarget.value)} onKeyDown=${(e) => { if (e.key === 'Enter') go(path); }} />
    </div>
    <div class="browser-body">
      <ul class="browser-places">${(listing?.places || []).map((p) => html`<li><button type="button" class=${p === listing.path ? 'on' : ''} onClick=${() => go(p)}>
        <${Icon} name=${p === listing.home ? 'Home' : 'Folder'} size=${14} />${baseName(p) || p}</button></li>`)}</ul>
      <ul class="browser-list">
        ${error && html`<li class="danger">${error}</li>`}
        ${(listing?.folders || []).map((f) => html`<li><button type="button" onDblClick=${() => go(join(f))} onClick=${() => !files && setPath(join(f))}>
          <${Icon} name="Folder" size=${15} />${f}</button></li>`)}
        ${shown.map((f) => {
          const full = join(f);
          const on = picked.includes(full);
          return html`<li><button type="button" class=${on ? 'on' : ''} onDblClick=${() => close(multiple ? [full] : full)}
            onClick=${(e) => setPicked(multiple && (e.metaKey || e.ctrlKey) ? (on ? picked.filter((x) => x !== full) : [...picked, full]) : [full])}>
            <${Icon} name="File" size=${15} />${f}</button></li>`;
        })}
      </ul>
    </div>
    <div class="dialog-buttons">
      <${Button} onClick=${() => close(undefined)}>${t('common.cancel')}</${Button}>
      <${Button} kind="accent" disabled=${files && picked.length === 0}
        onClick=${() => close(files ? (multiple ? picked : picked[0]) : path)}>${t('common.ok')}</${Button}>
    </div>
  </div>`;
}

/** pickFolder lets the user choose a folder on this computer (the page can't open the system's dialog). */
export function pickFolder(start, title = t('ask.folder_title')) {
  return showDialog((close) => html`<${Browser} start=${start} close=${close} title=${title} />`, { wide: true });
}

export function pickFiles({ start, multiple = true, title = t('home.drop_select'), filter } = {}) {
  return showDialog((close) => html`<${Browser} start=${start} files=${true} multiple=${multiple} close=${close} title=${title} filter=${filter} />`, { wide: true });
}

// ---- notices ----

let noticeHost = null;
export function notify(text, kind = '') {
  if (!noticeHost) {
    noticeHost = document.createElement('div');
    noticeHost.className = 'notices';
    document.body.appendChild(noticeHost);
  }
  const el = document.createElement('div');
  el.className = 'notice ' + kind;
  el.textContent = text;
  noticeHost.appendChild(el);
  setTimeout(() => el.classList.add('out'), 4200);
  setTimeout(() => el.remove(), 4700);
}

/** guard runs an action and shows its error as a notice. */
export async function guard(fn) {
  try { return await fn(); } catch (e) { notify(e.message || String(e), 'danger'); return undefined; }
}

/** TagEditor: tags with an × each and a box that adds what is typed on Enter or comma. */
export function TagEditor({ tags, onChange, placeholder, normalize = (s) => s.trim(), suggestions = [] }) {
  const [text, setText] = useState('');
  const add = (raw) => {
    const parts = raw.split(/[,;\s]+/).map(normalize).filter(Boolean);
    const next = [...tags];
    for (const p of parts) if (!next.some((x) => x.toLowerCase() === p.toLowerCase())) next.push(p);
    if (next.length !== tags.length) onChange(next);
    setText('');
  };
  const listId = suggestions.length ? 'tags-' + Math.random().toString(36).slice(2, 8) : undefined;
  return html`<div class="tag-box">
    ${tags.map((tag) => html`<span class="tag">${tag}<button type="button" title=${t('tags.remove')} aria-label=${t('tags.remove') + ' ' + tag}
      onClick=${() => onChange(tags.filter((x) => x !== tag))}>×</button></span>`)}
    <input class="tag-input" value=${text} placeholder=${tags.length ? '' : placeholder} list=${listId}
      onInput=${(e) => { const v = e.currentTarget.value; if (/[,;]$/.test(v)) add(v); else setText(v); }}
      onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); add(text); } else if (e.key === 'Backspace' && !text && tags.length) onChange(tags.slice(0, -1)); }}
      onBlur=${() => text && add(text)} />
    ${listId && html`<datalist id=${listId}>${suggestions.map((s) => html`<option value=${s} />`)}</datalist>`}
  </div>`;
}
