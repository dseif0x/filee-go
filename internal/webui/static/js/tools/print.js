// Spreadsheet print studio (SpreadsheetPrintStudioViewModel): page setup for
// each sheet (or added region) of a workbook, an exact preview LibreOffice
// renders from a temporary copy, and one PDF of every included region.
import { html, useState, useEffect, useRef, Button, IconButton, Check, Select, NumberInput, Field, Spinner, Icon, Segmented, guard, notify } from '../ui.js';
import { api, openPath } from '../api.js';
import { t } from '../i18n.js';
import { useTool, ToolHead, ToolError } from './common.js';

export function PrintTool({ job: id }) {
  const [job, , error] = useTool(id);
  const [info, setInfo] = useState(null);
  const [plans, setPlans] = useState(null);
  const [selected, setSelected] = useState(0);
  const [preview, setPreview] = useState(null);
  const [page, setPage] = useState(0);
  const [status, setStatus] = useState('');
  const [exporting, setExporting] = useState(false);
  const timer = useRef(0);
  useEffect(() => {
    if (!job) return;
    api.get(`api/tools/${id}/print`).then((r) => {
      setInfo(r);
      setPlans(r.regions.map((reg, k) => ({ region: reg, sheet: r.sheets[k], included: !r.sheets[k].hidden, name: r.sheets[k].name, added: false })));
    }).catch((e) => setStatus(e.message));
  }, [job?.id]);
  const plan = plans?.[selected];
  const key = plan ? JSON.stringify(plan.region) : '';
  useEffect(() => {
    if (!plan || !info?.available) return undefined;
    clearTimeout(timer.current);
    setStatus(t('sheet_studio.rendering'));
    timer.current = setTimeout(async () => {
      try {
        const r = await api.post(`api/tools/${id}/print/preview`, plan.region);
        setPreview(r); setPage(0); setStatus(r.status);
      } catch (e) { setPreview(null); setStatus(e.message); }
    }, 500);
    return () => clearTimeout(timer.current);
  }, [key, info?.available]);
  if (error) return html`<${ToolError} error=${error} />`;
  if (!job || !plans) return html`<div class="tool-missing">${status ? html`<p class="danger">${status}</p>` : html`<${Spinner} size=${28} />`}</div>`;

  const setRegion = (f) => setPlans(plans.map((p, k) => (k === selected ? { ...p, region: { ...p.region, ...f } } : p)));
  const r = plan.region;
  const sh = plan.sheet;
  const warning = r.scaleMode === 'source' && sh.suspiciousScale ? t('sheet_studio.warning_scale', sh.pageSetup.scalePercent)
    : sh.wideUnbounded && !r.writePrintArea ? t('sheet_studio.warning_wide') : '';
  const applyFix = () => setRegion({
    range: sh.usedRange, writePrintArea: true, scaleMode: 'fit', fitWidth: sh.usedColumns > 60 ? 2 : 1, fitHeight: 0,
    ...(sh.usedColumns > 20 ? { orientation: 'landscape' } : {}),
    ...(r.paper === 'source' ? { paper: sh.pageSetup.paper === 'source' ? 'a4' : sh.pageSetup.paper } : {}), keepBreaks: false,
  });
  const reset = () => setPlans(plans.map((p, k) => (k === selected ? { ...p, region: info.regions.find((x) => x.sheet === p.sheet.index) } : p)));
  const addRegion = () => {
    const n = plans.filter((p) => p.sheet.index === sh.index).length + 1;
    const copy = { ...plan, region: { ...plan.region }, added: true, name: t('sheet_studio.region_name', sh.name, n) };
    const next = [...plans];
    next.splice(selected + 1, 0, copy);
    setPlans(next); setSelected(selected + 1);
  };
  const moveRegion = (dir) => {
    const j = selected + dir;
    if (j < 0 || j >= plans.length) return;
    const next = [...plans];
    [next[selected], next[j]] = [next[j], next[selected]];
    setPlans(next); setSelected(j);
  };
  const exportPdf = async () => {
    setExporting(true);
    setStatus(t('sheet_studio.exporting'));
    const res = await guard(() => api.post(`api/tools/${id}/print/export`, { regions: plans.filter((p) => p.included).map((p) => p.region) }));
    setExporting(false);
    if (res) { setStatus(res.message); if (res.path) notify(res.message); }
    else setStatus('');
  };
  const papers = info.papers.map((p) => ({ value: p.id, label: p.id === 'source' ? t('sheet_studio.from_workbook') : p.name }));
  const validRange = /^[A-Za-z$0-9:,' !]+$/.test(r.range || '');
  return html`<div class="tool">
    <${ToolHead} title=${t('sheet_studio.title') + ' · ' + info.file} job=${job} index=${-1} />
    <div class="tool-body">
      <div class="print-grid" style="flex:1">
        <aside style="border-right:1px solid var(--border);background:var(--surface);padding:12px" class="stack tight">
          <strong>${t('sheet_studio.sheets')}</strong>
          <ul class="select-list" style="padding:0">${plans.map((p, k) => html`<li><button type="button" class=${k === selected ? 'on' : ''} onClick=${() => setSelected(k)}>
            <input type="checkbox" checked=${p.included} onClick=${(e) => e.stopPropagation()} onChange=${(e) => setPlans(plans.map((x, n) => (n === k ? { ...x, included: e.currentTarget.checked } : x)))} />
            <span style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis">${p.name}</span>
            <span class="sub">${p.sheet.usedRange}</span></button></li>`)}</ul>
          <div class="row tight">
            <${IconButton} icon="ArrowUp" title=${t('group.move_up')} disabled=${selected === 0} onClick=${() => moveRegion(-1)} />
            <${IconButton} icon="ArrowDown" title=${t('group.move_down')} disabled=${selected === plans.length - 1} onClick=${() => moveRegion(1)} />
            ${plan.added && html`<${IconButton} icon="Delete" title=${t('common.delete')} onClick=${() => { setPlans(plans.filter((_, k) => k !== selected)); setSelected(Math.max(0, selected - 1)); }} />`}
          </div>
          <${Button} onClick=${addRegion}>${t('sheet_studio.add_region')}</${Button}>
        </aside>
        <section class="print-preview">
          ${!info.available ? html`<div class="empty"><${Icon} name="Engine" size=${32} /><p>LibreOffice: ${info.reason}</p>
              <p class="caption">${t('engines.package.libreoffice.description')}</p></div>`
            : !preview ? html`<${Spinner} size=${28} />` : html`
            <img src=${`api/tools/${id}/print/page/${page}?token=${preview.token}&max=1400`} alt="" />
            ${preview.pages > 1 && html`<div class="thumbs">${Array.from({ length: Math.min(preview.pages, 60) }, (_, n) => html`<button type="button" class=${n === page ? 'on' : ''}
              onClick=${() => setPage(n)}><img src=${`api/tools/${id}/print/page/${n}?token=${preview.token}&max=160`} alt=${String(n + 1)} /></button>`)}</div>`}`}
        </section>
        <aside style="border-left:1px solid var(--border);background:var(--surface);padding:12px" class="stack tight">
          <strong>${t('sheet_studio.page_setup')}</strong>
          <p class="caption">${sh.usedRange} · ${sh.usedRows} × ${sh.usedColumns}</p>
          ${warning && html`<div class="card flat" style="padding:10px;gap:6px"><p class="warning caption">${warning}</p><${Button} onClick=${applyFix}>${t('sheet_studio.apply_fix')}</${Button}></div>`}
          <${Field} label=${t('sheet_studio.print_area')} error=${validRange ? '' : t('sheet_studio.invalid_range')}>
            <div class="row tight"><input class="input" style="flex:1" value=${r.range} onChange=${(e) => setRegion({ range: e.currentTarget.value.trim(), writePrintArea: true })} />
              <${Button} onClick=${() => setRegion({ range: sh.usedRange, writePrintArea: true })}>${t('sheet_studio.use_content')}</${Button}></div></${Field}>
          <${Field} label=${t('sheet_studio.paper')}><${Select} value=${r.paper} options=${papers} onChange=${(v) => setRegion({ paper: v })} /></${Field}>
          <${Field} label=${t('sheet_studio.orientation')}><${Select} value=${r.orientation}
            options=${[['source', 'sheet_studio.from_workbook'], ['portrait', 'sheet_studio.portrait'], ['landscape', 'sheet_studio.landscape']].map(([v, k]) => ({ value: v, label: t(k) }))}
            onChange=${(v) => setRegion({ orientation: v })} /></${Field}>
          <${Field} label=${t('sheet_studio.scaling')}><${Select} value=${r.scaleMode}
            options=${[['source', 'sheet_studio.from_workbook'], ['percent', 'sheet_studio.exact_scale'], ['fit', 'sheet_studio.fit_pages']].map(([v, k]) => ({ value: v, label: t(k) }))}
            onChange=${(v) => setRegion({ scaleMode: v })} /></${Field}>
          ${r.scaleMode === 'percent' && html`<${Field} label=${t('sheet_studio.percent')}><${NumberInput} value=${r.scalePercent} min=${10} max=${400} onChange=${(v) => setRegion({ scalePercent: v })} /></${Field}>`}
          ${r.scaleMode === 'fit' && html`<div class="row"><${Field} label=${t('sheet_studio.pages_wide')}><${NumberInput} value=${r.fitWidth} min=${1} max=${100} onChange=${(v) => setRegion({ fitWidth: v })} /></${Field}>
            <${Field} label=${t('sheet_studio.pages_tall')}><${NumberInput} value=${r.fitHeight} min=${0} max=${100} onChange=${(v) => setRegion({ fitHeight: v })} /></${Field}></div>`}
          <span class="label">${t('sheet_studio.margins')}</span>
          <div class="grid2">${[['marginTopMm', 'top'], ['marginBottomMm', 'bottom'], ['marginLeftMm', 'left'], ['marginRightMm', 'right']].map(([f, k]) => html`
            <${Field} label=${t('sheet_studio.margin_' + k)}><${NumberInput} value=${Math.round(r[f] * 10) / 10} min=${0} max=${100} step=${0.5} onChange=${(v) => setRegion({ [f]: v })} /></${Field}>`)}</div>
          <${Field} label=${t('sheet_studio.page_order')}><${Select} value=${r.order}
            options=${[['source', 'sheet_studio.from_workbook'], ['downThenOver', 'sheet_studio.down_over'], ['overThenDown', 'sheet_studio.over_down']].map(([v, k]) => ({ value: v, label: t(k) }))}
            onChange=${(v) => setRegion({ order: v })} /></${Field}>
          <${Check} label=${t('sheet_studio.include_hidden')} checked=${r.includeHidden} onChange=${(v) => setRegion({ includeHidden: v })} />
          <${Check} label=${t('sheet_studio.keep_breaks')} checked=${r.keepBreaks} onChange=${(v) => setRegion({ keepBreaks: v })} />
          <${Button} kind="ghost" onClick=${reset}>${t('sheet_studio.reset_sheet')}</${Button}>
        </aside>
      </div>
    </div>
    <footer class="tool-foot">
      <span class="muted">${status}</span>
      <span class="spacer"></span>
      <${Button} kind="accent" icon="Download" disabled=${exporting || !info.available || !plans.some((p) => p.included)} onClick=${exportPdf}>${t('sheet_studio.export_pdf')}</${Button}>
    </footer>
  </div>`;
}
