// The web UI: the settings window (navigation on the left, the page on the
// right, as MainWindow) at #/<page>, and the tool windows at #/tool/<tool>?job=<id>.
import { html, render, useState, useEffect } from '../vendor/preact-htm.js';
import { store, subscribe, loadAll, connectEvents, refresh, api } from './api.js';
import { applyTheme, loadCustomTheme } from './theme.js';
import { t } from './i18n.js';
import { Icon, Button } from './ui.js';

import { HomePage } from './pages/home.js';
import { HistoryPage } from './pages/history.js';
import { GeneralPage } from './pages/general.js';
import { TriggersPage } from './pages/triggers.js';
import { ToolbarPage } from './pages/toolbar.js';
import { PresetsPage } from './pages/presets.js';
import { WatchPage } from './pages/watch.js';
import { ThemePage } from './pages/theme.js';
import { EnginesPage } from './pages/engines.js';
import { AboutPage } from './pages/about.js';

import { CropTool } from './tools/crop.js';
import { ClipTool } from './tools/clip.js';
import { HideTool } from './tools/hide.js';
import { ImageEditTool } from './tools/imageedit.js';
import { PdfPagesTool } from './tools/pdfpages.js';
import { PrintTool } from './tools/print.js';

export const pages = [
  { id: 'home', key: 'nav.home', icon: 'Home', view: HomePage },
  { id: 'history', key: 'home.recent', icon: 'Undo', view: HistoryPage },
  { id: 'general', key: 'nav.general', icon: 'Settings', view: GeneralPage },
  { id: 'triggers', key: 'nav.triggers', icon: 'Keyboard', view: TriggersPage },
  { id: 'toolbar', key: 'nav.toolbar', icon: 'Donut', view: ToolbarPage },
  { id: 'presets', key: 'nav.presets', icon: 'Tune', view: PresetsPage },
  { id: 'watch', key: 'nav.watch', icon: 'Folder', view: WatchPage },
  { id: 'theme', key: 'nav.theme', icon: 'Palette', view: ThemePage },
  { id: 'engines', key: 'nav.engines', icon: 'Engine', view: EnginesPage },
  { id: 'about', key: 'nav.about', icon: 'Info', view: AboutPage },
];

const tools = { crop: CropTool, clip: ClipTool, hide: HideTool, 'image-edit': ImageEditTool, 'pdf-pages': PdfPagesTool, print: PrintTool };

function parseRoute() {
  const h = location.hash.replace(/^#\/?/, '');
  const [path, query = ''] = h.split('?');
  const parts = path.split('/').filter(Boolean);
  const params = Object.fromEntries(new URLSearchParams(query));
  if (parts[0] === 'tool') return { tool: parts[1] || '', params };
  return { page: parts[0] || 'home', sub: parts.slice(1), params };
}

export function navigate(page, sub = []) {
  location.hash = '#/' + [page, ...sub].join('/');
}

function useRoute() {
  const [r, setR] = useState(parseRoute());
  useEffect(() => {
    const on = () => setR(parseRoute());
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return r;
}

function useStore() {
  const [, setN] = useState(0);
  useEffect(() => subscribe(() => setN((n) => n + 1)), []);
  return store;
}

function Shell({ route }) {
  const page = pages.find((p) => p.id === route.page) || pages[0];
  const View = page.view;
  const [update, setUpdate] = useState(null);
  useEffect(() => {
    if (store.settings?.checkForUpdates) api.get('api/update').then(setUpdate).catch(() => {});
  }, []);
  useEffect(() => { document.title = 'Filee · ' + t(page.key); }, [page.id, store.boot?.texts]);
  return html`<div class="shell">
    <nav class="sidebar">
      <div class="brand"><img src="filee.png" alt="" width="30" height="30" />
        <div><div class="brand-name">Filee</div><div class="caption">${t('app.tagline')}</div></div></div>
      <ul class="nav">
        ${pages.map((p) => html`<li><a href=${'#/' + p.id} class=${p.id === page.id ? 'on' : ''} aria-current=${p.id === page.id ? 'page' : undefined}>
          <${Icon} name=${p.icon} size=${17} /><span>${t(p.key)}</span></a></li>`)}
      </ul>
      <div class="sidebar-foot">
        ${update?.newer && html`<${Button} kind="accent" icon="Download" cls="wide" onClick=${() => window.open(update.download || update.url, '_blank')}>
          ${t('general.update_available', update.latest)}</${Button}>`}
        <div class="row tight">
          <a class="navlink" href="https://github.com/dseif0x/filee-go" target="_blank" rel="noopener"><${Icon} name="GitHub" size=${13} />GitHub</a>
        </div>
        <div class="caption version">${t('about.version', store.boot?.version)}</div>
        ${!store.connected && html`<div class="caption danger">● offline</div>`}
      </div>
    </nav>
    <main class="page" id="page"><div class="page-inner"><${View} route=${route} /></div></main>
  </div>`;
}

function ToolWindow({ route }) {
  const View = tools[route.tool];
  useEffect(() => { document.documentElement.classList.add('tool-window'); return () => document.documentElement.classList.remove('tool-window'); }, []);
  if (!View) return html`<div class="tool-missing"><p>${t('error.unsupported_source')}</p></div>`;
  if (!route.params.job) return html`<div class="tool-missing"><p>No job given (?job=…)</p></div>`;
  return html`<${View} job=${route.params.job} params=${route.params} />`;
}

function App() {
  useStore();
  const route = useRoute();
  useEffect(() => applyTheme(store.settings), [store.settings]);
  if (!store.boot) return html`<div class="boot">Filee</div>`;
  return route.tool !== undefined ? html`<${ToolWindow} route=${route} />` : html`<${Shell} route=${route} />`;
}

async function main() {
  try {
    await loadAll();
  } catch (e) {
    document.getElementById('app').innerHTML = '<div class="boot error">Filee could not start: ' + (e.message || e) + '</div>';
    return;
  }
  document.documentElement.lang = store.boot.language || 'en';
  applyTheme(store.settings);
  loadCustomTheme();
  connectEvents();
  refresh('jobs');
  render(html`<${App} />`, document.getElementById('app'));
}

main();
