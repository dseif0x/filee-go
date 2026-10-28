// General: language, start-up, closing, default save location, browser
// drops, history, library and the data folder (GeneralPage.axaml).
import { html, useState, Card, Toggle, Check, Select, Field, TextInput, NumberInput, FolderField, Button, confirmDialog, guard, notify } from '../ui.js';
import { store, saveSettings, api, refresh, openPath } from '../api.js';
import { t } from '../i18n.js';

export function languageName(code) {
  try {
    const n = new Intl.DisplayNames([code], { type: 'language' }).of(code);
    return n ? n.charAt(0).toUpperCase() + n.slice(1) : code;
  } catch { return code; }
}

export function GeneralPage() {
  const s = store.settings;
  const boot = store.boot;
  const [libraryMessage, setLibraryMessage] = useState('');
  const save = (f) => guard(() => saveSettings(f));
  if (!store.history) refresh('history');
  const os = boot.os;
  const contextText = t(os === 'darwin' ? 'general.context_menu_macos' : os === 'linux' ? 'general.context_menu_linux' : 'general.context_menu');
  const integration = os === 'darwin' ? t('general.integration_macos') : os === 'linux' ? t('general.integration_linux') : '';
  const languages = [{ value: 'auto', label: t('general.language_auto') }, ...boot.languages.map((c) => ({ value: c, label: languageName(c) }))];
  const loc = s.defaultOutput.location;

  const exportLibrary = async () => {
    const r = await fetch('api/library/export');
    if (!r.ok) { notify((await r.json()).error, 'danger'); return; }
    const blob = await r.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = 'Filee library.json';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 2000);
  };
  const importLibrary = () => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json,application/json';
    input.onchange = async () => {
      const f = input.files?.[0];
      if (!f) return;
      const r = await guard(() => api.raw('POST', 'api/library/import', f).then((x) => x.json()));
      if (r) setLibraryMessage(t('general.imported', r.imported));
    };
    input.click();
  };
  const reset = async () => {
    if (await confirmDialog(t('general.reset_confirm'), { danger: true, ok: t('general.reset_library') })) {
      await guard(() => api.post('api/library/reset'));
      setLibraryMessage('');
    }
  };
  const clearHistory = async () => {
    await guard(() => api.del('api/history'));
    notify(t('general.history_cleared'));
  };

  return html`<div class="stack">
    <h1>${t('general.title')}</h1>
    <${Card}>
      <${Field} label=${t('general.language')}>
        <${Select} label=${t('general.language')} value=${s.language} options=${languages} onChange=${(v) => save((x) => { x.language = v; })} cls="narrow" />
      </${Field}>
      <${Toggle} label=${t('general.start_with_system')} checked=${s.startWithSystem} onChange=${(v) => save((x) => { x.startWithSystem = v; })} />
      ${boot.supportsDock && html`
        <${Toggle} label=${t('general.show_in_dock')} checked=${s.showInDock} onChange=${(v) => save((x) => { x.showInDock = v; })} />
        <${Toggle} label=${t('general.show_menu_bar_icon')} checked=${s.showMenuBarIcon} onChange=${(v) => save((x) => { x.showMenuBarIcon = v; })} />`}
      <${Field} label=${t('general.close_action')}>
        <${Select} label=${t('general.close_action')} value=${s.closeAction}
          options=${['Ask', 'Tray', 'Quit'].map((v) => ({ value: v, label: t('general.close.' + v) }))} onChange=${(v) => save((x) => { x.closeAction = v; })} />
      </${Field}>
      <${Toggle} label=${contextText} checked=${s.contextMenuEnabled} onChange=${(v) => save((x) => { x.contextMenuEnabled = v; })} hint=${integration} />
    </${Card}>

    <${Card} title=${t('general.default_output')} hint=${t('general.default_output_hint')}>
      <${Select} label=${t('general.default_output')} value=${loc}
        options=${['SameFolder', 'Subfolder', 'CustomFolder'].map((v) => ({ value: v, label: t('presets.location.' + v) }))}
        onChange=${(v) => save((x) => { x.defaultOutput.location = v; })} />
      ${loc === 'Subfolder' && html`<${Field} label=${t('presets.subfolder')}>
        <${TextInput} value=${s.defaultOutput.subfolderName} placeholder="converted" onChange=${(v) => save((x) => { x.defaultOutput.subfolderName = v.trim() || 'converted'; })} />
      </${Field}>`}
      ${loc === 'CustomFolder' && html`<${Field} label=${t('presets.custom_folder')}>
        <${FolderField} value=${s.defaultOutput.customFolder} onChange=${(v) => save((x) => { x.defaultOutput.customFolder = v.trim(); })} />
      </${Field}>`}
      <${Toggle} label=${t('general.keep_dates')} hint=${t('general.keep_dates_hint')} checked=${s.keepFileDates} onChange=${(v) => save((x) => { x.keepFileDates = v; })} />
      <${Toggle} label=${t('general.zip_geo')} hint=${t('general.zip_geo_hint')} checked=${s.zipGeoResults} onChange=${(v) => save((x) => { x.zipGeoResults = v; })} />
    </${Card}>

    <${Card} title=${t('general.browser_drops')} hint=${t('general.browser_drops_hint')}>
      <${Field} label=${t('general.browser_drops_folder')}>
        <${FolderField} value=${s.browserDrops.folder} placeholder=${boot.downloads} onChange=${(v) => save((x) => { x.browserDrops.folder = v.trim(); })} />
      </${Field}>
      <${Check} label=${t('general.browser_drops_only_converted')} hint=${t('general.browser_drops_only_converted_hint')}
        checked=${s.browserDrops.saveOnlyConverted} onChange=${(v) => save((x) => { x.browserDrops.saveOnlyConverted = v; })} />
    </${Card}>

    <${Card} title=${t('general.history')}>
      <${Toggle} label=${t('general.no_history')} checked=${!s.keepHistory} onChange=${(v) => save((x) => { x.keepHistory = !v; })} />
      ${s.keepHistory && html`<${Field} label=${t('general.history_limit')}>
        <${NumberInput} value=${s.historyLimit} min=${boot.limits.minHistory} max=${boot.limits.maxHistory} step=${50}
          onChange=${(v) => save((x) => { x.historyLimit = v; })} label=${t('general.history_limit')} />
      </${Field}>`}
      <div class="row">
        <${Button} onClick=${clearHistory}>${t('general.clear_history')}</${Button}>
        <span class="muted">${t('general.history_count', store.history?.length ?? 0)}</span>
      </div>
    </${Card}>

    <${Card} title=${t('general.library')}>
      <div class="row">
        <${Button} icon="Upload" onClick=${exportLibrary}>${t('general.export')}</${Button}>
        <${Button} icon="Download" onClick=${importLibrary}>${t('general.import')}</${Button}>
        <${Button} kind="danger" onClick=${reset}>${t('general.reset_library')}</${Button}>
      </div>
      ${libraryMessage && html`<p class="muted">${libraryMessage}</p>`}
    </${Card}>

    <${Card}>
      <div class="row between">
        <div class="grow"><span class="label">${t('general.data_folder')}</span><div class="path">${boot.dataDir}</div></div>
        <${Button} icon="Folder" onClick=${() => openPath(boot.dataDir)}>${t('general.open_data_folder')}</${Button}>
      </div>
    </${Card}>
  </div>`;
}
