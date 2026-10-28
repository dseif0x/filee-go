// Theme: style, appearance, accent (swatches, custom colour, pastel pairs),
// softness, donut opacity, reduced motion, window size and a preview (ThemePage.axaml).
import { html, useState, Card, Select, Slider, Check, Toggle, Button, Field, guard } from '../ui.js';
import { store, saveSettings } from '../api.js';
import { t } from '../i18n.js';
import { swatches, themePresets, isDark, customThemeInfo } from '../theme.js';
import { DonutPreview } from './toolbar.js';

export function ThemePage() {
  const s = store.settings;
  const th = s.theme;
  const save = (f) => guard(() => saveSettings(f));
  const setTheme = (f) => save((x) => f(x.theme));
  const [hex, setHex] = useState(th.accent);
  const dark = isDark(s);
  const [sample, setSample] = useState(true);
  const custom = customThemeInfo();
  const reduce = th.reduceMotion === undefined || th.reduceMotion === null ? window.matchMedia('(prefers-reduced-motion: reduce)').matches : th.reduceMotion;
  const zoom = (d) => save((x) => { x.uiScale = d === 0 ? 1 : Math.round(Math.min(1.8, Math.max(0.7, (x.uiScale || 1) + d)) * 10) / 10; });
  const mod = store.boot.os === 'darwin' ? '⌘' : 'Ctrl';

  return html`<div class="stack">
    <h1>${t('theme.title')}</h1>
    <${Card} title=${t('theme.style')}>
      <div class="grid2">
        ${['Soft', 'MaterialYou'].map((v) => html`<button type="button" class=${'card flat style-choice' + (th.style === v ? ' on' : '')}
            onClick=${() => setTheme((x) => { x.style = v; })} aria-pressed=${th.style === v}>
          <div class="row"><strong>${t('theme.style.' + v)}</strong>${v === 'MaterialYou' && html`<span class="pill accent">${t('theme.style.m3_tag')}</span>`}</div>
          <span class="caption">${t(v === 'Soft' ? 'theme.style.soft_hint' : 'theme.style.material_you_hint')}</span>
        </button>`)}
      </div>
    </${Card}>

    <${Card}>
      <${Field} label=${t('theme.mode')}>
        <${Select} label=${t('theme.mode')} value=${th.mode} options=${['System', 'Light', 'Dark'].map((v) => ({ value: v, label: t('theme.mode.' + v) }))}
          onChange=${(v) => setTheme((x) => { x.mode = v; })} />
      </${Field}>
    </${Card}>

    <${Card} title=${t('theme.accent')}>
      <span class="label">${t('theme.presets_vibrant')}</span>
      <div class="swatches">
        ${swatches.map((c) => html`<button type="button" class=${'swatch' + (!th.presetKey && th.accent.toUpperCase() === c ? ' on' : '')} style=${'background:' + c}
          title=${c} aria-label=${c} onClick=${() => { setHex(c); setTheme((x) => { x.accent = c; x.presetKey = ''; }); }}></button>`)}
      </div>
      <span class="label">${t('theme.custom_color')}</span>
      <div class="row">
        <input type="color" value=${th.accent} aria-label=${t('theme.custom_color')}
          onChange=${(e) => { const v = e.currentTarget.value.toUpperCase(); setHex(v); setTheme((x) => { x.accent = v; x.presetKey = ''; }); }} />
        <input class="input mono" style="width:110px" value=${hex} onInput=${(e) => setHex(e.currentTarget.value)}
          onBlur=${() => { if (/^#[0-9a-f]{6}$/i.test(hex)) setTheme((x) => { x.accent = hex.toUpperCase(); x.presetKey = ''; }); else setHex(th.accent); }} />
      </div>
      <span class="label">${t('theme.presets_pastel')}</span>
      <div class="row">
        ${themePresets.map((p) => html`<button type="button" class=${'theme-preset' + (th.presetKey === p.key ? ' on' : '')}
            onClick=${() => setTheme((x) => { x.presetKey = p.key; x.accent = p.light; })}>
          <span class="dots"><span style=${'background:' + p.light}></span><span style=${'background:' + p.dark}></span></span>
          ${t('theme.preset.' + p.key)}</button>`)}
      </div>
    </${Card}>

    <${Card}>
      <${Slider} label=${t('theme.corner_radius')} min=${0} max=${28} value=${th.cornerRadius} onChange=${(v) => setTheme((x) => { x.cornerRadius = v; })} />
      <${Slider} label=${t('theme.donut_opacity')} min=${Math.round(store.boot.limits.minDonutOpacity * 100)} max=${100} value=${Math.round(s.donut.opacity * 100)}
        valueText=${(v) => v + ' %'} onChange=${(v) => save((x) => { x.donut.opacity = v / 100; })} />
      <${Check} label=${t('theme.reduce_motion')} hint=${t('theme.reduce_motion_hint')} checked=${reduce} onChange=${(v) => setTheme((x) => { x.reduceMotion = v; })} />
      <div>
        <span class="label">${t('theme.window_size')}</span>
        <div class="row">
          <${Button} onClick=${() => zoom(-0.1)}>${t('theme.window_size_smaller')}</${Button}>
          <span class="time">${Math.round((s.uiScale || 1) * 100)} %</span>
          <${Button} onClick=${() => zoom(0.1)}>${t('theme.window_size_larger')}</${Button}>
          <${Button} kind="ghost" onClick=${() => zoom(0)}>${t('theme.window_size_reset')}</${Button}>
        </div>
        <p class="caption">${t('theme.window_size_hint', mod)}</p>
      </div>
      ${custom.custom && html`<p class="caption">theme.json: ${Object.keys((dark ? custom.custom.dark : custom.custom.light) || {}).join(', ')}</p>`}
    </${Card}>

    <${Card} title=${t('theme.preview')}>
      <div class="preview-card">
        <${DonutPreview} size=${220} />
        <div class="stack tight">
          <div class="row"><${Button} kind="accent">${t('common.save')}</${Button}><${Button}>${t('common.cancel')}</${Button}></div>
          <${Toggle} label=${t('theme.preview_toggle')} hint=${t('theme.preview_toggle_hint')} checked=${sample} onChange=${setSample} />
        </div>
      </div>
    </${Card}>
  </div>`;
}
