// Texts by key from internal/i18n ({0} {1} … for arguments), and the names
// presets, profiles and formats are shown with (as internal/i18n names them).
import { store } from './api.js';

export function t(key, ...args) {
  const texts = store.boot?.texts || {};
  let s = texts[key];
  if (s === undefined) s = key;
  if (!args.length) return s;
  return s.replace(/\{(\d+)\}/g, (m, i) => (i < args.length ? String(args[i]) : m));
}

/** has: the key has a text. */
export const has = (key) => store.boot?.texts && key in store.boot.texts;

export function presetName(p) {
  if (!p) return '';
  if (p.name && p.name.trim()) return p.name;
  if (p.nameKey) return t(p.nameKey);
  return (p.targetFormat || '').toUpperCase();
}

function variant(p, suffix) {
  if ((p.name && p.name.trim()) || !p.nameKey) return presetName(p);
  const k = p.nameKey + suffix;
  return has(k) ? t(k) : presetName(p);
}
export const donutName = (p) => variant(p, '.donut');
export const ringName = (p) => variant(p, '.short');

export function profileName(p) {
  if (!p) return '';
  if (p.name && p.name.trim()) return p.name;
  if (p.nameKey) return t(p.nameKey);
  return p.id;
}

export function format(id) {
  return (store.boot?.formats || []).find((f) => f.id === id);
}

export function formatName(id) {
  if (id === 'same') return t('donut.same_format');
  if (id === 'group') return t('group.editor_title');
  if (id === 'folder') return t('format.folder');
  const f = format(id);
  return f ? f.name : (id || '').toUpperCase();
}

export const categoryOf = (id) => format(id)?.category;
export const categoryName = (c) => t('format.category.' + c);

/** formatFile is a file size for people. */
export function fileSize(bytes) {
  if (bytes == null) return '';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = bytes;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return (i === 0 ? v : v.toFixed(v < 10 ? 1 : 0)) + ' ' + u[i];
}

export const baseName = (p) => (p || '').split(/[\\/]/).pop();
export const dirName = (p) => {
  const i = Math.max((p || '').lastIndexOf('/'), (p || '').lastIndexOf('\\'));
  return i > 0 ? p.slice(0, i) : p;
};
export const extOf = (p) => { const b = baseName(p); const i = b.lastIndexOf('.'); return i > 0 ? b.slice(i + 1).toLowerCase() : ''; };

/** modifierLabel is how a platform writes the modifier keys ("⌥ Option" on a Mac). */
export function modifierLabel(mods) {
  const mac = store.boot?.os === 'darwin';
  const names = mac ? { Ctrl: '⌃ Control', Alt: '⌥ Option', Shift: '⇧ Shift', Meta: '⌘ Command' }
    : { Ctrl: 'Ctrl', Alt: 'Alt', Shift: 'Shift', Meta: store.boot?.os === 'windows' ? 'Win' : 'Super' };
  if (!mods || mods === 'None') return '';
  return mods.split(',').map((m) => names[m.trim()] || m.trim()).join(' + ');
}
