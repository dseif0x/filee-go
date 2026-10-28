// Shortcuts: the gestures that open the donut, flipping it, and the apps
// gestures never trigger in (TriggersPage.axaml).
import { html, useState, useEffect, Card, Toggle, Check, Select, Slider, Button, IconButton, TagEditor, guard } from '../ui.js';
import { store, saveSettings } from '../api.js';
import { t } from '../i18n.js';

const MODS = ['Ctrl', 'Alt', 'Shift', 'Meta'];

function modsOf(s) { return (s || 'None').split(',').map((x) => x.trim()).filter((x) => MODS.includes(x)); }
function modsString(list) { return list.length ? MODS.filter((m) => list.includes(m)).join(', ') : 'None'; }

function modLabel(m) {
  const mac = store.boot.os === 'darwin';
  return { Ctrl: mac ? '⌃ Ctrl' : 'Ctrl', Alt: mac ? '⌥ Option' : 'Alt', Shift: '⇧ Shift', Meta: mac ? '⌘ Cmd' : 'Win' }[m];
}

function modSymbols(list) {
  const mac = store.boot.os === 'darwin';
  const parts = list.map((m) => ({ Ctrl: mac ? '⌃' : 'Ctrl', Alt: mac ? '⌥ Option' : 'Alt', Shift: 'Shift', Meta: mac ? '⌘' : 'Win' }[m]));
  return parts.length ? parts.join(' + ') : '—';
}

/** describe is GestureText.Describe. */
export function describeGesture(g) {
  const mods = modsOf(g.modifiers);
  if (g.kind === 'KeyChord') return mods.length ? `${modSymbols(mods)} + ${g.key || '?'}` : g.key || '—';
  return `${modSymbols(mods)} + ${t('triggers.button.' + g.button)}`;
}

/** keyName turns a browser key event into the key names the settings use (Avalonia's). */
export function keyName(e) {
  const c = e.code || '';
  if (/^Key[A-Z]$/.test(c)) return c.slice(3);
  if (/^Digit\d$/.test(c)) return 'D' + c.slice(5);
  if (/^F\d{1,2}$/.test(c)) return c;
  if (/^Numpad\d$/.test(c)) return 'NumPad' + c.slice(6);
  const map = { Space: 'Space', Tab: 'Tab', Enter: 'Enter', Escape: 'Escape', Backspace: 'Back', ArrowLeft: 'Left', ArrowRight: 'Right',
    ArrowUp: 'Up', ArrowDown: 'Down', Insert: 'Insert', Delete: 'Delete', Home: 'Home', End: 'End', PageUp: 'PageUp', PageDown: 'PageDown',
    Backquote: 'OemTilde', Minus: 'OemMinus', Equal: 'OemPlus', BracketLeft: 'OemOpenBrackets', BracketRight: 'OemCloseBrackets',
    Semicolon: 'OemSemicolon', Quote: 'OemQuotes', Comma: 'OemComma', Period: 'OemPeriod', Slash: 'OemQuestion', Backslash: 'OemPipe' };
  if (c === 'Backspace') return 'Backspace';
  return map[c] || '';
}

const isModifier = (e) => ['Shift', 'Control', 'Alt', 'Meta', 'OS'].includes(e.key);

function KeyRecorder({ value, onChange, recordingText, withModifiers, onModifiers }) {
  const [rec, setRec] = useState(false);
  useEffect(() => {
    if (!rec) return undefined;
    const on = (e) => {
      e.preventDefault(); e.stopPropagation();
      if (isModifier(e)) return;
      const k = keyName(e);
      if (!k) return;
      if (withModifiers && onModifiers) {
        const mods = [];
        if (e.ctrlKey) mods.push('Ctrl'); if (e.altKey) mods.push('Alt'); if (e.shiftKey) mods.push('Shift'); if (e.metaKey) mods.push('Meta');
        onModifiers(mods);
      }
      onChange(k);
      setRec(false);
    };
    window.addEventListener('keydown', on, true);
    return () => window.removeEventListener('keydown', on, true);
  }, [rec]);
  return html`<div class="row tight">
    <span class="pill" style="min-width:70px;justify-content:center">${rec ? recordingText : value || '—'}</span>
    <${Button} kind=${rec ? 'accent' : ''} onClick=${() => setRec(!rec)}><span class="danger">●</span> ${t('triggers.record')}</${Button}>
    <${Button} kind="ghost" onClick=${() => onChange('')}>${t('triggers.clear')}</${Button}>
  </div>`;
}

function Gesture({ g, update, remove }) {
  const mods = modsOf(g.modifiers);
  const os = store.boot.os;
  const scopeLabel = (v) => t(v === 'FileManager' && os === 'darwin' ? 'triggers.scope.FileManager_macos' : v === 'FileManager' && os === 'linux' ? 'triggers.scope.FileManager_linux' : 'triggers.scope.' + v);
  const usesMouse = g.kind !== 'KeyChord';
  return html`<${Card}>
    <div class="row between">
      <div class="row"><${Toggle} label="" checked=${g.enabled} onChange=${(v) => update((x) => { x.enabled = v; })} />
        <strong class="accent-text">${describeGesture(g)}</strong></div>
      <${Button} kind="ghost danger" icon="Delete" onClick=${remove}>${t('triggers.remove')}</${Button}>
    </div>
    <div class="grid2">
      <div><span class="label">${t('triggers.kind')}</span>
        <${Select} label=${t('triggers.kind')} value=${g.kind} options=${['Drag', 'Hold', 'KeyChord'].map((v) => ({ value: v, label: t('triggers.kind.' + v) }))}
          onChange=${(v) => update((x) => { x.kind = v; if (v === 'KeyChord') { x.button = 'None'; if (!x.key) x.key = 'Space'; } else if (x.button === 'None') x.button = 'Left'; })} /></div>
      <div><span class="label">${t('triggers.scope')}</span>
        <${Select} label=${t('triggers.scope')} value=${g.scope} options=${['FileManager', 'Anywhere'].map((v) => ({ value: v, label: scopeLabel(v) }))}
          onChange=${(v) => update((x) => { x.scope = v; })} /></div>
      <div><span class="label">${t('triggers.modifiers')}</span>
        <div class="segmented">${MODS.map((m) => html`<button type="button" class=${mods.includes(m) ? 'on' : ''} aria-pressed=${mods.includes(m)}
          onClick=${() => update((x) => { const l = modsOf(x.modifiers); x.modifiers = modsString(l.includes(m) ? l.filter((y) => y !== m) : [...l, m]); })}>${modLabel(m)}</button>`)}</div></div>
      ${usesMouse ? html`<div><span class="label">${t('triggers.button')}</span>
        <${Select} label=${t('triggers.button')} value=${g.button} options=${['Left', 'Right', 'Middle', 'X1', 'X2'].map((v) => ({ value: v, label: t('triggers.button.' + v) }))}
          onChange=${(v) => update((x) => { x.button = v; })} /></div>`
        : html`<div><span class="label">${t('triggers.key')}</span>
        <${KeyRecorder} value=${g.key} recordingText=${t('triggers.recording')} withModifiers=${true}
          onModifiers=${(m) => update((x) => { x.modifiers = modsString(m); })} onChange=${(k) => update((x) => { x.key = k; })} /></div>`}
    </div>
    ${g.kind === 'Drag' && html`<${Slider} label=${t('triggers.threshold')} min=${0} max=${80} value=${g.dragThreshold}
      valueText=${(v) => (v === 0 ? t('triggers.threshold_system') : v + 'px')} onChange=${(v) => update((x) => { x.dragThreshold = v; })} />`}
    ${g.kind === 'Hold' && html`<${Slider} label=${t('triggers.hold_ms')} min=${150} max=${2000} step=${50} value=${g.holdMilliseconds}
      valueText=${(v) => v + ' ms'} onChange=${(v) => update((x) => { x.holdMilliseconds = v; })} />`}
  </${Card}>`;
}

export function TriggersPage() {
  const s = store.settings;
  const save = (f) => guard(() => saveSettings(f));
  const os = store.boot.os;
  const notice = os === 'darwin' ? t('triggers.macos_permissions') : os === 'linux' ? t('triggers.linux_x11') : '';
  const gestures = s.triggers;
  const flipKey = s.donut.flipKey;
  const reserved = store.boot.reservedFlipKeys;
  const flipError = flipKey && (reserved.some((r) => r.toLowerCase() === flipKey.toLowerCase())
    || gestures.some((g) => g.kind === 'KeyChord' && (g.key || '').toLowerCase() === flipKey.toLowerCase())) ? t('triggers.flip_key_taken', flipKey) : '';
  const newGesture = () => ({ id: Math.random().toString(16).slice(2, 10), enabled: true, kind: 'Drag', modifiers: os === 'darwin' ? 'Alt' : 'Ctrl', button: 'Left', dragThreshold: 0, holdMilliseconds: 450, scope: 'FileManager' });
  return html`<div class="stack">
    <div class="row between"><div><h1 style="margin-bottom:4px">${t('triggers.title')}</h1><p class="muted">${t('triggers.description')}</p></div>
      <${Button} kind="accent" icon="Plus" onClick=${() => save((x) => { x.triggers.push(newGesture()); })}>${t('triggers.add')}</${Button}></div>
    ${notice && html`<p class="caption">${notice}</p>`}
    ${s.paused && html`<p class="warning">${t('triggers.paused')}</p>`}
    ${!store.boot.supportsGestures && html`<p class="caption">${t('triggers.hook_stopped')}</p>`}
    ${gestures.map((g, i) => html`<${Gesture} key=${g.id} g=${g}
      update=${(f) => save((x) => f(x.triggers[i]))} remove=${() => save((x) => { x.triggers.splice(i, 1); })} />`)}

    <${Card} title=${t('triggers.flip_title')} hint=${t('triggers.flip_description')}>
      <div class="row"><span class="label" style="margin:0">${t('triggers.key')}</span>
        <${KeyRecorder} value=${flipKey} recordingText=${t('triggers.recording_key')} onChange=${(k) => save((x) => { x.donut.flipKey = k; })} />
        <${Button} kind="ghost" onClick=${() => save((x) => { x.donut.flipKey = 'Tab'; })}>${t('triggers.flip_reset')}</${Button}></div>
      ${flipError && html`<p class="caption danger">${flipError}</p>`}
      <${Check} label=${t('triggers.flip_wheel')} checked=${s.donut.flipWithWheel} onChange=${(v) => save((x) => { x.donut.flipWithWheel = v; })} />
    </${Card}>

    <${Card} title=${t('triggers.excluded')}>
      <${TagEditor} tags=${s.excludedProcesses} placeholder=${t('triggers.excluded_hint')} normalize=${(v) => v.trim().toLowerCase().replace(/\.exe$/, '')}
        onChange=${(tags) => save((x) => { x.excludedProcesses = tags; })} />
      <p class="caption">${t('triggers.excluded_note')}</p>
    </${Card}>
  </div>`;
}
