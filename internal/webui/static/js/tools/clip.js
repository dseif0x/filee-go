// Cut a clip (ClipWindowViewModel, ClipTimeline, ClipSequence): the video
// plays in a <video> served from the file itself (range requests) or from a
// preview FFmpeg makes; the cutting is the server's ClipCutter, so every
// edit, trim and undo works exactly as in the original.
import { html, useState, useEffect, useRef, Button, IconButton, Check, Select, Slider, Spinner, Icon, pickFiles, guard, notify } from '../ui.js';
import { api, store } from '../api.js';
import { t } from '../i18n.js';
import { useTool, currentIndex, ToolHead, Queue, Summary, finished, ToolError } from './common.js';

function display(s) {
  if (!(s > 0)) s = 0;
  const tenths = Math.round(s * 10);
  const h = Math.floor(tenths / 36000);
  const m = Math.floor(tenths / 600) % 60;
  const sec = Math.floor(tenths / 10) % 60;
  const d = tenths % 10;
  const mm = h ? String(m).padStart(2, '0') : String(m);
  return (h ? h + ':' : '') + mm + ':' + String(sec).padStart(2, '0') + '.' + d;
}

function parseTime(text) {
  const parts = text.trim().split(':').map(Number);
  if (parts.some((p) => !Number.isFinite(p))) return NaN;
  return parts.reduce((a, p) => a * 60 + p, 0);
}

function TimeField({ label, value, onCommit }) {
  const [v, setV] = useState(display(value));
  useEffect(() => setV(display(value)), [value]);
  return html`<label class="field"><span class="label">${label}</span>
    <input class="input time" style="width:110px" value=${v} onInput=${(e) => setV(e.currentTarget.value)}
      onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); const s = parseTime(v); if (Number.isFinite(s)) onCommit(s); } }}
      onBlur=${() => { const s = parseTime(v); if (Number.isFinite(s) && Math.abs(s - value) > 0.04) onCommit(s); else setV(display(value)); }} /></label>`;
}

export function ClipTool({ job: id }) {
  const [job, , error] = useTool(id);
  const i = job ? currentIndex(job) : -1;
  const video = useRef();
  const timeline = useRef();
  const seqRef = useRef();
  const [st, setSt] = useState(null);
  const [mediaError, setMediaError] = useState('');
  const [head, setHead] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [sel, setSel] = useState([]);
  const [fast, setFast] = useState(true);
  const [busy, setBusy] = useState('');
  const [zoom, setZoom] = useState(true);
  const drag = useRef(null);
  const base = job && i >= 0 ? `api/tools/${id}/files/${i}` : '';

  const op = async (body) => {
    const r = await guard(() => api.post(base + '/clip', body));
    if (r) { setSt(r); if (r.message) notify(r.message, r.result === false ? 'danger' : ''); }
    return r;
  };
  useEffect(() => { if (job) setFast(!job.preset.media.exactCut); }, [job?.id]);
  // Drags send many moves: one request at a time, the latest move waiting.
  const flight = useRef({ busy: false, next: null });
  const opDrag = (body) => {
    const f = flight.current;
    if (f.busy) { f.next = body; return Promise.resolve(null); }
    f.busy = true;
    return op(body).finally(() => {
      f.busy = false;
      const n = f.next;
      f.next = null;
      if (n) opDrag(n);
    });
  };
  const init = (duration) => op({ op: 'init', duration: duration || 0 }).then((r) => { if (!r) setMediaError(t('clip.cannot_play')); });
  // The next video: a fresh start (its metadata may already be there).
  useEffect(() => {
    setSt(null); setMediaError(''); setSel([]); setHead(0); setPlaying(false);
    const v = video.current;
    if (i >= 0 && v && v.readyState >= 1 && Number.isFinite(v.duration)) init(v.duration);
  }, [i]);

  // Playing: within the clip, past cut-out pieces, at each piece's speed and volume.
  useEffect(() => {
    const v = video.current;
    if (!v || !st) return undefined;
    let raf = 0;
    const tick = () => {
      const now = v.currentTime;
      setHead(now);
      if (!v.paused) {
        const p = st.pieces.find((x) => now >= x.start - 1e-3 && now < x.end);
        if (!p || now >= st.end) {
          const next = st.pieces.find((x) => x.start >= now && x.state !== 'removed');
          if (!next || now >= st.end) { v.pause(); setPlaying(false); }
          else v.currentTime = next.start;
        } else if (p.state === 'removed') {
          const next = st.pieces.find((x) => x.start >= p.end - 1e-3 && x.state !== 'removed');
          if (next) v.currentTime = next.start; else { v.pause(); setPlaying(false); }
        } else {
          v.playbackRate = p.speed || 1;
          v.volume = Math.min(1, p.muted ? 0 : p.volume);
          v.muted = p.muted || p.state === 'blank';
        }
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [st]);

  const seek = (s) => { if (video.current) video.current.currentTime = Math.max(0, Math.min(st?.duration || 0, s)); setHead(s); };
  const play = () => {
    const v = video.current;
    if (!v || !st) return;
    if (!v.paused) { v.pause(); setPlaying(false); return; }
    if (head < st.start || head >= st.end - 0.05) v.currentTime = st.start;
    v.play().then(() => setPlaying(true)).catch(() => notify(t('clip.cannot_play'), 'danger'));
  };
  const pieceAt = (s) => (st ? st.pieces.findIndex((p) => s >= p.start && s < p.end) : -1);
  const selected = sel.length ? sel : pieceAt(head) >= 0 ? [pieceAt(head)] : [];
  const cur = selected.length ? st?.pieces[selected[0]] : null;

  const save = async () => {
    if (!st) return;
    setBusy('save');
    await guard(() => api.post(base + '/clip/save', { fast }));
    setBusy('');
  };
  const removeSilence = async () => { setBusy('silence'); await op({ op: 'remove-silence' }); setBusy(''); };
  const addMusic = async () => {
    const p = await pickFiles({ multiple: false, title: t('clip.music_pick') });
    if (!p) return;
    setBusy('music');
    await op({ op: 'music', path: p });
    setBusy('');
  };

  // Timeline: the whole video (or zoomed on the clip), handles for start and end, the playhead.
  const span = st ? (zoom ? [Math.max(0, st.start - (st.end - st.start) * 0.1), Math.min(st.duration, st.end + (st.end - st.start) * 0.1)] : [0, st.duration]) : [0, 1];
  const toX = (s) => ((s - span[0]) / Math.max(1e-6, span[1] - span[0])) * 100;
  const fromEvent = (e, el) => {
    const b = el.getBoundingClientRect();
    return span[0] + ((e.clientX - b.left) / b.width) * (span[1] - span[0]);
  };
  const tlDown = (e, what, k) => {
    e.preventDefault(); e.stopPropagation();
    drag.current = { what, k, join: 'drag-' + Math.random().toString(36).slice(2), x0: fromEvent(e, timeline.current), start: st.start, el: timeline.current, end: e.shiftKey };
    e.currentTarget.setPointerCapture?.(e.pointerId);
    if (what === 'head') seek(fromEvent(e, timeline.current));
  };
  const tlMove = (e) => {
    const d = drag.current;
    if (!d) return;
    const s = fromEvent(e, d.el);
    if (d.what === 'head') seek(s);
    else if (d.what === 'start') opDrag({ op: 'move-start', at: s, join: d.join });
    else if (d.what === 'end') opDrag({ op: 'move-end', at: s, join: d.join });
    else if (d.what === 'move') opDrag({ op: 'move', at: d.start + (s - d.x0), join: d.join });
    else if (d.what === 'trim') opDrag({ op: 'trim', index: d.k, end: d.endEdge, at: s, join: d.join });
    else if (d.what === 'music') opDrag({ op: 'move-music', at: Math.max(0, d.offset + (s - d.x0)), join: d.join });
  };
  const tlUp = () => { drag.current = null; };

  useEffect(() => {
    const on = (e) => {
      if (!st || e.target.closest?.('input,select,textarea') || document.querySelector('.modal-backdrop')) return;
      const mod = e.ctrlKey || e.metaKey;
      const k = e.key.toLowerCase();
      if (e.key === ' ') { e.preventDefault(); play(); }
      else if (mod && k === 'z') { e.preventDefault(); op({ op: e.shiftKey ? 'redo' : 'undo' }); }
      else if (mod && k === 'y') { e.preventDefault(); op({ op: 'redo' }); }
      else if (mod && k === 'a') { e.preventDefault(); setSel(st.pieces.map((_, n) => n)); }
      else if (k === 'c' && !mod) op({ op: 'split', at: head });
      else if (e.key === 'Delete' || e.key === 'Backspace') { if (selected.length) { op({ op: 'remove', indices: selected, closeUp: e.shiftKey }); setSel([]); } }
      else if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); seek(head + (e.key === 'ArrowLeft' ? -1 : 1) * (e.shiftKey ? 1 : 0.1)); }
      else if (k === 's' && !mod) op({ op: 'move-start', at: head });
      else if (k === 'e' && !mod) op({ op: 'move-end', at: head });
      else if (k === 'z' && !mod) setZoom(!zoom);
      else if (e.key === 'Enter') { e.preventDefault(); save(); }
    };
    window.addEventListener('keydown', on);
    return () => window.removeEventListener('keydown', on);
  });

  if (error) return html`<${ToolError} error=${error} />`;
  if (!job) return html`<div class="tool-missing"><${Spinner} size=${28} /></div>`;
  const file = job.files[i];
  const next = job.files.slice(i + 1).some((f) => f.state === 'waiting');
  const speeds = store.boot.limits.clipSpeeds;
  return html`<div class="tool">
    <${ToolHead} title=${file?.name || job.name} job=${job} index=${i} />
    ${finished(job) ? html`<div class="tool-body"><div class="tool-stage"><${Summary} job=${job} /></div></div>` : i < 0 ? html`<div class="tool-body"><div class="tool-stage"><${Spinner} size=${28} /></div></div>` : html`
    <div class="tool-body" style="flex-direction:column">
      <div class="clip-stage">
        <video ref=${video} src=${base + '/clip/media'} preload="auto" playsinline
          onLoadedMetadata=${(e) => init(e.currentTarget.duration)}
          onError=${() => { setMediaError(t('clip.cannot_play')); if (!st) init(0); }}></video>
        ${mediaError && html`<div class="stage-hint" style="bottom:auto;top:12px">${mediaError}</div>`}
      </div>
      ${!st ? html`<div class="clip-controls"><div class="row">${mediaError ? html`<span class="danger">${mediaError}</span>` : html`<${Spinner} /> <span class="muted">${t('clip.preparing')}</span>`}</div></div>` : html`
      <div class="clip-controls">
        <div class="row">
          <${Button} kind="accent" onClick=${play} title=${playing ? t('clip.pause') : t('clip.play')}>${playing ? '❚❚' : '▶'}</${Button}>
          <span class="time">${display(head)} / ${st.display.duration}</span>
          <span class="spacer"></span>
          <${TimeField} label=${t('clip.start')} value=${st.start} onCommit=${(s) => op({ op: 'move-start', at: s })} />
          <${TimeField} label=${t('clip.end')} value=${st.end} onCommit=${(s) => op({ op: 'move-end', at: s })} />
          <${TimeField} label=${t('clip.length')} value=${st.length} onCommit=${(s) => op({ op: 'set-length', value: s })} />
        </div>
        <div class="timeline" ref=${timeline} aria-label=${t('clip.timeline')} onPointerDown=${(e) => tlDown(e, 'head')} onPointerMove=${tlMove} onPointerUp=${tlUp}>
          <div class="range" style=${`left:${toX(st.start)}%;width:${toX(st.end) - toX(st.start)}%`} onPointerDown=${(e) => tlDown(e, 'move')}></div>
          <div class="knob-h" style=${`left:${toX(st.start)}%`} title=${t('clip.start')} onPointerDown=${(e) => tlDown(e, 'start')}></div>
          <div class="knob-h" style=${`left:${toX(st.end)}%`} title=${t('clip.end')} onPointerDown=${(e) => tlDown(e, 'end')}></div>
          <div class="playhead" style=${`left:${toX(head)}%`}></div>
        </div>
        <div class="sequence" ref=${seqRef} aria-label=${t('clip.sequence')} onPointerMove=${tlMove} onPointerUp=${tlUp}
            onPointerDown=${(e) => { if (e.target === e.currentTarget) { drag.current = { what: 'head', el: seqRef.current }; seek(fromEvent(e, seqRef.current)); } }}>
          ${st.pieces.map((p, k) => p.state === 'removed' ? null : html`<div class=${'piece' + (p.state === 'blank' ? ' blank' : '') + (p.muted ? ' muted' : '') + (selected.includes(k) ? ' on' : '')}
              style=${`left:${toX(p.start)}%;width:${toX(p.end) - toX(p.start)}%`}
              onPointerDown=${(e) => { e.stopPropagation(); setSel(e.ctrlKey || e.metaKey || e.shiftKey ? (sel.includes(k) ? sel.filter((x) => x !== k) : [...sel, k]) : [k]); seek(Math.max(p.start, head < p.start || head >= p.end ? p.start : head)); }}>
            <span class="edge l" onPointerDown=${(e) => { tlDown(e, 'trim', k); drag.current.el = seqRef.current; drag.current.endEdge = false; }}></span>
            ${p.state === 'blank' ? html`<button type="button" class="btn small" onClick=${() => op({ op: 'close-gap', index: k })}>${t('clip.close_gap')}</button>`
              : html`${p.speed !== 1 ? p.speed + '× ' : ''}${p.muted ? '🔇 ' : p.volume !== 1 ? Math.round(p.volume * 100) + '% ' : ''}${display((p.end - p.start) / (p.speed || 1))}`}
            <span class="edge r" onPointerDown=${(e) => { tlDown(e, 'trim', k); drag.current.el = seqRef.current; drag.current.endEdge = true; }}></span>
          </div>`)}
          ${st.music ? html`<div class="music" style=${`left:${toX(st.start + (st.music.offset || 0))}%;width:${Math.max(2, toX(st.start + (st.music.offset || 0) + Math.min(st.music.length - (st.music.from || 0), st.length)) - toX(st.start + (st.music.offset || 0)))}%`}
              onPointerDown=${(e) => { tlDown(e, 'music'); drag.current.el = seqRef.current; drag.current.x0 = fromEvent(e, seqRef.current); drag.current.offset = st.music.offset || 0; }}
              title=${st.music.path}>♪ ${st.music.path.split(/[\\/]/).pop()}</div>`
            : html`<div class="caption" style="position:absolute;bottom:4px;left:8px">${t('clip.no_music_yet')}</div>`}
          <div class="seq-head" style=${`left:${toX(head)}%`}></div>
        </div>
        <div class="row tight">
          <${Button} icon="Cut" title=${t('clip.cut_hint')} onClick=${() => op({ op: 'split', at: head })}>${t('clip.cut')}</${Button}>
          ${cur?.state === 'kept' || !cur ? html`<${Button} icon="Delete" title=${t('clip.delete_hint')} disabled=${!selected.length}
              onClick=${(e) => { op({ op: 'remove', indices: selected, closeUp: e.shiftKey }); setSel([]); }}>${t('clip.delete')}</${Button}>`
            : html`<${Button} onClick=${() => op({ op: 'restore', index: selected[0] })}>${t('clip.restore')}</${Button}>`}
          <${Button} title=${t('clip.mute_hint')} disabled=${!cur} onClick=${() => op({ op: 'mute', indices: selected, muted: !cur?.muted })}>${cur?.muted ? t('clip.unmute') : t('clip.mute')}</${Button}>
          <${Button} disabled=${!st.hasGaps} title=${t('clip.close_gaps_hint')} onClick=${() => op({ op: 'close-gaps' })}>${t('clip.close_gaps')}</${Button}>
          <${Button} disabled=${!!busy} title=${t('clip.remove_silence_hint')} onClick=${removeSilence}>${busy === 'silence' ? t('clip.finding_silence') : t('clip.remove_silence')}</${Button}>
          <span class="spacer"></span>
          <${IconButton} icon="Undo" title=${t('clip.undo')} disabled=${!st.canUndo} onClick=${() => op({ op: 'undo' })} />
          <${Button} kind="ghost" title=${t('clip.redo')} disabled=${!st.canRedo} onClick=${() => op({ op: 'redo' })}>↻</${Button}>
          <${Button} kind=${zoom ? 'ghost on' : 'ghost'} title=${t('clip.zoom_hint')} onClick=${() => setZoom(!zoom)}>${t('clip.zoom')}</${Button}>
        </div>
        <div class="grid4" style="align-items:end">
          <${Slider} label=${t('clip.volume')} min=${0} max=${200} value=${Math.round((cur?.volume ?? 1) * 100)} disabled=${!cur} valueText=${(v) => v + ' %'}
            onChange=${(v) => op({ op: 'volume', indices: selected, value: v / 100 })} />
          <label class="field"><span class="label">${t('clip.speed')}</span>
            <${Select} value=${cur?.speed ?? 1} disabled=${!cur} options=${speeds.map((s) => ({ value: s, label: s + '×' }))}
              onChange=${(v) => op({ op: 'speed', indices: selected, value: v })} /></label>
          <div class="stack tight">
            <${Check} label=${t('clip.fade_in')} checked=${st.fadeIn > 0} onChange=${(v) => op({ op: 'fade-in', value: v ? 1 : 0 })} />
            <${Check} label=${t('clip.fade_out')} checked=${st.fadeOut > 0} onChange=${(v) => op({ op: 'fade-out', value: v ? 1 : 0 })} />
          </div>
          <div class="stack tight">
            ${st.music ? html`<${Slider} label=${t('clip.music')} min=${0} max=${200} value=${Math.round((st.music.volume ?? 0.6) * 100)} valueText=${(v) => v + ' %'}
                onChange=${(v) => op({ op: 'music-volume', value: v / 100 })} />
              <${Button} kind="ghost" onClick=${() => op({ op: 'music', path: '' })}>${t('clip.remove_music')}</${Button}>`
              : html`<${Button} title=${t('clip.music_hint')} disabled=${!!busy} onClick=${addMusic}>${busy === 'music' ? t('clip.loading_music') : t('clip.music') + '…'}</${Button}>`}
          </div>
        </div>
        <p class="caption">${t('clip.hint_keys')}</p>
      </div>`}
    </div>
    <${Queue} job=${job} />
    <footer class="tool-foot">
      <${Check} label=${t('clip.fast')} checked=${fast && (st?.plain ?? true)} disabled=${st && !st.plain} onChange=${setFast} />
      ${st && !st.plain && html`<span class="caption">${t('clip.fast_needs_encode')}</span>`}
      <span class="spacer"></span>
      ${selected.length > 1 && html`<span class="caption">${t('clip.selected_count', selected.length)}</span>`}
      <${Button} onClick=${() => guard(() => api.post(base + '/skip'))}>${t('crop.skip')}</${Button}>
      <${Button} kind="accent" icon="Cut" disabled=${!st || busy === 'save'} onClick=${save}>${t(next ? 'clip.save_next' : 'clip.save')}</${Button}>
    </footer>`}
  </div>`;
}
