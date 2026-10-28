// About: version, license, repository, updates (GitHub releases of
// dseif0x/filee-go) and support (AboutPage.axaml).
import { html, useState, Card, Toggle, Button, Icon, Spinner, guard } from '../ui.js';
import { store, api, saveSettings } from '../api.js';
import { t } from '../i18n.js';

const repo = 'https://github.com/dseif0x/filee-go';

export function AboutPage() {
  const s = store.settings;
  const [info, setInfo] = useState(null);
  const [checking, setChecking] = useState(false);
  const check = async () => {
    setChecking(true);
    const r = await guard(() => api.get('api/update?force=1'));
    setChecking(false);
    if (r) setInfo(r);
  };
  const status = !info ? '' : info.newer ? t('general.update_available', info.latest)
    : info.latest ? t('general.update_none', info.current) : info.error ? `${t('general.update_none', info.current)} · ${info.error}` : '';
  return html`<div class="stack">
    <h1>${t('about.title')}</h1>
    <${Card}>
      <div class="row" style="gap:18px">
        <img src="filee.png" width="72" height="72" alt="" />
        <div class="stack tight" style="gap:4px">
          <div style="font-size:24px;font-weight:700">Filee</div>
          <div class="muted">${t('about.version', store.boot.version)}</div>
          <div class="muted">${t('about.license')}</div>
          <a href=${repo} target="_blank" rel="noopener" class="row tight"><${Icon} name="Link" size=${13} />${repo}</a>
        </div>
      </div>
    </${Card}>
    <${Card} title=${t('about.updates')}>
      <${Toggle} label=${t('general.check_updates')} checked=${s.checkForUpdates} onChange=${(v) => guard(() => saveSettings((x) => { x.checkForUpdates = v; }))} />
      <div class="row">
        <${Button} onClick=${check} disabled=${checking}>${t('general.check_now')}</${Button}>
        ${info?.newer && html`<${Button} kind="accent" icon="Download" onClick=${() => window.open(info.download || info.url, '_blank')}>${t('update.download')}</${Button}>`}
        ${checking ? html`<${Spinner} />` : html`<span class="muted">${status}</span>`}
      </div>
      ${info?.notes?.length > 0 && html`<div class="stack tight">${info.notes.map((n) => html`<div><strong>${n.version}</strong>
        <pre class="caption selectable" style="white-space:pre-wrap;margin:4px 0 0">${n.body}</pre></div>`)}</div>`}
    </${Card}>
    <${Card} title=${t('about.support_title')}>
      <p class="muted">${t('about.support_text')}</p>
      <div class="row">
        <${Button} kind="accent" onClick=${() => window.open(repo, '_blank')}>${t('feedback.star')}</${Button}>
        <${Button} onClick=${() => window.open(repo + '/issues/new', '_blank')}>${t('feedback.issue')}</${Button}>
      </div>
    </${Card}>
  </div>`;
}
