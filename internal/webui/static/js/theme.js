// Applies the theme settings (mode, style, accent, corner radius, UI scale)
// and the user's theme.json colours as CSS variables (ThemeService).
import { api } from './api.js';

export const swatches = ['#7F77DD', '#1D9E75', '#D85A30', '#378ADD', '#D4537E', '#BA7517', '#639922', '#5F5E5A'];

export const themePresets = [
  { key: 'monochrome', light: '#1F1E1D', dark: '#F1EFE8' },
  { key: 'lavender', light: '#7F77DD', dark: '#BCAEE5' },
  { key: 'peach', light: '#E07A70', dark: '#F29B91' },
  { key: 'mint', light: '#529B7D', dark: '#88D5B1' },
  { key: 'rose', light: '#E86483', dark: '#F79BB0' },
  { key: 'sky', light: '#5B8BE3', dark: '#98B8F6' },
  { key: 'amber', light: '#CB852A', dark: '#F0BA6A' },
  { key: 'sage', light: '#5A8E51', dark: '#8DC983' },
];

const cssNames = {
  page: '--page', surface: '--surface', surfaceAlt: '--surface-alt', border: '--border', text: '--text',
  textSecondary: '--text-2', danger: '--danger', success: '--success', warning: '--warning',
};

let custom = null;
let customLoaded = false;
const media = window.matchMedia('(prefers-color-scheme: dark)');
let last = null;

export async function loadCustomTheme() {
  try { custom = (await api.get('api/theme')).theme; } catch { custom = null; }
  customLoaded = true;
  if (last) applyTheme(last);
}

export function customThemeInfo() { return { custom, loaded: customLoaded }; }

function hexToRgb(hex) {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex || '');
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function mix(a, b, w) { return a.map((v, i) => Math.round(v * (1 - w) + b[i] * w)); }
const rgb = (c) => `rgb(${c[0]} ${c[1]} ${c[2]})`;
const lum = (c) => { const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; }; return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2]); };

export function isDark(settings) {
  const mode = settings?.theme?.mode || 'System';
  return mode === 'Dark' || (mode === 'System' && media.matches);
}

export function applyTheme(settings) {
  last = settings;
  if (!settings) return;
  const th = settings.theme || {};
  const dark = isDark(settings);
  const root = document.documentElement;
  root.dataset.theme = dark ? 'dark' : 'light';
  root.dataset.style = th.style === 'MaterialYou' ? 'material' : 'soft';
  const preset = themePresets.find((p) => p.key === th.presetKey);
  const accentHex = preset ? (dark ? preset.dark : preset.light) : th.accent || '#7F77DD';
  const accent = hexToRgb(accentHex) || [127, 119, 221];
  const s = root.style;
  s.setProperty('--accent', rgb(accent));
  s.setProperty('--accent-soft', `rgb(${accent.join(' ')} / 0.18)`);
  s.setProperty('--accent-faint', `rgb(${accent.join(' ')} / 0.09)`);
  s.setProperty('--accent-strong', rgb(mix(accent, dark ? [255, 255, 255] : [0, 0, 0], 0.25)));
  s.setProperty('--on-accent', lum(accent) > 0.45 ? '#1F1E1D' : '#FFFFFF');
  const r = Math.max(0, Math.min(28, th.cornerRadius ?? 16));
  s.setProperty('--radius', r + 'px');
  s.setProperty('--radius-sm', Math.round(r * 0.55) + 'px');
  // Material You: surfaces tinted with the accent.
  for (const k of Object.values(cssNames)) s.removeProperty(k);
  if (th.style === 'MaterialYou') {
    const base = dark ? [27, 27, 25] : [250, 249, 252];
    s.setProperty('--page', rgb(mix(base, accent, dark ? 0.06 : 0.05)));
    s.setProperty('--surface', rgb(mix(dark ? [36, 36, 34] : [255, 255, 255], accent, dark ? 0.09 : 0.07)));
    s.setProperty('--surface-alt', rgb(mix(dark ? [46, 46, 43] : [240, 238, 245], accent, dark ? 0.14 : 0.12)));
  }
  const variant = custom && (dark ? custom.dark : custom.light);
  if (variant) {
    for (const [k, v] of Object.entries(variant)) {
      const name = cssNames[k];
      if (!name) continue;
      // #AARRGGBB → rgba.
      if (v.length === 9) {
        const a = parseInt(v.slice(1, 3), 16) / 255;
        const c = hexToRgb('#' + v.slice(3));
        s.setProperty(name, `rgb(${c.join(' ')} / ${a.toFixed(3)})`);
      } else s.setProperty(name, v);
    }
  }
  const reduce = th.reduceMotion;
  root.dataset.motion = reduce === true ? 'reduce' : reduce === false ? 'full' : 'system';
  const scale = Math.min(1.8, Math.max(0.7, settings.uiScale || 1));
  s.setProperty('zoom', String(scale));
}

media.addEventListener?.('change', () => applyTheme(last));
