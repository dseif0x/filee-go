// The preset editor (PresetEditorView / PresetEditorViewModel): only the
// option groups that apply to the chosen target are shown, and Apply keeps
// only what is shown, as the original does.
import { html, useState, useEffect, Card, Field, Select, Check, Slider, NumberInput, TextInput, FolderField, Button, IconButton,
  showDialog, confirmDialog, guard, notify, Icon } from '../ui.js';
import { store, api } from '../api.js';
import { t, presetName, formatName, categoryOf, categoryName, fileSize } from '../i18n.js';
import { ocrLanguages } from './ocrlangs.js';

const lossy = ['jpg', 'webp', 'avif', 'heic'];
const losslessAudio = ['wav', 'flac', 'aiff', 'wv', 'tta', 'w64'];
const maxHeightSteps = [0, 2160, 1440, 1080, 720, 480];
const bitrateSteps = [0, 32, 48, 64, 96, 128, 192, 256, 320];
const sampleRateSteps = [0, 8000, 16000, 22050, 44100, 48000];
const ocrTargets = ['pdf', 'docx', 'txt', 'md', 'hwpx'];
const frameTargets = ['png', 'jpg'];
const motionTargets = ['mp4', 'mov'];
const joinTargets = ['mp4', 'm4a'];
const h264Targets = ['mp4', 'm4v', 'mov', 'mkv', 'flv', 'ts', 'm2ts', '3gp', '3g2'];

function videoCodecsFor(target) {
  switch (target) {
    case 'mp4': case 'm4v': case 'mkv': return ['Default', 'Hevc', 'Av1'];
    case 'mov': return ['Default', 'Hevc'];
    case 'webm': return ['Default', 'Av1'];
  }
  return ['Default'];
}
const audioCodecsFor = (target) => (target === 'm4a' || target === 'm4b' ? ['Default', 'Alac'] : ['Default']);
const effective = (list, v) => (list.includes(v) ? v : 'Default');
const livePhotoTargets = (k) => (k === 'Apple' ? ['jpg', 'heic'] : k === 'Android' ? ['jpg'] : []);

function steps(list, current, label) {
  const all = list.includes(current) ? list : [...list, current];
  return all.map((v) => ({ value: v, label: label(v) }));
}

function nativeName(culture, code) {
  try {
    const n = new Intl.DisplayNames([culture], { type: 'language' }).of(culture);
    return n ? n.charAt(0).toUpperCase() + n.slice(1) : code;
  } catch { return code; }
}

/** visibility is PresetEditorViewModel's Show… properties for the preset as edited. */
export function visibility(p, orig, ui) {
  const target = p.targetFormat;
  const cat = categoryOf(target);
  const isTool = orig.kind === 'Tool';
  const isClip = isTool && orig.tool === 'clip';
  const targetIsImage = target === 'same' || cat === 'Image';
  const targetIsVideo = cat === 'Video' || isClip;
  const targetIsAudio = cat === 'Audio';
  const audioCodecs = audioCodecsFor(target);
  const targetIsLossyMedia = targetIsVideo || (targetIsAudio && !losslessAudio.includes(target) && effective(audioCodecs, p.media.audioCodec) !== 'Alac');
  const liveKinds = ['None', ...['Apple', 'Android'].filter((k) => livePhotoTargets(k).includes(target))];
  const showLivePhotoChoice = !isTool && liveKinds.length > 1;
  const isLivePhoto = showLivePhotoChoice && p.media.livePhoto !== 'None' && livePhotoTargets(p.media.livePhoto).includes(target);
  const showOcr = !isTool && ocrTargets.includes(target);
  const recognizes = showOcr && p.pdf.ocr;
  const videoCodecs = videoCodecsFor(target);
  const isSlideshow = orig.media?.slideshow;
  const v = {
    isTool, isClip, targetIsImage, targetIsVideo, targetIsAudio, liveKinds, isLivePhoto, showLivePhotoChoice, recognizes,
    videoCodecs, audioCodecs,
    canTurn: targetIsImage || target === 'pdf',
    showImage: !isTool && targetIsImage && !isLivePhoto,
    showTurnForPdf: target === 'pdf',
    showQuality: !isTool && (target === 'same' || lossy.includes(target)),
    showTiff: target === 'tiff' || target === 'same',
    showWebp: target === 'webp' || target === 'same',
    showMultiPageTiff: target === 'tiff',
    showFitsTiff: !isTool && target === 'tiff',
    showFitsDebayer: !isTool && cat === 'Image' && target !== 'tiff',
    showIco: target === 'ico',
    showPdf: !isTool && !isLivePhoto && !['Video', 'Audio', 'Archive', 'Font', 'Subtitle', 'Data', 'Model3D', 'Geo'].includes(cat) && target !== 'eml',
    showZipResult: cat === 'Geo',
    showPdfBuild: target === 'pdf',
    showPageSize: target === 'pdf' && !recognizes,
    showPdfSplit: target === 'pdf' && !recognizes,
    showPdfCompression: target === 'pdf' && !recognizes,
    showPdfTablesNote: target === 'xlsx',
    showOcr,
    showPdfCombine: cat === 'Image' && !['tiff', 'gif', 'apng', 'mng', 'ico', 'cur', 'icns', 'svg', 'pdf'].includes(target),
    showDocument: target === 'pdf',
    showLeaveOutPictures: target === 'md',
    showAudioChannels: targetIsAudio && target !== 'amr',
    showWav: target === 'wav' || target === 'w64',
    showVideoCodec: targetIsVideo && videoCodecs.length > 1,
    showHardwareEncoding: targetIsVideo && (['Hevc', 'Av1'].includes(effective(videoCodecs, p.media.videoCodec)) || h264Targets.includes(target)),
    showAudioCodec: audioCodecs.length > 1,
    showMediaQuality: targetIsLossyMedia,
    showMaxHeight: targetIsVideo,
    showVideoMaxSize: cat === 'Video' && !isClip && !orig.media?.joinIntoOne && !isSlideshow && !['vob', 'dv'].includes(target),
    showAudioBitrate: targetIsLossyMedia,
    showRemoveAudio: targetIsVideo,
    showArchive: cat === 'Archive',
    showArchiveOptions: cat === 'Archive' && target !== 'folder',
    showExtractNote: target === 'folder',
    showSaveEveryFrame: !isTool && !isLivePhoto && frameTargets.includes(target),
    showSlideshow: isSlideshow && joinTargets.includes(target) && targetIsVideo,
    showExtractMotionPhoto: !isTool && motionTargets.includes(target),
    showAnimationSpeed: ['gif', 'webp', 'apng', 'mng', 'same'].includes(target),
    showTrace: ['svg', 'svgz', 'dxf', 'dwg'].includes(target),
    showCad: !isTool && ['dxf', 'dwg'].includes(target),
    showCadColors: !isTool && (target === 'pdf' || target === 'svg' || cat === 'Image'),
  };
  v.showMedia = v.showMediaQuality || v.showMaxHeight || v.showRemoveAudio || v.showAudioChannels || v.showVideoCodec || v.showAudioCodec;
  return v;
}

/** uiState reads the editor's own choices out of a preset (resize, location). */
function uiState(p) {
  const i = p.image;
  return {
    resize: i.customSize ? 'Custom' : i.minShortEdge ? 'MinShortEdge' : i.resize || 'None',
    customWidth: i.customSize?.width ?? 1920, customHeight: i.customSize?.height ?? 1080, lockAspect: i.customSize?.lockAspect ?? false,
    minShortEdge: i.minShortEdge ?? 1080, setDpi: i.dpi != null, dpi: i.dpi ?? 300,
    location: p.output.askEveryTime ? 'Ask' : p.output.location,
    icoSizes: (i.icoSizes || []).join(', '),
    imageMaxSizeMb: i.maxSizeMb || null, videoMaxSizeMb: p.media.maxSizeMb || null,
  };
}

/** apply is PresetEditorViewModel.Apply: the edited copy with what isn't shown reset. */
export function applyEdits(p, orig, ui) {
  const v = visibility(p, orig, ui);
  const out = structuredClone(p);
  const im = out.image;
  im.quality = clamp(im.quality, 1, 100);
  im.maxSizeMb = v.showQuality ? clamp(ui.imageMaxSizeMb || 0, 0, 4096) : 0;
  if (ui.resize === 'Custom') {
    im.resize = 'None';
    im.customSize = { width: clamp(ui.customWidth, 16, 16384), height: clamp(ui.customHeight, 16, 16384), lockAspect: ui.lockAspect };
    delete im.minShortEdge;
  } else if (ui.resize === 'MinShortEdge') {
    im.resize = 'None';
    delete im.customSize;
    im.minShortEdge = clamp(ui.minShortEdge, 16, 16384);
  } else {
    im.resize = ui.resize;
    delete im.customSize;
    delete im.minShortEdge;
  }
  if (!v.canTurn) { im.rotation = 'None'; im.flip = 'None'; }
  im.resizePercent = clamp(im.resizePercent, 1, 1000);
  im.longEdge = clamp(im.longEdge, 16, 20000);
  im.width = clamp(im.width, 16, 20000);
  if (ui.setDpi) im.dpi = clamp(ui.dpi, 36, 2400); else delete im.dpi;
  im.backgroundTolerance = clamp(im.backgroundTolerance, 0, 100);
  im.background = (im.background || '').trim() || '#FFFFFF';
  let sizes = ui.icoSizes.split(',').map((s) => parseInt(s.trim(), 10)).filter((x) => x > 0 && x <= 256);
  sizes = [...new Set(sizes)];
  im.icoSizes = sizes.length ? sizes : [256, 48, 32, 16];
  im.animationSpeed = v.showAnimationSpeed ? im.animationSpeed : 1;

  const pdf = out.pdf;
  pdf.marginMm = clamp(pdf.marginMm || 0, 0, 100);
  pdf.renderDpi = clamp(pdf.renderDpi, 36, 1200);
  pdf.pageRange = (pdf.pageRange || '').trim();
  pdf.watermark = v.showPdfBuild ? (pdf.watermark || '').trim() : '';
  pdf.pageNumbers = v.showPdfBuild && pdf.pageNumbers;
  pdf.password = v.showPdfBuild && !out.document.pdfA ? pdf.password || '' : '';
  pdf.passwordLeftOut = false;
  pdf.ocr = v.recognizes;
  if (pdf.ocr) { pdf.splitPages = false; pdf.compression = 'None'; pdf.compressWithGhostscript = false; pdf.pageSize = 'FitImage'; pdf.marginMm = 0; }
  out.document.leaveOutPictures = v.showLeaveOutPictures && out.document.leaveOutPictures;

  const m = out.media;
  m.maxSizeMb = v.showVideoMaxSize ? clamp(ui.videoMaxSizeMb || 0, 0, 4096) : 0;
  m.videoCodec = v.isLivePhoto ? (m.videoCodec === 'Hevc' ? 'Hevc' : 'Default') : effective(v.videoCodecs, m.videoCodec);
  m.audioCodec = effective(v.audioCodecs, m.audioCodec);
  m.saveEveryFrame = v.showSaveEveryFrame && m.saveEveryFrame;
  m.frameInterval = m.saveEveryFrame ? clamp(m.frameInterval, 1, 1000) : 1;
  m.extractMotionPhoto = v.showExtractMotionPhoto && m.extractMotionPhoto;
  if (v.showSlideshow) m.slideSeconds = clamp(m.slideSeconds, 0.5, 60);
  m.livePhoto = v.isLivePhoto ? m.livePhoto : 'None';
  m.livePhotoStart = clamp(m.livePhotoStart || 0, 0, 86400);
  m.livePhotoLength = clamp(m.livePhotoLength, 0.5, 5);
  m.livePhotoCover = clamp(m.livePhotoCover, 0, 5);
  out.trace.colors = clamp(out.trace.colors, 2, 64);
  out.trace.detail = clamp(out.trace.detail, 0, 100);
  out.cad.version = v.showCad ? out.cad.version : '';
  out.cad.colors = v.showCadColors ? out.cad.colors : '';

  const o = out.output;
  o.askEveryTime = ui.location === 'Ask';
  if (ui.location !== 'Ask') o.location = ui.location;
  o.subfolderName = (o.subfolderName || '').trim() || 'converted';
  o.subfolderOnlyForSeveralFiles = ui.location === 'Subfolder' && o.subfolderOnlyForSeveralFiles;
  o.customFolder = (o.customFolder || '').trim();
  o.fileNamePattern = (o.fileNamePattern || '').trim() || '{name}';
  o.zipResult = v.showZipResult && o.zipResult;
  // A built-in preset keeps its translatable name unless it was renamed.
  if (orig.nameKey && out.name.trim() === presetName({ ...orig, name: '' })) out.name = orig.name;
  else out.name = out.name.trim();
  return out;
}

function clamp(v, lo, hi) { const n = Number(v); return Math.max(lo, Math.min(hi, Number.isFinite(n) ? n : lo)); }

function targetOptions() {
  const formats = (store.boot.formats || []).filter((f) => f.writable);
  return [{ value: 'same', label: t('presets.same_as_source') }, ...formats.map((f) => ({ value: f.id, label: f.name, detail: categoryName(f.category) }))];
}

/** PresetEditor edits a copy of preset; onChange gets the edited (raw) copy and the ui state. */
export function PresetEditor({ preset, onState }) {
  const [p, setP] = useState(() => withName(preset));
  const [ui, setUi] = useState(() => uiState(preset));
  useEffect(() => { setP(withName(preset)); setUi(uiState(preset)); }, [preset]);
  useEffect(() => onState?.(p, ui), [p, ui]);
  const set = (f) => setP((old) => { const n = structuredClone(old); f(n); return n; });
  const setU = (f) => setUi((old) => ({ ...old, ...f }));
  const v = visibility(p, preset, ui);
  const im = p.image;
  const pdf = p.pdf;
  const m = p.media;
  const o = p.output;
  const opt = (prefix, list) => list.map((x) => ({ value: x, label: t(prefix + x) }));
  const nameError = !p.name.trim() ? t('presets.name_required') : '';
  const isMac = store.boot.os === 'darwin';

  return html`<div class="editor">
    <div class="grid2">
      <${Field} label=${t('presets.name')} error=${nameError}><${TextInput} live value=${p.name} onChange=${(x) => set((n) => { n.name = x; })} /></${Field}>
      ${p.kind !== 'Group' && html`<${Field} label=${t('presets.target')}>
        <${Select} label=${t('presets.target')} value=${p.targetFormat} options=${targetOptions()} disabled=${v.isTool} onChange=${(x) => set((n) => { n.targetFormat = x; })} />
      </${Field}>`}
    </div>

    ${v.showLivePhotoChoice && html`<${Card} title=${t('presets.group.live_photo')}>
      <${Field} label=${t('presets.live_photo')}>
        <${Select} value=${v.liveKinds.includes(m.livePhoto) ? m.livePhoto : 'None'} options=${opt('presets.live_photo.', v.liveKinds)} onChange=${(x) => set((n) => { n.media.livePhoto = x; })} />
      </${Field}>
      ${v.isLivePhoto && html`<p class="caption">${t(m.livePhoto === 'Android' ? 'presets.live_photo_hint.android' : 'presets.live_photo_hint.apple')}</p>
        <div class="grid3">
          <${Field} label=${t('presets.live_photo_start')}><${NumberInput} value=${m.livePhotoStart || 0} min=${0} step=${0.1} onChange=${(x) => set((n) => { n.media.livePhotoStart = x; })} /></${Field}>
          <${Field} label=${t('presets.live_photo_length')}><${NumberInput} value=${m.livePhotoLength} min=${0.5} max=${5} step=${0.1} onChange=${(x) => set((n) => { n.media.livePhotoLength = x; })} /></${Field}>
          <${Field} label=${t('presets.live_photo_cover')}><${NumberInput} value=${m.livePhotoCover} min=${0} max=${5} step=${0.1} onChange=${(x) => set((n) => { n.media.livePhotoCover = x; })} /></${Field}>
        </div>
        <${Slider} label=${t('presets.live_photo_quality')} min=${1} max=${100} value=${im.quality} onChange=${(x) => set((n) => { n.image.quality = x; })} />
        <div class="grid3">
          <${Field} label=${t('presets.live_photo_video_quality')}><${Select} value=${m.quality} options=${opt('presets.media_quality.', ['High', 'Balanced', 'Small'])} onChange=${(x) => set((n) => { n.media.quality = x; })} /></${Field}>
          <${Field} label=${t('presets.max_height')}><${Select} value=${m.maxHeight || 0} options=${steps(maxHeightSteps, m.maxHeight || 0, (h) => (h ? h + 'p' : t('presets.max_height.original')))} onChange=${(x) => set((n) => { n.media.maxHeight = x; })} /></${Field}>
          <${Field} label=${t('presets.video_codec')}><${Select} value=${m.videoCodec === 'Hevc' ? 'Hevc' : 'Default'} options=${[{ value: 'Default', label: t('presets.video_codec.h264') }, { value: 'Hevc', label: t('presets.video_codec.hevc') }]} onChange=${(x) => set((n) => { n.media.videoCodec = x; })} /></${Field}>
        </div>
        <${Check} label=${t('presets.remove_audio')} checked=${m.removeAudio} onChange=${(x) => set((n) => { n.media.removeAudio = x; })} />`}
    </${Card}>`}

    ${v.showImage && html`<${Card} title=${t('presets.group.image')}>
      ${v.showQuality && html`<${Slider} label=${t('presets.quality')} min=${1} max=${100} value=${im.quality} onChange=${(x) => set((n) => { n.image.quality = x; })} />
        <${Field} label=${t('presets.max_size')} hint=${t('presets.max_size_hint_image')}>
          <${NumberInput} value=${ui.imageMaxSizeMb} min=${0} max=${4096} step=${0.1} allowEmpty onChange=${(x) => setU({ imageMaxSizeMb: x })} /></${Field}>`}
      <div class="grid2">
        <${Field} label=${t('presets.resize')}><${Select} value=${ui.resize} options=${opt('presets.resize.', ['None', 'Percent', 'LongEdge', 'Width', 'Custom', 'MinShortEdge'])} onChange=${(x) => setU({ resize: x })} /></${Field}>
        ${ui.resize === 'Percent' && html`<${Field} label=${t('presets.percent')}><${NumberInput} value=${im.resizePercent} min=${1} max=${1000} onChange=${(x) => set((n) => { n.image.resizePercent = x; })} /></${Field}>`}
        ${ui.resize === 'LongEdge' && html`<${Field} label=${t('presets.long_edge')}><${NumberInput} value=${im.longEdge} min=${16} max=${20000} onChange=${(x) => set((n) => { n.image.longEdge = x; })} /></${Field}>`}
        ${ui.resize === 'Width' && html`<${Field} label=${t('presets.width')}><${NumberInput} value=${im.width} min=${16} max=${20000} onChange=${(x) => set((n) => { n.image.width = x; })} /></${Field}>`}
        ${ui.resize === 'MinShortEdge' && html`<${Field} label=${t('presets.min_short_edge')} hint=${t('presets.min_short_edge_hint')}><${NumberInput} value=${ui.minShortEdge} min=${16} max=${16384} onChange=${(x) => setU({ minShortEdge: x })} /></${Field}>`}
      </div>
      ${ui.resize === 'Custom' && html`<div class="row">
        <${Field} label=${t('presets.width')}><${NumberInput} value=${ui.customWidth} min=${16} max=${16384} onChange=${(x) => setU({ customWidth: x })} /></${Field}>
        <${Field} label=${t('presets.height')}><${NumberInput} value=${ui.customHeight} min=${16} max=${16384} onChange=${(x) => setU({ customHeight: x })} /></${Field}>
        <${Check} label=${t('presets.lock_aspect')} checked=${ui.lockAspect} onChange=${(x) => setU({ lockAspect: x })} /></div>`}
      <div class="grid2">
        <${Field} label=${t('presets.rotation')}><${Select} value=${im.rotation || 'None'} options=${opt('presets.rotation.', ['None', 'Right90', 'Left90', 'Rotate180'])} onChange=${(x) => set((n) => { n.image.rotation = x; })} /></${Field}>
        <${Field} label=${t('presets.flip')}><${Select} value=${im.flip || 'None'} options=${opt('presets.flip.', ['None', 'Horizontal', 'Vertical'])} onChange=${(x) => set((n) => { n.image.flip = x; })} /></${Field}>
      </div>
      <div class="row"><${Check} label=${t('presets.dpi')} checked=${ui.setDpi} onChange=${(x) => setU({ setDpi: x })} />
        <${NumberInput} value=${ui.dpi} min=${36} max=${2400} disabled=${!ui.setDpi} onChange=${(x) => setU({ dpi: x })} label=${t('presets.dpi')} /></div>
      <${Check} label=${t('presets.keep_metadata')} checked=${im.keepMetadata} onChange=${(x) => set((n) => { n.image.keepMetadata = x; })} />
      <${Check} label=${t('presets.keep_color_profile')} hint=${t('presets.keep_color_profile_hint')} checked=${im.keepMetadata || im.keepColorProfile} disabled=${im.keepMetadata}
        onChange=${(x) => set((n) => { n.image.keepColorProfile = x; })} />
      <${Check} label=${t('presets.grayscale')} checked=${im.grayscale} onChange=${(x) => set((n) => { n.image.grayscale = x; })} />
      ${v.showWebp && html`<${Check} label=${t('presets.webp_lossless')} checked=${im.webpLossless} onChange=${(x) => set((n) => { n.image.webpLossless = x; })} />`}
      <${Check} label=${t('presets.remove_background')} hint=${t('presets.remove_background_hint')} checked=${im.removeBackground} onChange=${(x) => set((n) => { n.image.removeBackground = x; })} />
      ${im.removeBackground && html`<div class="stack tight" style="margin-left:23px">
        <${Slider} label=${t('presets.background_tolerance')} min=${0} max=${100} value=${im.backgroundTolerance} onChange=${(x) => set((n) => { n.image.backgroundTolerance = x; })} />
        <p class="caption">${t('presets.background_tolerance_hint')}</p>
        <${Check} label=${t('presets.keep_enclosed_background')} hint=${t('presets.keep_enclosed_background_hint')} checked=${im.keepEnclosedBackground} onChange=${(x) => set((n) => { n.image.keepEnclosedBackground = x; })} /></div>`}
      <div class="grid2">
        <${Field} label=${t('presets.background')}><${TextInput} value=${im.background} onChange=${(x) => set((n) => { n.image.background = x; })} /></${Field}>
        ${v.showTiff && html`<${Field} label=${t('presets.tiff_compression')}><${Select} value=${im.tiffCompression} options=${['None', 'Lzw', 'Zip'].map((x) => ({ value: x, label: x.toUpperCase() }))} onChange=${(x) => set((n) => { n.image.tiffCompression = x; })} /></${Field}>`}
        ${v.showIco && html`<${Field} label=${t('presets.ico_sizes')}><${TextInput} value=${ui.icoSizes} onChange=${(x) => setU({ icoSizes: x })} /></${Field}>`}
        ${v.showAnimationSpeed && html`<${Field} label=${t('presets.animation_speed')} hint=${t('presets.animation_speed_hint')}>
          <${Select} value=${im.animationSpeed || 1} options=${store.boot.limits.animationSpeeds.map((s) => ({ value: s, label: s === 1 ? t('presets.animation_speed.original') : s + '×' }))} onChange=${(x) => set((n) => { n.image.animationSpeed = x; })} /></${Field}>`}
      </div>
      ${v.showMultiPageTiff && html`<${Check} label=${t('presets.multipage_tiff')} hint=${t('presets.multipage_tiff_hint')} checked=${im.multiPageTiff} onChange=${(x) => set((n) => { n.image.multiPageTiff = x; })} />`}
      ${v.showFitsTiff && html`<${Field} label=${t('presets.fits_tiff')} hint=${t('presets.fits_tiff_hint')}><${Select} value=${im.fitsTiff || 'Linear'} options=${opt('presets.fits_tiff.', ['Linear', 'Stretched'])} onChange=${(x) => set((n) => { n.image.fitsTiff = x; })} /></${Field}>`}
      ${v.showFitsDebayer && html`<${Check} label=${t('presets.fits_debayer')} hint=${t('presets.fits_debayer_hint')} checked=${im.fitsDebayer} onChange=${(x) => set((n) => { n.image.fitsDebayer = x; })} />`}
      ${v.showSaveEveryFrame && html`<${Check} label=${t('presets.save_every_frame')} hint=${t('presets.save_every_frame_hint')} checked=${m.saveEveryFrame} onChange=${(x) => set((n) => { n.media.saveEveryFrame = x; })} />
        ${m.saveEveryFrame && html`<div style="margin-left:23px"><${Field} label=${t('presets.frame_interval')}><${NumberInput} value=${m.frameInterval} min=${1} max=${1000} onChange=${(x) => set((n) => { n.media.frameInterval = x; })} /></${Field}>
          <p class="caption">${t('presets.frame_estimate', Math.ceil(1800 / Math.max(1, m.frameInterval)), '100,000')}</p></div>`}`}
    </${Card}>`}

    ${v.showMedia && html`<${Card} title=${t('presets.group.media')}>
      ${v.showExtractMotionPhoto && html`<${Check} label=${t('presets.extract_motion_photo')} hint=${t('presets.extract_motion_photo_hint')} checked=${m.extractMotionPhoto} onChange=${(x) => set((n) => { n.media.extractMotionPhoto = x; })} />`}
      ${v.showSlideshow && html`<div class="row"><${Field} label=${t('presets.slide_seconds')}><${NumberInput} value=${m.slideSeconds} min=${0.5} max=${60} step=${0.5} onChange=${(x) => set((n) => { n.media.slideSeconds = x; })} /></${Field}>
        <${Check} label=${t('presets.crossfade')} hint=${t('presets.crossfade_hint')} checked=${m.crossfade} onChange=${(x) => set((n) => { n.media.crossfade = x; })} /></div>`}
      <div class="grid2">
        ${v.showMediaQuality && html`<${Field} label=${t('presets.media_quality')} hint=${t('presets.media_quality_hint')}><${Select} value=${m.quality} options=${opt('presets.media_quality.', ['High', 'Balanced', 'Small'])} onChange=${(x) => set((n) => { n.media.quality = x; })} /></${Field}>`}
        ${v.showMaxHeight && html`<${Field} label=${t('presets.max_height')}><${Select} value=${m.maxHeight || 0} options=${steps(maxHeightSteps, m.maxHeight || 0, (h) => (h ? h + 'p' : t('presets.max_height.original')))} onChange=${(x) => set((n) => { n.media.maxHeight = x; })} /></${Field}>`}
        ${v.showVideoMaxSize && html`<${Field} label=${t('presets.max_size')} hint=${t('presets.max_size_hint_video')}><${NumberInput} value=${ui.videoMaxSizeMb} min=${0} max=${4096} step=${0.1} allowEmpty onChange=${(x) => setU({ videoMaxSizeMb: x })} /></${Field}>`}
        ${v.showVideoCodec && html`<${Field} label=${t('presets.video_codec')} hint=${t('presets.video_codec_hint')}><${Select} value=${effective(v.videoCodecs, m.videoCodec)}
          options=${v.videoCodecs.map((c) => ({ value: c, label: t(c === 'Hevc' ? 'presets.video_codec.hevc' : c === 'Av1' ? 'presets.video_codec.av1' : p.targetFormat === 'webm' ? 'presets.video_codec.vp9' : 'presets.video_codec.h264') }))}
          onChange=${(x) => set((n) => { n.media.videoCodec = x; })} /></${Field}>`}
        ${v.showAudioCodec && html`<${Field} label=${t('presets.audio_codec')}><${Select} value=${effective(v.audioCodecs, m.audioCodec)}
          options=${v.audioCodecs.map((c) => ({ value: c, label: t(c === 'Alac' ? 'presets.audio_codec.alac' : 'presets.audio_codec.aac') }))} onChange=${(x) => set((n) => { n.media.audioCodec = x; })} /></${Field}>`}
        ${v.showAudioBitrate && html`<${Field} label=${t('presets.audio_bitrate')}><${Select} value=${m.audioBitrateKbps || 0} options=${steps(bitrateSteps, m.audioBitrateKbps || 0, (k) => (k ? k + ' kbps' : t('presets.audio_bitrate.auto')))} onChange=${(x) => set((n) => { n.media.audioBitrateKbps = x; })} /></${Field}>`}
        ${v.showAudioChannels && html`<${Field} label=${t('presets.audio_channels')}><${Select} value=${m.audioChannels || 0} options=${[0, 1, 2].map((c) => ({ value: c, label: t('presets.audio_channels.' + c) }))} onChange=${(x) => set((n) => { n.media.audioChannels = x; })} /></${Field}>`}
        ${v.showWav && html`<${Field} label=${t('presets.sample_rate')}><${Select} value=${m.audioSampleRate || 0} options=${steps(sampleRateSteps, m.audioSampleRate || 0, (r) => (r ? r / 1000 + ' kHz' : t('presets.sample_rate.source')))} onChange=${(x) => set((n) => { n.media.audioSampleRate = x; })} /></${Field}>
          <${Field} label=${t('presets.wav_format')}><${Select} value=${m.wavFormat} options=${opt('presets.wav_format.', ['Pcm16', 'Pcm8', 'Pcm24', 'Pcm32', 'Float32'])} onChange=${(x) => set((n) => { n.media.wavFormat = x; })} /></${Field}>`}
      </div>
      ${v.showRemoveAudio && html`<${Check} label=${t('presets.remove_audio')} checked=${m.removeAudio} onChange=${(x) => set((n) => { n.media.removeAudio = x; })} />`}
      ${v.showHardwareEncoding && html`<${Check} label=${t('presets.hardware_encoding')} hint=${t('presets.hardware_encoding_hint')} checked=${m.hardwareEncoding} onChange=${(x) => set((n) => { n.media.hardwareEncoding = x; })} />`}
    </${Card}>`}

    ${v.showArchive && html`<${Card} title=${t('presets.group.archive')}>
      ${v.showArchiveOptions && html`<${Field} label=${t('presets.archive_level')}><${Select} value=${p.archive.level} options=${opt('presets.archive_level.', ['Store', 'Fast', 'Normal', 'Maximum'])} onChange=${(x) => set((n) => { n.archive.level = x; })} /></${Field}>
        <${Check} label=${t('presets.combine')} checked=${p.archive.combineIntoOne} onChange=${(x) => set((n) => { n.archive.combineIntoOne = x; })} />`}
      ${v.showExtractNote && html`<p class="caption">${t('presets.extract_note')}</p>`}
    </${Card}>`}

    ${v.showTrace && html`<${Card} title=${t('presets.group.trace')} hint=${t('presets.trace_note')}>
      <div class="grid2">
        <${Field} label=${t('presets.trace_mode')}><${Select} value=${p.trace.mode} options=${opt('presets.trace_mode.', ['Color', 'BlackAndWhite'])} onChange=${(x) => set((n) => { n.trace.mode = x; })} /></${Field}>
        ${p.trace.mode === 'Color' && html`<${Slider} label=${t('presets.trace_colors')} min=${2} max=${64} value=${p.trace.colors} onChange=${(x) => set((n) => { n.trace.colors = x; })} />`}
      </div>
      <${Slider} label=${t('presets.trace_detail')} min=${0} max=${100} value=${p.trace.detail} onChange=${(x) => set((n) => { n.trace.detail = x; })} />
      <p class="caption">${t('presets.trace_detail_hint')}</p>
    </${Card}>`}

    ${v.showCad && html`<${Card} title=${t('presets.group.cad')}>
      <${Field} label=${t('presets.cad_version')} hint=${t('presets.cad_version_hint')}>
        <${Select} value=${p.cad.version || 'AutoCad2018'} options=${['AutoCad2018', 'AutoCad2013', 'AutoCad2010', 'AutoCad2007', 'AutoCad2004', 'AutoCad2000', 'SameAsSource']
          .map((x) => ({ value: x, label: x === 'SameAsSource' ? t('presets.cad_version.same') : x.replace('AutoCad', 'AutoCAD ') }))} onChange=${(x) => set((n) => { n.cad.version = x; })} /></${Field}>
    </${Card}>`}

    ${v.showLeaveOutPictures && html`<${Card} title=${t('presets.group.document')}>
      <${Check} label=${t('presets.leave_out_pictures')} hint=${t('presets.leave_out_pictures_hint')} checked=${p.document.leaveOutPictures} onChange=${(x) => set((n) => { n.document.leaveOutPictures = x; })} />
    </${Card}>`}

    ${v.showPdf && html`<${Card} title=${t('presets.group.pdf')}>
      ${v.showPdfBuild && html`<${Check} label=${t('presets.merge')} checked=${pdf.mergeIntoSingle} onChange=${(x) => set((n) => { n.pdf.mergeIntoSingle = x; })} />`}
      ${v.showPdfSplit && html`<${Check} label=${t('presets.split')} checked=${pdf.splitPages} onChange=${(x) => set((n) => { n.pdf.splitPages = x; })} />`}
      ${v.showPdfTablesNote && html`<p class="caption">${t('presets.pdf_tables_note')}</p>`}
      ${v.showPdfCompression && html`<${Field} label=${t('presets.pdf_compression')} hint=${t('presets.pdf_compression_hint')}>
          <${Select} value=${pdf.compression || 'None'} options=${opt('presets.pdf_compression.', ['None', 'Screen', 'Ebook', 'Print'])} onChange=${(x) => set((n) => { n.pdf.compression = x; })} /></${Field}>
        ${pdf.compression && pdf.compression !== 'None' && html`<div style="margin-left:23px" class="stack tight">
          <${Check} label=${t('presets.pdf_ghostscript')} hint=${t('presets.pdf_ghostscript_hint')} checked=${pdf.compressWithGhostscript} onChange=${(x) => set((n) => { n.pdf.compressWithGhostscript = x; })} />
          <${Check} label=${t('presets.pdf_keep_metadata')} checked=${im.keepMetadata} onChange=${(x) => set((n) => { n.image.keepMetadata = x; })} /></div>`}`}
      ${v.showOcr && html`<${Check} label=${t('presets.ocr')} hint=${t(isMac ? 'presets.ocr_hint_macos' : 'presets.ocr_hint')} checked=${pdf.ocr} onChange=${(x) => set((n) => { n.pdf.ocr = x; })} />
        ${pdf.ocr && html`<${Field} label=${t('presets.ocr_language')}><${Select} value=${pdf.ocrLanguage || 'eng'}
          options=${ocrLanguages.map(([code, culture, size]) => ({ value: code, label: nativeName(culture, code), detail: t('presets.ocr_language_size', fileSize(size)) }))}
          onChange=${(x) => set((n) => { n.pdf.ocrLanguage = x; })} /></${Field}>`}`}
      ${v.showPdfCombine && html`<${Check} label=${t('presets.combine_pages')} hint=${t('presets.combine_pages_hint')} checked=${pdf.combinePages} onChange=${(x) => set((n) => { n.pdf.combinePages = x; })} />
        ${pdf.combinePages && html`<div style="margin-left:23px" class="stack tight">
          <${Check} label=${t('presets.trim_page_margins')} checked=${pdf.trimPageMargins} onChange=${(x) => set((n) => { n.pdf.trimPageMargins = x; })} />
          <${Check} label=${t('presets.split_tall_image')} hint=${t('presets.split_tall_image_hint')} checked=${pdf.splitTallImage} onChange=${(x) => set((n) => { n.pdf.splitTallImage = x; })} /></div>`}`}
      ${v.showPageSize && html`<div class="grid2">
        <${Field} label=${t('presets.page_size')}><${Select} value=${pdf.pageSize} options=${opt('presets.page_size.', ['FitImage', 'A4', 'Letter'])} onChange=${(x) => set((n) => { n.pdf.pageSize = x; })} /></${Field}>
        ${pdf.pageSize !== 'FitImage' && html`<${Field} label=${t('presets.margin')}><${NumberInput} value=${pdf.marginMm || 0} min=${0} max=${100} onChange=${(x) => set((n) => { n.pdf.marginMm = x; })} /></${Field}>`}
      </div>`}
      ${v.showTurnForPdf && html`<div class="grid2">
        <${Field} label=${t('presets.rotation')}><${Select} value=${im.rotation || 'None'} options=${opt('presets.rotation.', ['None', 'Right90', 'Left90', 'Rotate180'])} onChange=${(x) => set((n) => { n.image.rotation = x; })} /></${Field}>
        <${Field} label=${t('presets.flip')}><${Select} value=${im.flip || 'None'} options=${opt('presets.flip.', ['None', 'Horizontal', 'Vertical'])} onChange=${(x) => set((n) => { n.image.flip = x; })} /></${Field}>
      </div>`}
      <div class="grid2">
        <${Field} label=${t('presets.render_dpi')}><${NumberInput} value=${pdf.renderDpi} min=${36} max=${1200} onChange=${(x) => set((n) => { n.pdf.renderDpi = x; })} /></${Field}>
        <${Field} label=${t('presets.page_range')}><${TextInput} value=${pdf.pageRange || ''} placeholder=${t('presets.page_range_hint')} onChange=${(x) => set((n) => { n.pdf.pageRange = x; })} /></${Field}>
      </div>
      ${v.showDocument && html`<${Check} label=${t('presets.pdfa')} checked=${p.document.pdfA} onChange=${(x) => set((n) => { n.document.pdfA = x; })} />`}
      ${v.showPdfBuild && html`<div class="grid2">
          <${Field} label=${t('presets.watermark')}><${TextInput} value=${pdf.watermark || ''} placeholder=${t('presets.watermark_hint')} onChange=${(x) => set((n) => { n.pdf.watermark = x; })} /></${Field}>
          <${Field} label=${t('presets.pdf_password')}><${TextInput} type="password" value=${pdf.password || ''} disabled=${p.document.pdfA} maxLength=${127} onChange=${(x) => set((n) => { n.pdf.password = x; })} /></${Field}>
        </div>
        <p class="caption">${t('presets.pdf_password_hint')}</p>
        <${Check} label=${t('presets.page_numbers')} checked=${pdf.pageNumbers} onChange=${(x) => set((n) => { n.pdf.pageNumbers = x; })} />`}
    </${Card}>`}

    ${v.showCadColors && html`<${Card} title=${t('presets.group.cad')}>
      <${Field} label=${t('presets.cad_colors')} hint=${t('presets.cad_colors_hint')}>
        <${Select} value=${p.cad.colors || 'AsDrawn'} options=${opt('presets.cad_colors.', ['AsDrawn', 'BlackAndWhite', 'Grayscale'])} onChange=${(x) => set((n) => { n.cad.colors = x; })} /></${Field}>
    </${Card}>`}

    ${v.showZipResult || true ? html`<${Card} title=${t('presets.group.output')}>
      <div class="grid2">
        <${Field} label=${t('presets.location')}><${Select} value=${ui.location} options=${opt('presets.location.', ['Default', 'SameFolder', 'Subfolder', 'CustomFolder', 'Ask'])} onChange=${(x) => setU({ location: x })} /></${Field}>
        <${Field} label=${t('presets.conflict')}><${Select} value=${o.conflict} options=${opt('presets.conflict.', ['Rename', 'Overwrite', 'Skip'])} onChange=${(x) => set((n) => { n.output.conflict = x; })} /></${Field}>
      </div>
      ${ui.location === 'Subfolder' && html`<${Field} label=${t('presets.subfolder')}><${TextInput} value=${o.subfolderName} onChange=${(x) => set((n) => { n.output.subfolderName = x; })} /></${Field}>
        <${Check} label=${t('presets.subfolder_only_several')} hint=${t('presets.subfolder_only_several_hint')} checked=${o.subfolderOnlyForSeveralFiles} onChange=${(x) => set((n) => { n.output.subfolderOnlyForSeveralFiles = x; })} />`}
      ${ui.location === 'CustomFolder' && html`<${Field} label=${t('presets.custom_folder')}><${FolderField} value=${o.customFolder} onChange=${(x) => set((n) => { n.output.customFolder = x; })} /></${Field}>`}
      <${Field} label=${t('presets.pattern')} hint=${t('presets.pattern_hint')}><${TextInput} value=${o.fileNamePattern} onChange=${(x) => set((n) => { n.output.fileNamePattern = x; })} /></${Field}>
      <${Check} label=${t('presets.keep_dates')} hint=${t('presets.keep_dates_hint')} checked=${o.keepDates} onChange=${(x) => set((n) => { n.output.keepDates = x; })} />
      ${v.showZipResult && html`<${Check} label=${t('presets.zip_result')} hint=${t('presets.zip_result_hint')} checked=${o.zipResult} onChange=${(x) => set((n) => { n.output.zipResult = x; })} />`}
      <${Check} label=${t('presets.replace_original')} hint=${t('presets.replace_original_hint')} checked=${o.replaceOriginal} onChange=${(x) => set((n) => { n.output.replaceOriginal = x; })} />
    </${Card}>` : null}
  </div>`;
}

function withName(p) { return { ...structuredClone(p), name: p.name || presetName(p) }; }

/** savePreset applies the edits and saves; returns the saved preset. */
export async function saveEdited(edited, orig, ui) {
  if (!edited.name.trim()) { notify(t('presets.name_required'), 'danger'); return null; }
  const out = applyEdits(edited, orig, ui);
  return guard(() => api.put('api/presets/' + orig.id, out));
}

/** editPresetDialog opens the editor in a dialog (the donut's ✎). */
export function editPresetDialog(preset, { isNew } = {}) {
  let state = null;
  return showDialog((close) => html`<div class="stack">
    <h2>${t('presets.editor_title')}</h2>
    <div style="max-height:65vh;overflow:auto;padding-right:4px"><${PresetEditor} preset=${preset} onState=${(p, ui) => { state = { p, ui }; }} /></div>
    <div class="dialog-buttons">
      <${Button} onClick=${() => close(null)}>${t('common.cancel')}</${Button}>
      <${Button} kind="accent" onClick=${async () => { const r = await saveEdited(state.p, preset, state.ui); if (r) close(r); }}>${t('presets.save')}</${Button}>
    </div></div>`, { wide: true }).then(async (r) => {
    if (!r && isNew) await guard(() => api.del('api/presets/' + preset.id));
    return r;
  });
}

/** newPresetDialog makes an empty preset (PNG) and edits it; null when cancelled. */
export async function newPresetDialog() {
  const p = await guard(() => api.post('api/presets?new=1', { name: t('presets.add_empty'), targetFormat: 'png', kind: 'Convert' }));
  if (!p) return null;
  return editPresetDialog(p, { isNew: true });
}

/** editGroupDialog edits a group: its name and up to six presets (GroupEditorWindow). */
export function editGroupDialog(group) {
  const max = store.boot.limits.maxSubPresets;
  return showDialog((close) => html`<${GroupEditor} group=${group} max=${max} close=${close} />`);
}

function GroupEditor({ group, max, close }) {
  const [name, setName] = useState(group.name || presetName(group));
  const [ids, setIds] = useState(group.subPresetIds || []);
  const [pick, setPick] = useState('');
  const all = (store.library?.presets || []).filter((p) => p.kind !== 'Group' && p.id !== group.id);
  const find = (id) => all.find((p) => p.id === id);
  const save = async () => {
    const g = { ...structuredClone(group), name: name.trim() || presetName(group), subPresetIds: ids };
    if (await guard(() => api.put('api/presets/' + group.id, g))) close(g);
  };
  const del = async () => {
    if (await confirmDialog(t('group.delete_confirm', presetName(group)), { danger: true, ok: t('common.delete') })) {
      await guard(() => api.del('api/presets/' + group.id));
      close(null);
    }
  };
  return html`<div class="stack">
    <h2>${t('group.editor_title')}</h2>
    <p class="caption">${t('group.hint')}</p>
    <${Field} label=${t('presets.name')}><input class="input" value=${name} onInput=${(e) => setName(e.currentTarget.value)} /></${Field}>
    <span class="label">${t('group.members', ids.length, max)}</span>
    ${ids.length === 0 ? html`<p class="muted">${t('group.empty')}</p>` : html`<ul class="list">${ids.map((id, i) => html`<li>
      <span class="grow">${presetName(find(id) || { targetFormat: id })}</span>
      <${IconButton} icon="ArrowUp" title=${t('group.move_up')} disabled=${i === 0} onClick=${() => { const n = [...ids]; [n[i - 1], n[i]] = [n[i], n[i - 1]]; setIds(n); }} />
      <${IconButton} icon="ArrowDown" title=${t('group.move_down')} disabled=${i === ids.length - 1} onClick=${() => { const n = [...ids]; [n[i + 1], n[i]] = [n[i], n[i + 1]]; setIds(n); }} />
      <${IconButton} icon="Close" title=${t('group.remove')} onClick=${() => setIds(ids.filter((x) => x !== id))} /></li>`)}</ul>`}
    ${ids.length >= max ? html`<p class="caption">${t('group.full', max)}</p>` : html`<div class="row">
      <select class="input grow" style="flex:1" value=${pick} onChange=${(e) => setPick(e.currentTarget.value)} aria-label=${t('group.add_placeholder')}>
        <option value="">${t('group.add_placeholder')}</option>
        ${all.filter((p) => !ids.includes(p.id)).map((p) => html`<option value=${p.id}>${presetName(p)}</option>`)}
      </select>
      <${Button} disabled=${!pick} onClick=${() => { setIds([...ids, pick]); setPick(''); }}>${t('group.add')}</${Button}></div>`}
    <div class="dialog-buttons">
      <${Button} kind="danger" onClick=${del}>${t('group.delete')}</${Button}>
      <span class="spacer"></span>
      <${Button} onClick=${() => close(null)}>${t('common.cancel')}</${Button}>
      <${Button} kind="accent" onClick=${save}>${t('common.save')}</${Button}>
    </div>
  </div>`;
}
