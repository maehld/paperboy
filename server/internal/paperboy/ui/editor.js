// Page editor for Paperboy (ADR 0068). Vanilla JavaScript, no build step: the
// page model and the plugin schema come from the page API (ADR 0066), edits
// stay in the browser until Save writes the file with the hash it was read
// with.
(() => {
  'use strict';

  const el = (id) => document.getElementById(id);
  const start = JSON.parse(el('editor-data').textContent);

  // pageHref builds the route of one page file (ADR 0152): the name sits in the
  // path, the rest stays in the query.
  function pageHref(name, params) {
    const rest = params ? params.toString() : '';
    return '/ui/pages/' + encodeURIComponent(name) + (rest ? '?' + rest : '');
  }

  const state = {
    name: start.name,
    display: start.display, // preview display type
    kind: start.kind, // image or text: the kind of the open file
    width: start.width, // characters of a text display, pixels of an image display
    height: start.height,
    reference: start.reference, // grid plugins declare their sizes on
    grid: start.grid, // grid a new page starts with
    file: start.file || '', // label of the open file; empty: the page file
    pageHash: start.page_hash || '', // hash of the page file, for a new variant
    pageTitle: start.page_title || '', // the title of the page file, inherited by a variant (ADR 0110)
    pageExists: !!start.page_exists, // the page file exists
    displays: start.displays || [], // configured display types (ADR 0110)
    named: !!start.named, // the page names this variant
    widgetsPage: !!start.widgets_page, // the widgets page: one widget, no page
    usedBy: start.usedBy || [], // plans that name the page, for the delete warning
    rename: start.rename || null, // endpoint and confirmation for a rename (ADR 0079)
    orphan: start.orphan || '', // the variant has no page file (ADR 0110)
    hash: '',
    template: null,
    readOnly: false,
    selected: -1,
    drag: null, // {kind, i, axis, grab, at, span, start, moved} while a box moves or resizes
    placing: null, // {plugin, widget} or {plugin, name, value: true} waiting for a cell
    filter: '', // text of the palette filter
    collapsed: new Set(), // plugins whose values the palette hides
    collapsedSections: new Set(), // shortcut groups the palette folds (Pinned)
    openPlugin: '', // the one plugin group the widget palette shows (ADR 0152)
    openResolved: false, // the palette picked its group after the plugins answered
    plugins: [],
    dirty: false,
    previewURL: '',
    previewTimer: 0,
    signedOut: false, // the UI password changed: stop previews until the page reloads (ADR 0096)
    previewKind: '', // '' none, 'image' or 'text'
    previewLines: [], // the text lines of a text display, one char per cell
    gridLock: false, // keep the grid ratio when one axis changes (ADR 0128)
    gridRatio: null, // [columns, rows] the lock derives from, captured when it is switched on
  };

  let suppressPaletteClick = false; // a finished palette drag must not toggle placing

  const text = () => state.kind === 'text';
  // A text page is a layout of rows of values (ADR 0111). The palette offers
  // the declared values, and the inspector edits rows and cells.
  const rowsPage = () => !state.widgetsPage && text();
  const textCellsValid = () => {
    const t = state.template;
    if (!t || !Array.isArray(t.cells) || t.cells.length === 0) return false;
    for (const c of t.cells) {
      const at = c.at, span = c.span;
      if (!Array.isArray(at) || !Array.isArray(span)) return false;
      if (at[0] < 0 || at[1] < 0 || span[0] < 1 || span[1] < 1 ||
        at[0] + span[0] > state.width || at[1] + span[1] > state.height) return false;
      if ((c.text || '') === (c.value || '')) return false;
    }
    for (let i = 0; i < t.cells.length; i++) {
      const a = t.cells[i];
      for (let j = i + 1; j < t.cells.length; j++) {
        const b = t.cells[j];
        if (a.at[0] < b.at[0] + b.span[0] && b.at[0] < a.at[0] + a.span[0] &&
          a.at[1] < b.at[1] + b.span[1] && b.at[1] < a.at[1] + a.span[1]) return false;
      }
    }
    return true;
  };
  const targetPath = () => 'pages/' + state.name + (state.file ? '.' + state.file : '') + '.yaml';

  // ---- status and where ----

  function setStatus(message, kind) {
    const s = el('status');
    s.textContent = message || '';
    s.className = 'status' + (kind ? ' ' + kind : '');
  }

  // updateWhere names what the widgets page shows; the page editor keeps its
  // bar to the name and the actions (ADR 0093).
  function updateWhere() {
    if (!state.widgetsPage) return;
    const c = state.template && state.template.cells[0];
    el('preview-where').textContent = [
      c ? c.widget + ' · ' + c.span[0] + '×' + c.span[1] : 'no widget yet',
      state.display + ' display',
    ].join(' · ');
  }

  // updateFile names the open file behind the page name: nothing for the page
  // file, the label for a variant (ADR 0110).
  function updateFile() {
    const label = el('file-label');
    if (label) label.textContent = state.file;
  }

  // orphaned reports whether the open variant has no page file any more; the
  // editor names it and the pages list offers to assign it (ADR 0110).
  function orphaned() {
    return !!state.orphan;
  }

  function updateSave() {
    const content = rowsPage() ? textCellsValid() : !!state.template && (!state.widgetsPage || state.template.cells.length > 0);
    const hasCell = !!state.template && !state.readOnly && content;
    // The page editor saves, the widgets page copies the cell: one button each.
    const save = el('save');
    if (save) save.disabled = !hasCell;
    const copy = el('copy-cell');
    if (copy) copy.disabled = !hasCell;
    const del = el('delete');
    if (del) del.hidden = state.widgetsPage || !state.hash;
    const rename = el('rename');
    if (rename) rename.hidden = state.widgetsPage || !state.rename;
  }

  // Rebuilding the board and the preview after a change, without touching the
  // inputs the operator is typing in: `origin` names the panel the edit came
  // from, and that panel keeps its elements (and so its focus and caret).
  function changed(origin) {
    if (!state.widgetsPage) {
      state.dirty = true;
      setStatus('unsaved changes', '');
      updateWhere();
    } else {
      updateURL();
      updateWhere();
    }
    updateSave();
    renderBoard();
    if (origin === 'inspector') updateSnippet();
    else {
      renderInspector();
    }
    schedulePreview();
  }

  // ---- model ----

  function blank() {
    if (rowsPage()) {
      return {
        title: state.file ? '' : state.name,
        kind: 'text',
        overflow: 'strip',
        cells: [{ at: [0, 0], span: [Math.max(1, state.width), 1], text: 'Text' }],
        style: {},
      };
    }
    const grid = text() ? [state.width, state.height] : state.grid.slice();
    return { title: state.file ? '' : state.name, kind: state.kind, grid: grid, style: {}, cells: [] };
  }

  // normalize keeps the model in the shape serialize writes back: two-element
  // coordinates, args, styles and the variants map present.
  function normalize(t) {
    if (rowsPage()) {
      t.kind = 'text';
      t.grid = [state.width, state.height];
      t.style = t.style || {};
      t.variants = t.variants || {};
      t.overflow = t.overflow || 'strip';
      t.cells = (t.cells || []).map((c) => {
        const at = Array.isArray(c.at) && c.at.length === 2 ? [Math.round(Number(c.at[0])) || 0, Math.round(Number(c.at[1])) || 0] : [0, 0];
        const span = Array.isArray(c.span) && c.span.length === 2 ? [Math.max(1, Math.round(Number(c.span[0])) || 1), Math.max(1, Math.round(Number(c.span[1])) || 1)] : [1, 1];
        return {
          at,
          span,
          text: c.text || '',
          value: c.value || '',
          label: c.label || '',
          labels: c.labels && typeof c.labels === 'object' ? c.labels : {},
          unit: c.unit || '',
          scale: typeof c.scale === 'string' ? c.scale : '',
          flow: c.flow === 'arrow' || (c.flow && typeof c.flow === 'object' && c.flow.out && c.flow.in) ? c.flow : null,
          align: c.align === 'right' || c.align === 'decimal' ? c.align : 'left',
          overflow: c.overflow === 'wrap' || c.overflow === 'scroll' ? c.overflow : '',
          args: c.args || {},
        };
      });
      return t;
    }
    t.kind = t.kind || state.kind;
    t.grid = Array.isArray(t.grid) && t.grid.length === 2 ? [t.grid[0], t.grid[1]] : state.grid.slice();
    t.style = t.style || {};
    t.variants = t.variants || {};
    t.cells = (t.cells || []).map((c) => ({
      at: [c.at[0], c.at[1]],
      span: Array.isArray(c.span) && c.span.length === 2 ? [c.span[0], c.span[1]] : [1, 1],
      widget: c.widget,
      args: c.args || {},
      style: c.style || {},
    }));
    return t;
  }

  // mapSpan turns a span of the 6×8 reference grid into the page grid, so a
  // widget that asks for half the width keeps half the width on 8×6 or 10×6
  // as well (ADR 0128). It rounds and clamps to the grid.
  function mapSpan(span, grid) {
    const axis = (cells, want, of) => Math.max(1, Math.min(want, Math.round(cells * want / of)));
    return [axis(span[0], grid[0], 6), axis(span[1], grid[1], 8)];
  }

  // cellGrid is the grid the cells are placed on: the open file's, or the
  // display type's on the widgets page.
  function cellGrid() {
    return state.template && state.template.grid ? state.template.grid : state.grid;
  }

  function num(value) {
    if (value === '' || value === null || value === undefined) return null;
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
  }

  function styleOut(style) {
    const out = {};
    const padding = num(style.padding);
    if (padding !== null) out.padding = padding;
    for (const key of ['rule', 'text_size', 'divider']) {
      const v = num(style[key]);
      if (v) out[key] = v;
    }
    return out;
  }

  // serialize builds the body of PUT /api/pages: only the fields the API
  // knows, with the defaults Page.Encode leaves out. The page file carries the
  // title and the variants map; a variant carries neither (ADR 0110).
  function serialize() {
    const t = state.template;
    const out = rowsPage()
      ? { kind: 'text', overflow: t.overflow || 'strip', style: styleOut(t.style || {}), cells: t.cells.map((c) => {
        const cell = { at: [c.at[0], c.at[1]], span: [c.span[0], c.span[1]] };
        if (c.text) cell.text = c.text;
        else {
          cell.value = c.value;
          if (c.label) cell.label = c.label;
          if (c.labels && Object.keys(c.labels).length) cell.labels = c.labels;
          if (c.unit) cell.unit = c.unit;
          if (c.scale) cell.scale = c.scale;
          if (c.flow === 'arrow') cell.flow = 'arrow';
          else if (c.flow && c.flow.out && c.flow.in) cell.flow = { out: c.flow.out, in: c.flow.in };
        }
        if (c.align && c.align !== 'left') cell.align = c.align;
        if (c.overflow) cell.overflow = c.overflow;
        if (c.args && Object.keys(c.args).length) cell.args = c.args;
        return cell;
      }) }
      : { kind: t.kind, grid: [t.grid[0], t.grid[1]], style: styleOut(t.style || {}), cells: [] };
    if (!state.file) {
      out.title = t.title || '';
      if (t.variants && Object.keys(t.variants).length) out.variants = t.variants;
    }
    if (t.notices === 'none') out.notices = 'none';
    if (rowsPage()) return out;
    out.cells = t.cells.map((c) => {
      const cell = { at: [c.at[0], c.at[1]], span: [c.span[0], c.span[1]], widget: c.widget, args: c.args || {} };
      const style = styleOut(c.style || {});
      if (Object.keys(style).length) cell.style = style;
      return cell;
    });
    return out;
  }

  // previewBody builds the body of POST /api/preview. Unlike serialize it also
  // carries the inherited title of a variant (ADR 0110), because the header
  // widget shows it on the device as well.
  function previewBody() {
    const out = serialize();
    if (state.file) out.title = state.pageTitle || '';
    return out;
  }

  function widgetInfo(plugin, widget) {
    const p = state.plugins.find((x) => x.name === plugin);
    if (!p || !p.description) return null;
    return (p.description.widgets || []).find((w) => w.widget === widget) || null;
  }

  // textValues flattens the value declarations of /api/plugins. A text cell
  // shows one of them; protocol 7 answers single values only (ADR 0111).
  function textValues() {
    const out = [];
    for (const p of state.plugins) {
      for (const v of (p.description && p.description.values) || []) {
        if (v.kind === 'list') continue;
        out.push({ plugin: p.name, name: v.name, title: v.title || v.name, type: v.type || '', unit: v.unit || '',
          precision: v.precision || 0, progression: v.progression || [], enum: v.enum || [],
          args: v.args || {} });
      }
    }
    return out;
  }

  function valueKey(v) { return v.plugin + '/' + v.name; }

  function valueInfo(key) {
    return textValues().find((v) => valueKey(v) === key) || null;
  }

  function firstValueKey() {
    const v = textValues()[0];
    return v ? valueKey(v) : '';
  }

  // defaultValueWidth leaves room for a sample of the value and its unit; a
  // progression offers several units (ADR 0122).
  function defaultValueWidth(info) {
    let sample = 'Text';
    let unit = '';
    if (info) {
      switch (info.type) {
        case 'number': {
          let best = '0';
          for (const step of [{ unit: info.unit, precision: info.precision || 0 }].concat(info.progression || [])) {
            const n = step.precision > 0 ? '0,' + '0'.repeat(step.precision) : '0';
            const text = step.unit ? n + ' ' + step.unit : n;
            if (text.length > best.length) best = text;
          }
          sample = best;
          break;
        }
        case 'duration': sample = '00min'; break;
        case 'timestamp': sample = '00:00'; break;
        case 'direction': sample = '^'; break;
        default: sample = (info.title || info.name || 'Text').slice(0, 4);
      }
      if (info.type !== 'number') unit = info.unit ? ' ' + info.unit : '';
    }
    return Math.max(5, Math.min(state.width, (sample + unit).length));
  }

  // startPaletteDrag drags a value or the literal Text entry onto the board.
  function startPaletteDrag(e, button, plugin, name, literal) {
    const startX = e.clientX;
    const startY = e.clientY;
    const pointerId = e.pointerId;
    const info = valueInfo(plugin + '/' + name);
    const width = literal ? 0 : defaultValueWidth(info);
    let active = false;
    let pos = null;
    let ghost = null;
    let finished = false;
    const move = (ev) => {
      const dx = ev.clientX - startX;
      const dy = ev.clientY - startY;
      if (!active && Math.abs(dx) + Math.abs(dy) < 4) return;
      if (!active) {
        active = true;
        state.placing = null;
        button.classList.add('dragging');
        ghost = document.createElement('div');
        ghost.className = 'palette-ghost';
        ghost.textContent = literal ? 'Text' : plugin + '/' + name;
        document.body.appendChild(ghost);
      }
      if (ev.cancelable) ev.preventDefault();
      ghost.style.left = (ev.clientX + 12) + 'px';
      ghost.style.top = (ev.clientY + 12) + 'px';
      pos = cellAtPointer(ev);
      if (pos) {
        const w = literal ? Math.min(10, state.width - pos.x) : width;
        showDropMarker(pos.x, pos.y, Math.max(1, w), 1, !fitsText(pos.x, pos.y, Math.max(1, w), 1, -1));
      } else {
        clearDropMarker();
      }
    };
    const finish = (ev, dropIt) => {
      if (finished) return;
      finished = true;
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', cancel);
      button.classList.remove('dragging');
      if (ghost) ghost.remove();
      clearDropMarker();
      if (button.hasPointerCapture && button.hasPointerCapture(pointerId)) {
        try { button.releasePointerCapture(pointerId); } catch (err) { /* already gone */ }
      }
      if (!active) return;
      suppressPaletteClick = true;
      setTimeout(() => { suppressPaletteClick = false; }, 0);
      if (dropIt) {
        const at = cellAtPointer(ev);
        if (at) {
          placeTextAt(at.x, at.y, plugin, name, literal);
          return;
        }
      }
    };
    const up = (ev) => finish(ev, true);
    const cancel = (ev) => finish(ev, false);
    try { button.setPointerCapture && button.setPointerCapture(pointerId); } catch (err) { /* no capture */ }
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', cancel);
  }

  // pickTextValue and pickTextLiteral toggle click-then-place mode.
  function pickTextValue(plugin, name) {
    if (!state.template) return;
    const same = state.placing && state.placing.value && !state.placing.literal &&
      state.placing.plugin === plugin && state.placing.name === name;
    state.placing = same ? null : { plugin, name, value: true };
    if (state.placing) setCellsVisible(true);
    renderPalette();
    renderBoard();
  }

  function pickTextLiteral() {
    if (!state.template) return;
    state.placing = state.placing && state.placing.literal ? null : { literal: true, value: true };
    if (state.placing) setCellsVisible(true);
    renderPalette();
    renderBoard();
  }

  function fitsText(x, y, w, h, ignore) {
    if (x < 0 || y < 0 || w < 1 || h < 1 || x+w > state.width || y+h > state.height) return false;
    return !overlaps(x, y, w, h, ignore);
  }

  // placeTextAt puts a value or literal text box at the clicked slot.
  function placeTextAt(x, y, plugin, name, literal) {
    const t = state.template;
    if (!t) return;
    const info = valueInfo(plugin + '/' + name);
    let w = literal ? Math.min(10, state.width - x) : defaultValueWidth(info);
    w = Math.max(1, Math.min(w, state.width - x));
    if (!fitsText(x, y, w, 1, -1)) {
      if (fitsText(x, y, 1, 1, -1)) w = 1;
      else { setStatus('That area is taken; click a free slot.', 'error'); return; }
    }
    t.cells = t.cells || [];
    t.cells.push({
      at: [x, y], span: [w, 1],
      text: literal ? 'Text' : '', value: literal ? '' : plugin + '/' + name,
      label: '', labels: {}, unit: '', scale: '', flow: null, args: {},
      align: !literal && info && info.type === 'number' ? 'decimal' : 'left',
      overflow: '',
    });
    state.selected = t.cells.length - 1;
    state.placing = null;
    changed();
    renderPalette();
  }

  // showDropMarker draws the rectangle a palette drop would create.
  function showDropMarker(x, y, w, h, invalid) {
    const board = el('board');
    if (!board || !state.template) return;
    clearDropMarker();
    const width = Math.max(1, Math.min(w, state.width));
    const height = Math.max(1, Math.min(h, state.height));
    const col = Math.max(0, Math.min(state.width - width, x));
    const row = Math.max(0, Math.min(state.height - height, y));
    const marker = document.createElement('div');
    marker.className = 'drop-marker' + (invalid ? ' invalid' : '');
    marker.style.gridArea = (row + 1) + ' / ' + (col + 1) + ' / span ' + height + ' / span ' + width;
    board.appendChild(marker);
  }

  function clearDropMarker() {
    const board = el('board');
    if (!board) return;
    for (const node of board.querySelectorAll('.drop-marker')) node.remove();
    for (const slot of board.querySelectorAll('.slot.drop')) slot.classList.remove('drop');
  }

  // cellAtPointer maps the pointer to a slot only while it is over the board;
  // a drop outside the board cancels.
  function cellAtPointer(e) {
    const board = el('board');
    const t = state.template;
    if (!board || !t) return null;
    const r = board.getBoundingClientRect();
    if (!r.width || !r.height) return null;
    if (e.clientX < r.left || e.clientX >= r.right || e.clientY < r.top || e.clientY >= r.bottom) return null;
    return cellAt(e);
  }

  function overlaps(x, y, w, h, skip) {
    return state.template.cells.some((c, i) => i !== skip &&
      x < c.at[0] + c.span[0] && c.at[0] < x + w &&
      y < c.at[1] + c.span[1] && c.at[1] < y + h);
  }

  // ---- page settings ----

  function renderSettings() {
    if (state.widgetsPage) return;
    const t = state.template;
    const off = !t || state.readOnly;
    for (const id of ['title', 'padding', 'rule', 'text-size', 'divider', 'notices']) el(id).disabled = off;
    if (el('overflow')) el('overflow').disabled = off;
    el('title').disabled = off;
    // A variant inherits the title and cannot change it (ADR 0110).
    el('title').readOnly = state.file !== '';
    el('title').title = state.file ? 'The page file owns the title: pages/' + state.name + '.yaml' : '';
    el('cols').disabled = off || text();
    el('rows').disabled = off || text();
    renderGridLock(off);
    if (!t) {
      for (const id of ['title', 'cols', 'rows', 'padding', 'rule', 'text-size', 'divider']) el(id).value = '';
      return;
    }
    if (state.gridLock && !state.gridRatio) state.gridRatio = t.grid.slice();
    const s = t.style || {};
    el('title').value = t.title || state.pageTitle || '';
    el('cols').value = rowsPage() ? state.width : t.grid[0];
    el('rows').value = rowsPage() ? state.height : t.grid[1];
    el('padding').value = s.padding === undefined || s.padding === null ? '' : s.padding;
    el('rule').value = s.rule || '';
    el('text-size').value = s.text_size || '';
    el('divider').value = s.divider || '';
    el('notices').value = t.notices === 'none' ? 'none' : 'show';
    if (el('overflow')) el('overflow').value = t.overflow || 'strip';
  }

  function clampCells(n) {
    return Math.max(1, Math.min(80, Math.round(n)));
  }

  // gridFromInputs reads both fields. While the lock is on, the axis the
  // operator did not change follows the ratio the lock was switched on with,
  // rounded up: 6 × 8 gives 3 × 4, and 2 × 3 as well (ADR 0128). The ratio is
  // the base, not the rounded result, so repeated changes do not drift.
  function gridFromInputs(t, source) {
    const cols = clampCells(Number(el('cols').value) || t.grid[0]);
    const rows = clampCells(Number(el('rows').value) || t.grid[1]);
    const r = state.gridLock ? state.gridRatio : null;
    if (!r || !(r[0] > 0 && r[1] > 0)) return [cols, rows];
    if (source === 'rows') return [clampCells(Math.ceil(rows * r[0] / r[1])), rows];
    return [cols, clampCells(Math.ceil(cols * r[1] / r[0]))];
  }

  // cellSizeText names the pixels of one cell and how far they are from
  // square, because a lock cannot make every density square.
  // cellSizeText names the grid and the size of one cell in pixels, for the
  // note under the board.
  function cellSizeText(t) {
    if (!t || text() || !(state.width > 0 && state.height > 0)) return '';
    const cols = Number(el('cols').value) || t.grid[0];
    const rows = Number(el('rows').value) || t.grid[1];
    if (!(cols > 0 && rows > 0)) return '';
    const w = state.width / cols;
    const h = state.height / rows;
    const dev = Math.round(Math.abs(w / h - 1) * 100);
    const shape = dev <= 1 ? ', square' : ', ' + dev + ' % ' + (w > h ? 'wider than tall' : 'taller than wide');
    return cols + ' × ' + rows + ' cells, ' + Math.round(w) + ' × ' + Math.round(h) + ' px on ' + state.display + shape;
  }

  function renderGridLock(off) {
    const b = el('grid-lock');
    if (!b) return;
    const t = state.template;
    b.hidden = rowsPage() || text() || !t;
    b.disabled = off === undefined ? !t || state.readOnly : off;
    b.setAttribute('aria-pressed', state.gridLock ? 'true' : 'false');
    const ratio = state.gridRatio ? state.gridRatio[0] + ' × ' + state.gridRatio[1] : '';
    b.title = state.gridLock
      ? 'The grid keeps ' + ratio + ' when one axis changes; click to release'
      : 'Keep the grid ratio when one axis changes';
  }

  // setGridLock remembers the lock and the ratio it holds. The lock changes
  // nothing by itself: like the chain of an image editor it applies to the next
  // change of an axis, so a page keeps its grid and the ratio does not drift.
  function setGridLock(on) {
    state.gridLock = !!on;
    state.gridRatio = state.gridLock && state.template ? state.template.grid.slice() : null;
    try {
      localStorage.setItem('paperboy.editor.gridlock', state.gridLock ? '1' : '0');
    } catch (e) {
      // storage may be off; the choice then lasts for this visit
    }
    renderGridLock();
    renderSettings();
  }

  function applyGrid(source) {
    const t = state.template;
    if (!t || text()) return;
    const [cols, rows] = gridFromInputs(t, source);
    for (const c of t.cells) {
      if (c.at[0] + c.span[0] > cols || c.at[1] + c.span[1] > rows) {
        setStatus('a widget leaves the ' + cols + '×' + rows + ' grid; move or delete it first', 'error');
        el('cols').value = t.grid[0];
        el('rows').value = t.grid[1];
        renderSettings();
        return false;
      }
    }
    el('cols').value = cols;
    el('rows').value = rows;
    if (cols !== t.grid[0] || rows !== t.grid[1]) {
      t.grid = [cols, rows];
      changed();
    }
    renderSettings();
    return true;
  }

  // ---- palette ----

  // renderValuePalette lists the values the plugins declare, grouped by
  // plugin, so a text page picks data instead of widgets (ADR 0111).
  function renderValuePalette(box) {
    if (!state.plugins.length) {
      box.innerHTML = '<p class="hint">No plugin answered.</p>';
      return;
    }
    const filter = state.filter.trim().toLowerCase();
    let shown = 0;
    if (!filter || 'text literal'.includes(filter)) {
      const textButton = document.createElement('button');
      textButton.type = 'button';
      textButton.className = 'widget' + (state.placing && state.placing.literal ? ' active' : '');
      textButton.textContent = 'Text — literal text';
      const note = document.createElement('span');
      note.className = 'note';
      note.textContent = 'a plain label';
      textButton.appendChild(note);
      textButton.addEventListener('click', () => {
        if (suppressPaletteClick) { suppressPaletteClick = false; return; }
        pickTextLiteral();
      });
      box.appendChild(textButton);
      shown++;
    }
    for (const p of state.plugins) {
      const values = ((p.description && p.description.values) || []).filter((v) => v.kind !== 'list');
      const matches = values.filter((v) => !filter ||
        (p.name + ' ' + v.name + ' ' + (v.title || '')).toLowerCase().includes(filter));
      if (!matches.length) continue;
      const section = document.createElement('div');
      section.className = 'plugin';
      const head = document.createElement('div');
      head.className = 'plugin-head';
      head.title = 'Show or hide the values of this plugin';
      const collapsed = state.collapsed.has(p.name);
      const chev = document.createElement('span');
      chev.className = 'chev';
      chev.textContent = collapsed ? '▸' : '▾';
      const name = document.createElement('span');
      name.textContent = p.name + (p.description && p.description.title ? ' · ' + p.description.title : '');
      const count = document.createElement('span');
      count.className = 'count';
      count.textContent = String(matches.length);
      head.append(chev, name, count);
      head.addEventListener('click', () => {
        if (collapsed) state.collapsed.delete(p.name);
        else state.collapsed.add(p.name);
        renderPalette();
      });
      section.appendChild(head);
      if (collapsed) {
        box.appendChild(section);
        continue;
      }
      if (p.error) {
        const note = document.createElement('span');
        note.className = 'note';
        note.textContent = p.error;
        section.appendChild(note);
      }
      for (const v of matches) {
        shown++;
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'widget' + (state.placing && state.placing.value && !state.placing.literal &&
          state.placing.plugin === p.name && state.placing.name === v.name ? ' active' : '');
        b.textContent = v.name + ' — ' + (v.title || v.name);
        const note = document.createElement('span');
        note.className = 'note';
        note.textContent = [v.type, v.unit].filter(Boolean).join(' · ');
        if (note.textContent) b.appendChild(note);
        b.addEventListener('click', () => {
          if (suppressPaletteClick) {
            suppressPaletteClick = false;
            return;
          }
          pickTextValue(p.name, v.name);
        });
        b.addEventListener('pointerdown', (e) => {
          if (e.button !== undefined && e.button !== 0) return;
          startPaletteDrag(e, b, p.name, v.name, false);
        });
        section.appendChild(b);
      }
      box.appendChild(section);
    }
    if (!shown) {
      const p = document.createElement('p');
      p.className = 'hint';
      p.textContent = filter ? 'No value matches “' + state.filter.trim() + '”.' : 'No plugin declares a value.';
      box.appendChild(p);
    }
  }

  // ---- palette ----

  const pinnedKey = 'pb-pinned-widgets';
  const openKey = 'pb-open-plugin';

  // storedList reads a list of widget ids the browser keeps.
  function storedList(key) {
    try {
      const value = JSON.parse(localStorage.getItem(key) || '[]');
      return Array.isArray(value) ? value.filter((item) => typeof item === 'string') : [];
    } catch (e) {
      return [];
    }
  }

  function storeList(key, list) {
    try {
      localStorage.setItem(key, JSON.stringify(list));
    } catch (e) {
      // A browser that refuses storage keeps only the list in memory.
    }
  }

  // storedValue reads a string the browser keeps; '' when it refuses storage.
  function storedValue(key) {
    try {
      return localStorage.getItem(key) || '';
    } catch (e) {
      return '';
    }
  }

  function storeValue(key, value) {
    try {
      localStorage.setItem(key, value);
    } catch (e) {
      // A browser that refuses storage keeps only the choice in memory.
    }
  }

  function isPinned(id) {
    return storedList(pinnedKey).includes(id);
  }

  // togglePin keeps a widget at the top of the palette (ADR 0152).
  function togglePin(id) {
    const pinned = storedList(pinnedKey);
    storeList(pinnedKey, pinned.includes(id) ? pinned.filter((item) => item !== id) : [id, ...pinned]);
    renderPalette();
  }

  // pluginHasImageWidget says whether the widget palette can list the plugin.
  function pluginHasImageWidget(p) {
    return ((p.description && p.description.widgets) || [])
      .some((w) => !w.kinds || !w.kinds.length || w.kinds.includes('image'));
  }

  function knownPlugin(name) {
    return state.plugins.some((p) => p.name === name && pluginHasImageWidget(p));
  }

  // pluginOfCell names the plugin of a placed cell, or ''.
  function pluginOfCell(i) {
    const cells = state.template && state.template.cells;
    const cell = cells && typeof i === 'number' && i >= 0 ? cells[i] : null;
    return cell && typeof cell.widget === 'string' ? cell.widget.split('/')[0] : '';
  }

  // setOpenPlugin makes one plugin group the open one and remembers the choice
  // in the browser, like the pins (ADR 0152). An empty name folds every group.
  function setOpenPlugin(name) {
    state.openPlugin = name;
    storeValue(openKey, name);
  }

  // toggleSection folds or unfolds one shortcut group; both start open.
  function toggleSection(key) {
    if (state.collapsedSections.has(key)) state.collapsedSections.delete(key);
    else state.collapsedSections.add(key);
    renderPalette();
  }

  // fitsSelectedCell says whether a widget fits the selected cell: 0 fits, 1
  // unknown, 2 does not fit. Without a selected cell nothing is known, and on
  // the widgets page a pick replaces the cell, so no widget is a misfit there.
  function fitsSelectedCell(widget) {
    if (state.widgetsPage) return 1;
    const cells = state.template && state.template.cells;
    const cell = cells && typeof state.selected === 'number' ? cells[state.selected] : null;
    if (!cell || !cell.span) return 1;
    const grid = cellGrid();
    const sizes = widget.sizes && widget.sizes.length ? widget.sizes : [{ columns: 2, rows: 2 }];
    const fits = sizes.some((size) => {
      const span = mapSpan([size.columns, size.rows], grid);
      return span[0] <= cell.span[0] && span[1] <= cell.span[1];
    });
    return fits ? 0 : 2;
  }

  // Thumbnails load when the entry scrolls into view, so a folded or long
  // palette asks the server for no picture it does not show. The server caches
  // each picture per size and arguments (ADR 0139). A browser without an
  // IntersectionObserver sets every source at once.
  const thumbObserver = typeof IntersectionObserver === 'function'
    ? new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const img = entry.target;
        thumbObserver.unobserve(img);
        if (img.dataset.src) {
          img.src = img.dataset.src;
          delete img.dataset.src;
        }
      }
    }, { rootMargin: '200px' })
    : null;

  // widgetThumb renders the widget small, so a long palette stays scannable.
  function widgetThumb(plugin, widget) {
    const img = document.createElement('img');
    img.className = 'thumb';
    img.loading = 'lazy';
    img.alt = '';
    const src = '/api/widget/' + encodeURIComponent(plugin) + '/' + encodeURIComponent(widget) +
      '?' + new URLSearchParams({ display: state.display, grid: cellGrid().join('x') }).toString();
    if (thumbObserver) {
      img.dataset.src = src;
      thumbObserver.observe(img);
    } else {
      img.src = src;
    }
    return img;
  }

  // widgetButton builds one entry: the picture, the name, the title and the
  // sizes it declares, plus the pin.
  function widgetButton(plugin, widget, fits) {
    const id = plugin + '/' + widget.widget;
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'widget' + (state.placing && state.placing.plugin === plugin &&
      state.placing.widget === widget.widget ? ' active' : '') + (fits === 2 ? ' nofit' : '');
    b.title = fits === 2 ? 'Does not fit the selected cell' : '';
    b.appendChild(widgetThumb(plugin, widget.widget));
    const label = document.createElement('span');
    label.className = 'label';
    const strong = document.createElement('strong');
    strong.textContent = widget.widget;
    label.appendChild(strong);
    if (widget.title) {
      const note = document.createElement('span');
      note.className = 'note';
      note.textContent = widget.title;
      label.appendChild(note);
    }
    if (widget.sizes && widget.sizes.length) {
      const note = document.createElement('span');
      note.className = 'note';
      note.textContent = widget.sizes.map((size) => mapSpan([size.columns, size.rows], cellGrid()).join('×')).join(', ');
      label.appendChild(note);
    }
    b.appendChild(label);
    const pin = document.createElement('span');
    pin.className = 'pin' + (isPinned(id) ? ' on' : '');
    pin.textContent = isPinned(id) ? '★' : '☆';
    pin.title = isPinned(id) ? 'Unpin this widget' : 'Pin this widget';
    pin.setAttribute('role', 'button');
    pin.tabIndex = -1;
    pin.addEventListener('click', (e) => {
      e.stopPropagation();
      togglePin(id);
    });
    b.appendChild(pin);
    b.addEventListener('click', () => {
      if (suppressPaletteClick) {
        suppressPaletteClick = false;
        return;
      }
      pick(plugin, widget.widget);
    });
    b.addEventListener('dragstart', (e) => {
      e.dataTransfer.setData('text/plain', id);
      e.dataTransfer.effectAllowed = 'copy';
    });
    return b;
  }

  // paletteHead builds the head of a palette group. A toggle makes it a button
  // that folds the group; without one the head is a plain label, as in the
  // filtered palette where every group shows its matches.
  function paletteHead(title, count, open, toggle) {
    const head = document.createElement(toggle ? 'button' : 'div');
    head.className = 'plugin-head';
    if (toggle) {
      head.type = 'button';
      head.title = open ? 'Hide this group' : 'Show this group';
      head.setAttribute('aria-expanded', open ? 'true' : 'false');
      head.appendChild(Object.assign(document.createElement('span'), {
        className: 'chev',
        textContent: open ? '▾' : '▸',
      }));
      head.addEventListener('click', toggle);
    } else {
      head.classList.add('static');
    }
    head.appendChild(Object.assign(document.createElement('span'), { textContent: title }));
    head.appendChild(Object.assign(document.createElement('span'), {
      className: 'count',
      textContent: String(count),
    }));
    return head;
  }

  // widgetSection renders the shortcut group (Pinned). It folds on its own and
  // starts open; the plugin groups below share one open group.
  function widgetSection(title, entries, folded, toggle) {
    const section = document.createElement('div');
    section.className = 'plugin';
    section.appendChild(paletteHead(title, entries.length, !folded, toggle));
    if (folded) return section;
    for (const entry of entries) section.appendChild(entry);
    return section;
  }

  // renderPalette lists the widgets of the plugins with a picture each. Pinned
  // widgets come first, one plugin group is open and a selected cell opens the
  // group of its widget (ADR 0152, 0157). A filter shows every group with a
  // match and suspends the fold.
  function renderPalette() {
    const box = el('palette');
    box.innerHTML = '';
    if (thumbObserver) thumbObserver.disconnect();
    if (rowsPage()) {
      renderValuePalette(box);
      return;
    }
    if (state.readOnly || !state.template) {
      box.innerHTML = '<p class="hint">The file does not read as a page; fix it by hand and reload.</p>';
      return;
    }
    if (!state.plugins.length) {
      box.innerHTML = '<p class="hint">No plugin answered.</p>';
      return;
    }
    // A text page returns above; the Playground shows image display types only
    // (ADR 0124), so this palette lists image widgets and no text entry.
    const filter = state.filter.trim().toLowerCase();
    const known = new Map();
    for (const p of state.plugins) {
      for (const w of (p.description && p.description.widgets) || []) {
        if (w.kinds && w.kinds.length && !w.kinds.includes('image')) continue;
        known.set(p.name + '/' + w.widget, { plugin: p.name, widget: w });
      }
    }
    const matches = (list) => list.filter((item) => {
      const entry = known.get(item);
      if (!entry) return false;
      if (!filter) return true;
      return (entry.plugin + ' ' + entry.widget.widget + ' ' + (entry.widget.title || '')).toLowerCase().includes(filter);
    });
    // The first palette open picks the group of the selected cell, else the one
    // the operator opened last, else the first plugin that offers a widget.
    if (!state.openResolved) {
      state.openResolved = true;
      const remembered = storedValue(openKey);
      const own = pluginOfCell(state.selected);
      const first = (state.plugins.find(pluginHasImageWidget) || {}).name || '';
      state.openPlugin = [own, remembered].find((name) => name && knownPlugin(name)) || first;
    }
    let shown = 0;

    if (!filter) {
      const pinned = matches(storedList(pinnedKey));
      if (pinned.length) {
        box.appendChild(widgetSection('Pinned', pinned.map((id) => {
          const entry = known.get(id);
          return widgetButton(entry.plugin, entry.widget, fitsSelectedCell(entry.widget));
        }), state.collapsedSections.has('pinned'), () => toggleSection('pinned')));
      }
    }

    for (const p of state.plugins) {
      const widgets = (p.description && p.description.widgets) || [];
      const fitting = widgets.filter((w) => !w.kinds || !w.kinds.length || w.kinds.includes('image'));
      const found = fitting.filter((w) => !filter ||
        (p.name + ' ' + w.widget + ' ' + (w.title || '')).toLowerCase().includes(filter));
      if (filter && !found.length) continue;
      // One plugin group is open; a filter expands every group with a match.
      const open = !!filter || p.name === state.openPlugin;
      const title = p.name + (p.description && p.description.title ? ' · ' + p.description.title : '');
      const section = document.createElement('div');
      section.className = 'plugin';
      const toggle = filter ? null : () => {
        setOpenPlugin(open ? '' : p.name);
        renderPalette();
      };
      section.appendChild(paletteHead(title, found.length, open, toggle));
      if (!open) {
        box.appendChild(section);
        continue;
      }
      if (p.error) {
        const note = document.createElement('span');
        note.className = 'note';
        note.textContent = p.error;
        section.appendChild(note);
      }
      // The widgets that fit the selected cell come first (ADR 0152).
      const ranked = found
        .map((w) => ({ w: w, rank: fitsSelectedCell(w) }))
        .sort((a, b) => a.rank - b.rank || a.w.widget.localeCompare(b.w.widget));
      for (const item of ranked) {
        shown++;
        section.appendChild(widgetButton(p.name, item.w, item.rank));
      }
      box.appendChild(section);
    }
    if (!shown && filter) {
      const p = document.createElement('p');
      p.className = 'hint';
      p.textContent = 'No widget matches “' + state.filter.trim() + '”.';
      box.appendChild(p);
    }
  }

  function pick(plugin, widget) {
    if (!state.template || rowsPage()) return;
    if (state.widgetsPage) {
      showWidget(plugin, widget);
      return;
    }
    const same = state.placing && state.placing.plugin === plugin && state.placing.widget === widget;
    state.placing = same ? null : { plugin: plugin, widget: widget };
    // Placing needs the boxes: a hidden overlay turns itself back on.
    if (state.placing) setCellsVisible(true);
    renderPalette();
    renderBoard();
  }

  // setWidget puts one widget into the widgets page's single cell, at the first
  // size its plugin declares.
  function setWidget(plugin, widget) {
    const info = widgetInfo(plugin, widget);
    const ref = info && info.sizes && info.sizes.length ? [info.sizes[0].columns, info.sizes[0].rows] : [2, 1];
    const grid = state.template.grid;
    const span = mapSpan(ref, grid);
    state.template.cells = [{ at: [0, 0], span: span, widget: plugin + '/' + widget, args: {}, style: {} }];
    state.selected = 0;
    setOpenPlugin(plugin);
  }

  function showWidget(plugin, widget) {
    if (!state.template) return;
    setWidget(plugin, widget);
    setStatus('');
    changed();
    renderPalette();
  }

  // firstWidget names the first widget the palette offers for a display kind,
  // in the order the palette lists the plugins (the built-in header), so the
  // widgets page and a new variant open with a preview instead of an error
  // (ADR 0084, 0110).
  function firstWidget(kind) {
    for (const p of state.plugins) {
      const widgets = (p.description && p.description.widgets) || [];
      const w = widgets.find((x) => !x.kinds || !x.kinds.length || x.kinds.includes(kind));
      if (w) return { plugin: p.name, widget: w.widget };
    }
    return null;
  }

  function place(plugin, widget, x, y) {
    const t = state.template;
    if (!t) return;
    const info = widgetInfo(plugin, widget);
    const ref = info && info.sizes && info.sizes.length ? [info.sizes[0].columns, info.sizes[0].rows] : [1, 1];
    let span = mapSpan(ref, t.grid);
    span = [Math.max(1, Math.min(span[0], t.grid[0] - x)), Math.max(1, Math.min(span[1], t.grid[1] - y))];
    if (overlaps(x, y, span[0], span[1], -1)) {
      span = [1, 1];
      if (overlaps(x, y, 1, 1, -1)) {
        setStatus('that cell is taken', 'error');
        return;
      }
    }
    t.cells.push({ at: [x, y], span: span, widget: plugin + '/' + widget, args: {}, style: {} });
    state.selected = t.cells.length - 1;
    state.placing = null;
    setOpenPlugin(plugin);
    setStatus('');
    changed();
    renderPalette();
    renderInspector();
  }

  // moveCell puts a placed box on another cell and keeps its span. A drop near
  // an edge lands as far as the box still fits, an occupied stretch refuses it.
  function moveCell(i, x, y) {
    const t = state.template;
    const c = t && t.cells[i];
    if (!c) return;
    const span = [Math.min(c.span[0], t.grid[0]), Math.min(c.span[1], t.grid[1])];
    const at = [
      Math.max(0, Math.min(t.grid[0] - span[0], x)),
      Math.max(0, Math.min(t.grid[1] - span[1], y)),
    ];
    if (overlaps(at[0], at[1], span[0], span[1], i)) {
      setStatus('that cell is taken', 'error');
      return;
    }
    if (at[0] === c.at[0] && at[1] === c.at[1] && span[0] === c.span[0] && span[1] === c.span[1]) return;
    c.at = at;
    c.span = span;
    state.selected = i;
    setStatus('');
    changed();
  }

  // setSpan commits a new size of a placed box; it clamps to the grid and
  // refuses a size that would cover another box.
  function setSpan(i, w, h) {
    const t = state.template;
    const c = t && t.cells[i];
    if (!c) return;
    w = Math.max(1, Math.min(t.grid[0] - c.at[0], w));
    h = Math.max(1, Math.min(t.grid[1] - c.at[1], h));
    if (w === c.span[0] && h === c.span[1]) return;
    if (overlaps(c.at[0], c.at[1], w, h, i)) {
      setStatus('that size overlaps another widget', 'error');
      return;
    }
    c.span = [w, h];
    state.selected = i;
    setStatus('');
    changed();
  }

  // cellAt maps a pointer to the board cell under it, clamped to the grid.
  function cellAt(e) {
    const board = el('board');
    const t = state.template;
    if (!board || !t) return null;
    const r = board.getBoundingClientRect();
    if (!r.width || !r.height) return null;
    const x = Math.floor(((e.clientX - r.left) / r.width) * t.grid[0]);
    const y = Math.floor(((e.clientY - r.top) / r.height) * t.grid[1]);
    return { x: Math.max(0, Math.min(t.grid[0] - 1, x)), y: Math.max(0, Math.min(t.grid[1] - 1, y)) };
  }

  // selectCell marks one box without rebuilding the board, so a drag that has
  // just started keeps its element (and its pointer capture).
  // followSelection opens the palette group of the selected cell, so a widget
  // can be replaced without a search.
  function followSelection(i) {
    const plugin = pluginOfCell(i);
    if (!plugin || plugin === state.openPlugin || !knownPlugin(plugin)) return;
    setOpenPlugin(plugin);
    renderPalette();
  }

  function selectCell(i) {
    state.selected = i;
    const board = el('board');
    if (board) {
      for (const node of board.querySelectorAll('.cell')) {
        node.classList.toggle('selected', Number(node.dataset.i) === i);
      }
    }
    renderInspector();
    followSelection(i);
  }

  // paintCell moves a box on the board while the pointer drags it; the model
  // and the preview follow on pointerup.
  function paintCell(i, at, span) {
    const board = el('board');
    const node = board && board.querySelector('.cell[data-i="' + i + '"]');
    if (!node) return;
    node.style.gridArea = (at[1] + 1) + ' / ' + (at[0] + 1) + ' / span ' + span[1] + ' / span ' + span[0];
    const pos = node.querySelector('.pos');
    if (pos) pos.textContent = at[0] + ',' + at[1] + ' · ' + span[0] + '×' + span[1];
  }

  // startDrag begins moving a box (kind "move") or resizing it (kind "resize",
  // axis x, y or both). The grabbed point stays under the pointer, so a box
  // does not jump when it is picked up in the middle (ADR 0085).
  function startDrag(e, i, kind, axis) {
    const t = state.template;
    const c = t && t.cells[i];
    if (!c || e.button !== 0) return;
    const pos = cellAt(e);
    if (!pos) return;
    state.drag = {
      kind: kind, i: i, axis: axis || '',
      grab: [pos.x - c.at[0], pos.y - c.at[1]],
      at: [c.at[0], c.at[1]], span: [c.span[0], c.span[1]],
      start: [e.clientX, e.clientY], moved: false,
    };
    selectCell(i);
    const node = e.currentTarget.closest('.cell');
    if (node) {
      node.classList.add('dragging');
      try {
        node.setPointerCapture(e.pointerId);
      } catch (err) {
        // the window listens as well; capture only makes fast drags smoother
      }
    }
    window.addEventListener('pointermove', dragMove);
    window.addEventListener('pointerup', dragEnd);
    window.addEventListener('pointercancel', dragEnd);
    e.preventDefault();
  }

  function dragMove(e) {
    const d = state.drag;
    const t = state.template;
    const c = d && t && t.cells[d.i];
    if (!d || !c) return;
    if (!d.moved && Math.abs(e.clientX - d.start[0]) + Math.abs(e.clientY - d.start[1]) < 3) return;
    const pos = cellAt(e);
    if (!pos) return;
    if (d.kind === 'move') {
      const at = [
        Math.max(0, Math.min(t.grid[0] - c.span[0], pos.x - d.grab[0])),
        Math.max(0, Math.min(t.grid[1] - c.span[1], pos.y - d.grab[1])),
      ];
      if (overlaps(at[0], at[1], c.span[0], c.span[1], d.i)) return;
      if (at[0] === d.at[0] && at[1] === d.at[1]) return;
      d.moved = true;
      d.at = at;
      paintCell(d.i, at, c.span);
      return;
    }
    let w = d.span[0], h = d.span[1];
    if (d.axis !== 'y') w = pos.x - c.at[0] + 1;
    if (d.axis !== 'x') h = pos.y - c.at[1] + 1;
    w = Math.max(1, Math.min(t.grid[0] - c.at[0], w));
    h = Math.max(1, Math.min(t.grid[1] - c.at[1], h));
    if (overlaps(c.at[0], c.at[1], w, h, d.i)) return;
    if (w === d.span[0] && h === d.span[1]) return;
    d.moved = true;
    d.span = [w, h];
    paintCell(d.i, c.at, d.span);
  }

  function dragEnd() {
    window.removeEventListener('pointermove', dragMove);
    window.removeEventListener('pointerup', dragEnd);
    window.removeEventListener('pointercancel', dragEnd);
    const d = state.drag;
    state.drag = null;
    const board = el('board');
    const node = board && board.querySelector('.cell.dragging');
    if (node) node.classList.remove('dragging');
    if (!d || !d.moved) return;
    if (d.kind === 'move') moveCell(d.i, d.at[0], d.at[1]);
    else setSpan(d.i, d.span[0], d.span[1]);
  }

  // boardDragOver accepts a drag over the board and marks the drop position of
  // a text page.
  function boardDragOver(e) {
    e.preventDefault();
  }

  // boardDrop places a widget that was dragged out of the palette.
  function boardDrop(e) {
    e.preventDefault();
    const t = state.template;
    if (!t) return;
    const pos = cellAt(e);
    if (!pos) return;
    const id = e.dataTransfer.getData('text/plain');
    const [plugin, widget] = id.split('/');
    if (plugin && widget) place(plugin, widget, pos.x, pos.y);
  }

  // ---- board ----

  // boardClasses keeps the board's state classes together: a text display draws
  // its character grid, placement highlights the free slots, and the Cells
  // switch hides the boxes (ADR 0074).
  function boardClasses() {
    return [text() ? 'text' : '', state.placing ? 'placing' : '', cellsHidden() ? 'hide-cells' : '']
      .filter(Boolean).join(' ');
  }

  function cellsHidden() {
    const box = el('show-cells');
    return !!box && !box.checked;
  }

  // setCellsVisible drives the Cells switch and remembers the choice per
  // browser, so the overlay comes back the way the operator left it.
  function setCellsVisible(on) {
    const box = el('show-cells');
    if (box) box.checked = on;
    const board = el('board');
    if (board) board.classList.toggle('hide-cells', !on);
    try {
      localStorage.setItem('paperboy.editor.cells', on ? '1' : '0');
    } catch (e) {
      // storage may be off; the switch still works for this view
    }
  }

  function renderBoard() {
    if (state.widgetsPage) return;
    const board = el('board');
    board.innerHTML = '';
    board.className = boardClasses();
    const t = state.template;
    if (!t) {
      el('board-note').textContent = 'The file cannot be edited until it reads as a page again.';
      return;
    }
    const cols = rowsPage() ? state.width : t.grid[0];
    const rows = rowsPage() ? state.height : t.grid[1];
    board.style.gridTemplateColumns = 'repeat(' + cols + ', 1fr)';
    board.style.gridTemplateRows = 'repeat(' + rows + ', 1fr)';
    board.style.aspectRatio = state.width + ' / ' + state.height;
    for (let y = 0; y < rows; y++) {
      for (let x = 0; x < cols; x++) {
        const slot = document.createElement('div');
        slot.className = 'slot';
        slot.dataset.x = String(x);
        slot.dataset.y = String(y);
        slot.style.gridArea = (y + 1) + ' / ' + (x + 1);
        slot.addEventListener('click', () => {
          if (rowsPage()) {
            if (state.placing) placeTextAt(x, y, state.placing.plugin, state.placing.name, state.placing.literal);
            else select(-1);
          } else if (state.placing) place(state.placing.plugin, state.placing.widget, x, y);
          else select(-1);
        });
        board.appendChild(slot);
      }
    }
    if (rowsPage()) {
      t.cells.forEach((c, i) => {
        const box = document.createElement('div');
        box.className = 'cell text' + (i === state.selected ? ' selected' : '');
        box.dataset.i = String(i);
        box.style.gridArea = (c.at[1] + 1) + ' / ' + (c.at[0] + 1) + ' / span ' + c.span[1] + ' / span ' + c.span[0];
        box.title = (c.text ? 'text' : c.value || 'no value') + ' · ' + c.at[0] + ',' + c.at[1] + ' · ' + c.span[0] + '×' + c.span[1];
        box.addEventListener('pointerdown', (e) => {
          e.stopPropagation();
          startDrag(e, i, 'move');
        });
        for (const [axis, cls, label] of [
          ['x', 'handle-x', 'Drag to change the width'],
          ['y', 'handle-y', 'Drag to change the height'],
          ['both', 'handle-xy', 'Drag to resize'],
        ]) {
          const handle = document.createElement('span');
          handle.className = 'handle ' + cls;
          handle.title = label;
          handle.addEventListener('pointerdown', (e) => {
            e.stopPropagation();
            startDrag(e, i, 'resize', axis);
          });
          box.appendChild(handle);
        }
        board.appendChild(box);
      });
      let note = state.width + '×' + state.height + ' characters, ' + t.cells.length + ' boxes';
      const scrollers = t.cells.filter((c) => (c.overflow || t.overflow || 'strip') === 'scroll');
      if (scrollers.length) {
        const display = state.displays.find((d) => d.name === state.display);
        const step = (display && display.scroll_step) || 300;
        note += ' · scrolls, step ' + step + ' ms';
      }
      if (state.placing) note = 'Click a slot to place ' + (state.placing.literal ? 'text' : state.placing.plugin + '/' + state.placing.name) + '.';
      el('board-note').textContent = note;
      applyBoardPreview();
      return;
    }
    t.cells.forEach((c, i) => {
      const cell = document.createElement('div');
      cell.className = 'cell' + (i === state.selected ? ' selected' : '');
      cell.dataset.i = String(i);
      cell.style.gridArea = (c.at[1] + 1) + ' / ' + (c.at[0] + 1) + ' / span ' + c.span[1] + ' / span ' + c.span[0];
      const name = document.createElement('span');
      name.className = 'w';
      name.textContent = c.widget;
      const pos = document.createElement('span');
      pos.className = 'pos';
      pos.textContent = c.at[0] + ',' + c.at[1] + ' · ' + c.span[0] + '×' + c.span[1];
      cell.append(name, pos);
      // The body moves the box, an edge resizes it. The grabbed point stays
      // under the pointer, so the box does not jump when it is picked up in the
      // middle (ADR 0085).
      cell.addEventListener('pointerdown', (e) => {
        e.stopPropagation();
        startDrag(e, i, 'move');
      });
      for (const [axis, cls, label] of [
        ['x', 'handle-x', 'Drag to change the width'],
        ['y', 'handle-y', 'Drag to change the height'],
        ['both', 'handle-xy', 'Drag to resize'],
      ]) {
        const handle = document.createElement('span');
        handle.className = 'handle ' + cls;
        handle.title = label;
        handle.addEventListener('pointerdown', (e) => {
          e.stopPropagation();
          startDrag(e, i, 'resize', axis);
        });
        cell.appendChild(handle);
      }
      board.appendChild(cell);
    });
    let note = '';
    if (state.placing) {
      note = 'Click a free cell to place ' + state.placing.plugin + '/' + state.placing.widget + '.';
    } else {
      const size = cellSizeText(t);
      if (size) note = size + '.';
    }
    el('board-note').textContent = note;
    applyBoardPreview();
  }

  // applyBoardPreview paints the latest preview into the board it fetched for:
  // an image as the board background, the text lines as one character per slot.
  // It only touches the background and the slot text, so the cells and the
  // selection stay where they are.
  function applyBoardPreview() {
    const board = el('board');
    if (!board) return;
    if (state.previewKind === 'image' && state.previewURL) {
      board.style.backgroundImage = 'url("' + state.previewURL + '")';
    } else {
      board.style.backgroundImage = '';
    }
    for (const slot of board.querySelectorAll('.slot')) slot.textContent = '';
    if (state.previewKind !== 'text' || !state.template) return;
    const [cols, rows] = rowsPage() ? [state.width, state.height] : state.template.grid;
    for (let y = 0; y < rows; y++) {
      const line = state.previewLines[y] || '';
      for (let x = 0; x < cols; x++) {
        const slot = board.querySelector('.slot[data-x="' + x + '"][data-y="' + y + '"]');
        if (slot) slot.textContent = line[x] || '';
      }
    }
    // The pixel font fills the glyph box of one slot (ADR 0116).
    const cell = board.querySelector('.slot');
    if (cell) {
      const h = cell.getBoundingClientRect().height;
      if (h > 0) board.style.setProperty('--lcd-glyph', (h * 1.0) + 'px');
    }
  }

  function select(i) {
    state.selected = i;
    renderBoard();
    renderInspector();
    followSelection(i);
  }

  // ---- inspector ----

  function field(label, control) {
    const l = document.createElement('label');
    l.className = 'field';
    const span = document.createElement('span');
    span.textContent = label;
    l.append(span, control);
    return l;
  }

  function numberInput(value, step, min) {
    const input = document.createElement('input');
    input.type = 'number';
    input.step = String(step);
    if (min !== undefined) input.min = String(min);
    input.value = value === undefined || value === null ? '' : String(value);
    return input;
  }

  // enumLabel is the display text of an enum value; the stored value stays
  // the key, and plugins without labels keep the key as the label.
  function enumLabel(arg, value) {
    const key = String(value);
    return (arg && arg.enum_labels && arg.enum_labels[key]) || key;
  }

  function argValue(raw, arg) {
    const type = arg && arg.type;
    if (type === 'string' || type === 'duration') return raw;
    if (type === 'list') {
      if (Array.isArray(raw)) return raw.map((s) => jsonOrString(String(s)));
      return raw.split(',').map((s) => s.trim()).filter((s) => s !== '').map(jsonOrString);
    }
    return jsonOrString(raw);
  }

  function jsonOrString(s) {
    try {
      return JSON.parse(s);
    } catch (e) {
      return s;
    }
  }

  function setArg(cell, name, raw, arg) {
    if (!arg || arg.type !== 'bool') {
      if (raw === '' || raw === null || (Array.isArray(raw) && raw.length === 0)) delete cell.args[name];
      else cell.args[name] = argValue(raw, arg);
      changed('inspector');
      return;
    }
    // A bool that differs from its declared default is written out, so an
    // unchecked box can still mean false for a default of true.
    const value = raw === true;
    const fallback = arg.default === true;
    if (value === fallback) delete cell.args[name];
    else cell.args[name] = value;
    changed('inspector');
  }

  function renderInspector() {
    const box = el('inspector');
    box.innerHTML = '';
    const head = document.createElement('h3');
    head.textContent = state.widgetsPage ? 'Widget' : 'Cell';
    box.appendChild(head);
    if (rowsPage()) {
      renderTextInspector(box);
      updateSave();
      return;
    }
    const t = state.template;
    const c = t && state.selected >= 0 ? t.cells[state.selected] : null;
    if (!c) {
      const p = document.createElement('p');
      p.className = 'empty';
      p.textContent = state.widgetsPage
        ? 'Pick a widget from the palette to change its size and its arguments.'
        : 'Select a cell to change its size, its arguments and its style.';
      box.appendChild(p);
      updateSnippet();
      updateSave();
      return;
    }
    const where = document.createElement('p');
    where.className = 'muted';
    where.textContent = c.widget + ' at ' + c.at[0] + ',' + c.at[1];
    box.appendChild(where);

    // span, clipped to the grid and rejected when it would overlap
    const spanWrap = document.createElement('span');
    spanWrap.className = 'grid2';
    const cols = numberInput(c.span[0], 1, 1);
    cols.max = String(t.grid[0] - c.at[0]);
    const rows = numberInput(c.span[1], 1, 1);
    rows.max = String(t.grid[1] - c.at[1]);
    spanWrap.append(cols, rows);
    box.appendChild(field('Span', spanWrap));
    const applySpan = () => {
      const w = Math.max(1, Math.min(t.grid[0] - c.at[0], Math.round(num(cols.value) || 1)));
      const h = Math.max(1, Math.min(t.grid[1] - c.at[1], Math.round(num(rows.value) || 1)));
      if (overlaps(c.at[0], c.at[1], w, h, state.selected)) {
        setStatus('that size overlaps another widget', 'error');
        cols.value = c.span[0];
        rows.value = c.span[1];
        return;
      }
      cols.value = w;
      rows.value = h;
      if (w !== c.span[0] || h !== c.span[1]) {
        c.span = [w, h];
        setStatus('');
        changed('inspector');
      }
    };
    cols.addEventListener('change', applySpan);
    rows.addEventListener('change', applySpan);

    // position, for a precise move without the mouse; it clamps like a drag
    const posWrap = document.createElement('span');
    posWrap.className = 'grid2';
    const atX = numberInput(c.at[0], 1, 0);
    atX.max = String(t.grid[0] - c.span[0]);
    const atY = numberInput(c.at[1], 1, 0);
    atY.max = String(t.grid[1] - c.span[1]);
    posWrap.append(atX, atY);
    box.appendChild(field('Position', posWrap));
    const applyPos = () => {
      const x = Math.max(0, Math.min(t.grid[0] - c.span[0], Math.round(num(atX.value) || 0)));
      const y = Math.max(0, Math.min(t.grid[1] - c.span[1], Math.round(num(atY.value) || 0)));
      if (overlaps(x, y, c.span[0], c.span[1], state.selected)) {
        setStatus('that position overlaps another widget', 'error');
        atX.value = c.at[0];
        atY.value = c.at[1];
        return;
      }
      atX.value = x;
      atY.value = y;
      if (x !== c.at[0] || y !== c.at[1]) moveCell(state.selected, x, y);
    };
    atX.addEventListener('change', applyPos);
    atY.addEventListener('change', applyPos);

    // arguments from the plugin's schema (ADR 0055)
    const info = widgetInfo(c.widget.split('/')[0], c.widget.split('/')[1]);
    renderArgFields(box, c, (info && info.args) || {});

    const style = c.style || {};
    c.style = style;
    const h = document.createElement('h4');
    h.textContent = 'Style (pt)';
    box.appendChild(h);
    for (const key of ['padding', 'rule', 'text_size']) {
      const input = numberInput(style[key], 0.1, 0);
      input.addEventListener('input', () => {
        const v = num(input.value);
        if (v === null) delete style[key];
        else style[key] = v;
        changed('inspector');
      });
      box.appendChild(field(key, input));
    }

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'danger';
    remove.textContent = state.widgetsPage ? 'Clear' : 'Delete cell';
    remove.addEventListener('click', removeCell);
    box.appendChild(remove);
    updateSnippet();
    updateSave();
  }

  // renderArgFields draws the argument controls of a widget or a value from
  // the plugin's schema (ADR 0055, 0120).
  function renderArgFields(box, c, args) {
    c.args = c.args || {};
    const names = Object.keys(args).sort();
    if (names.length) {
      const h = document.createElement('h4');
      h.textContent = 'Arguments';
      box.appendChild(h);
    }
    for (const name of names) {
      const arg = args[name];
      const wrap = document.createElement('div');
      wrap.className = 'arg';
      const label = document.createElement('span');
      label.className = 'name';
      label.textContent = name + (arg.required ? ' *' : '');
      wrap.appendChild(label);
      const current = c.args[name];
      let control;
      if (arg.type === 'bool') {
        control = document.createElement('input');
        control.type = 'checkbox';
        control.checked = current === undefined ? arg.default === true : current === true;
        control.addEventListener('change', () => setArg(c, name, control.checked, arg));
      } else if (Array.isArray(arg.enum) && arg.enum.length && arg.type !== 'list') {
        control = document.createElement('select');
        const none = document.createElement('option');
        none.value = '';
        // An argument may label its empty value, e.g. the device that shows
        // the page (ADR 0123).
        none.textContent = enumLabel(arg, '') ||
          (arg.default === undefined || arg.default === null ? 'default' : 'default (' + arg.default + ')');
        control.appendChild(none);
        for (const value of arg.enum) {
          const option = document.createElement('option');
          option.value = String(value);
          option.textContent = enumLabel(arg, value);
          control.appendChild(option);
        }
        // A stored value that is no longer configured stays visible, so the
        // select does not show the empty value for a page that names it.
        const stored = current === undefined || current === null ? '' : String(current);
        if (stored !== '' && !arg.enum.some((v) => String(v) === stored)) {
          const option = document.createElement('option');
          option.value = stored;
          option.textContent = stored + ' (not configured)';
          control.appendChild(option);
        }
        control.value = stored;
        control.addEventListener('change', () => {
          if (control.value === '') setArg(c, name, '', arg);
          else {
            const chosen = arg.enum.find((v) => String(v) === control.value);
            c.args[name] = chosen === undefined ? control.value : chosen;
            changed('inspector');
          }
        });
      } else if (arg.type === 'list' && Array.isArray(arg.enum) && arg.enum.length) {
        // A list with a fixed set: one check box per value in two columns,
        // with a select all/none bar when the list is long enough to need it.
        control = document.createElement('div');
        control.className = 'checks';
        control.setAttribute('role', 'group');
        control.setAttribute('aria-label', name);
        const chosen = (Array.isArray(current) ? current : Array.isArray(arg.default) ? arg.default : []).map(String);
        // A stored value that is no longer configured keeps its box, so the
        // page does not lose it unnoticed (ADR 0123).
        const choices = arg.enum.map(String);
        for (const value of chosen) {
          if (value !== '' && !choices.includes(value)) choices.push(value);
        }
        const grid = document.createElement('div');
        grid.className = 'checks-grid';
        const boxes = [];
        let count = null;
        for (const value of choices) {
          const item = document.createElement('label');
          item.className = 'check';
          const box = document.createElement('input');
          box.type = 'checkbox';
          box.value = String(value);
          box.checked = chosen.includes(String(value));
          const text = document.createElement('span');
          text.textContent = enumLabel(arg, value);
          if (!arg.enum.some((v) => String(v) === String(value))) text.textContent += ' (not configured)';
          text.title = String(value);
          item.append(box, text);
          grid.appendChild(item);
          boxes.push(box);
        }
        const pick = () => {
          const picked = boxes.filter((i) => i.checked).map((i) => i.value);
          if (count) count.textContent = picked.length + ' of ' + boxes.length;
          // An empty pick stays an empty list when the arg has a default, so
          // "None" means none and not "show the defaults again".
          if (picked.length === 0 && Array.isArray(arg.default)) {
            c.args[name] = [];
            changed('inspector');
            return;
          }
          setArg(c, name, picked, arg);
        };
        if (choices.length > 3) {
          const bar = document.createElement('div');
          bar.className = 'checks-bar';
          count = document.createElement('span');
          count.className = 'checks-count';
          count.textContent = chosen.length + ' of ' + choices.length;
          const actions = document.createElement('span');
          actions.className = 'checks-actions';
          for (const [label, title, on] of [['All', 'Select all values', true], ['None', 'Clear the selection', false]]) {
            const button = document.createElement('button');
            button.type = 'button';
            button.textContent = label;
            button.title = title;
            button.addEventListener('click', () => {
              for (const box of boxes) box.checked = on;
              pick();
            });
            actions.appendChild(button);
          }
          bar.append(count, actions);
          control.appendChild(bar);
        }
        control.appendChild(grid);
        // No checked box means the device that shows the page; the plugin's
        // label for the empty value says so (ADR 0123).
        if (arg.enum_labels && arg.enum_labels['']) {
          const note = document.createElement('span');
          note.className = 'desc';
          note.textContent = 'none: ' + arg.enum_labels[''];
          control.appendChild(note);
        }
        for (const box of boxes) box.addEventListener('change', pick);
      } else {
        control = document.createElement('input');
        control.type = 'text';
        control.autocomplete = 'off';
        if (arg.default !== undefined && arg.default !== null) control.placeholder = String(arg.default);
        if (Array.isArray(current)) control.value = current.join(', ');
        else if (current !== undefined) control.value = String(current);
        control.addEventListener('input', () => setArg(c, name, control.value, arg));
      }
      wrap.appendChild(control);
      if (arg.description) {
        const desc = document.createElement('span');
        desc.className = 'desc';
        desc.textContent = arg.description;
        wrap.appendChild(desc);
      }
      box.appendChild(wrap);
    }
  }

  // renderTextInspector shows the fields of the selected text box.
  function renderTextInspector(box) {
    const t = state.template;
    const c = t && t.cells && state.selected >= 0 ? t.cells[state.selected] : null;
    if (!c) {
      const hint = document.createElement('p');
      hint.className = 'empty';
      hint.textContent = 'Select a cell on the board.';
      box.appendChild(hint);
      return;
    }
    const where = document.createElement('p');
    where.className = 'muted';
    where.textContent = ((c.text || '') !== '' ? 'text' : (c.value || 'no value')) + ' at ' + c.at[0] + ',' + c.at[1];
    box.appendChild(where);
    textCellFields(box, c);
    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'danger';
    remove.textContent = 'Delete cell';
    remove.addEventListener('click', removeCell);
    box.appendChild(remove);
  }

  // textCellFields edits one text box in the inspector panel.
  function textCellFields(box, c) {

    const spanWrap = document.createElement('span');
    spanWrap.className = 'grid2';
    const cols = numberInput(c.span[0], 1, 1);
    cols.max = String(state.width - c.at[0]);
    const rows = numberInput(c.span[1], 1, 1);
    rows.max = String(state.height - c.at[1]);
    spanWrap.append(cols, rows);
    box.appendChild(field('Span', spanWrap));
    const applySpan = () => {
      const w = Math.max(1, Math.min(state.width - c.at[0], Math.round(num(cols.value) || 1)));
      const h = Math.max(1, Math.min(state.height - c.at[1], Math.round(num(rows.value) || 1)));
      setSpan(state.selected, w, h);
    };
    cols.addEventListener('change', applySpan);
    rows.addEventListener('change', applySpan);

    const posWrap = document.createElement('span');
    posWrap.className = 'grid2';
    const atX = numberInput(c.at[0], 1, 0);
    atX.max = String(state.width - c.span[0]);
    const atY = numberInput(c.at[1], 1, 0);
    atY.max = String(state.height - c.span[1]);
    posWrap.append(atX, atY);
    box.appendChild(field('Position', posWrap));
    const applyPos = () => {
      const x = Math.max(0, Math.min(state.width - c.span[0], Math.round(num(atX.value) || 0)));
      const y = Math.max(0, Math.min(state.height - c.span[1], Math.round(num(atY.value) || 0)));
      moveCell(state.selected, x, y);
    };
    atX.addEventListener('change', applyPos);
    atY.addEventListener('change', applyPos);

    const isText = (c.text || '') !== '';
    const kind = document.createElement('select');
    for (const [value, text] of [['value', 'Value'], ['text', 'Text']]) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = text;
      kind.appendChild(option);
    }
    kind.value = isText ? 'text' : 'value';
    kind.addEventListener('change', () => {
      c.args = {};
      if (kind.value === 'text') {
        c.text = c.text || 'Text';
        c.value = '';
        c.label = '';
        c.labels = {};
        c.unit = '';
        c.scale = '';
        c.flow = null;
      } else {
        c.value = c.value || firstValueKey();
        c.text = '';
      }
      changed();
    });
    box.appendChild(field('Content', kind));

    if (isText) {
      const area = document.createElement('textarea');
      area.rows = 3;
      area.value = c.text;
      area.addEventListener('input', () => {
        c.text = area.value;
        changed('inspector');
      });
      box.appendChild(field('Text', area));
    } else {
      const values = textValues();
      const select = document.createElement('select');
      const none = document.createElement('option');
      none.value = '';
      none.textContent = values.length ? '— choose a value —' : '— no value declared —';
      select.appendChild(none);
      const groups = new Map();
      for (const v of values) {
        const key = valueKey(v);
        if (!groups.has(v.plugin)) {
          const plugin = state.plugins.find((p) => p.name === v.plugin) || {};
          const group = document.createElement('optgroup');
          group.label = v.plugin + (plugin.description && plugin.description.title ? ' · ' + plugin.description.title : '');
          groups.set(v.plugin, group);
          select.appendChild(group);
        }
        const option = document.createElement('option');
        option.value = key;
        option.textContent = v.name + ' — ' + v.title + (v.unit ? ' (' + v.unit + ')' : '');
        groups.get(v.plugin).appendChild(option);
      }
      if (c.value && !values.some((v) => valueKey(v) === c.value)) {
        const option = document.createElement('option');
        option.value = c.value;
        option.textContent = c.value + ' — not declared';
        select.appendChild(option);
      }
      select.value = c.value || '';
      select.addEventListener('change', () => {
        c.value = select.value;
        // the arguments, the scale and the enum labels belong to the value, so
        // a new one starts without them
        c.args = {};
        c.scale = '';
        c.labels = {};
        c.flow = null;
        changed();
      });
      box.appendChild(field('Value', select));
      const info = valueInfo(c.value);
      if (info) {
        const note = document.createElement('span');
        note.className = 'desc';
        const units = info.progression && info.progression.length
          ? (info.unit ? info.unit + ' → ' : '') + info.progression.map((s) => s.unit).join(', ')
          : info.unit;
        note.textContent = [info.type, units].filter(Boolean).join(' · ');
        box.appendChild(note);
      }

      const label = document.createElement('input');
      label.type = 'text';
      label.autocomplete = 'off';
      label.value = c.label || '';
      label.addEventListener('input', () => {
        c.label = label.value;
        changed('inspector');
      });
      box.appendChild(field('Label', label));

      // An enum value shows readable text per key; the file keeps the key
      // (ADR 0127). One input per declared key, empty means the key itself.
      const enumKeys = (info && info.type === 'enum' && Array.isArray(info.enum)) ? info.enum : [];
      if (enumKeys.length) {
        const heading = document.createElement('h4');
        heading.textContent = 'Labels';
        box.appendChild(heading);
        for (const key of enumKeys) {
          const input = document.createElement('input');
          input.type = 'text';
          input.autocomplete = 'off';
          input.placeholder = key;
          input.value = (c.labels && c.labels[key]) || '';
          input.addEventListener('input', () => {
            c.labels = c.labels || {};
            if (input.value === '') delete c.labels[key];
            else c.labels[key] = input.value;
            changed('inspector');
          });
          box.appendChild(field(key, input));
        }
      }

      // The unit is a choice of declared, none and custom, because an empty
      // text field cannot tell "declared" from "none" (ADR 0122).
      const unitChoice = document.createElement('select');
      for (const [value, text] of [['', 'declared'], ['none', 'none'], ['custom', 'custom']]) {
        const option = document.createElement('option');
        option.value = value;
        option.textContent = text;
        unitChoice.appendChild(option);
      }
      const customUnit = document.createElement('input');
      customUnit.type = 'text';
      customUnit.autocomplete = 'off';
      customUnit.placeholder = info && info.unit ? info.unit : '';
      const isCustom = c.unit !== '' && c.unit !== 'none';
      customUnit.value = isCustom ? c.unit : '';
      customUnit.hidden = !isCustom;
      unitChoice.value = c.unit === 'none' ? 'none' : isCustom ? 'custom' : '';
      unitChoice.addEventListener('change', () => {
        if (unitChoice.value === 'none') {
          c.unit = 'none';
        } else if (unitChoice.value === 'custom') {
          if (!customUnit.value) customUnit.value = (info && info.unit) || '';
          c.unit = customUnit.value;
        } else {
          c.unit = '';
        }
        customUnit.hidden = unitChoice.value !== 'custom';
        changed('inspector');
        if (unitChoice.value === 'custom') customUnit.focus();
      });
      customUnit.addEventListener('input', () => {
        c.unit = customUnit.value;
        changed('inspector');
      });
      const unitWrap = document.createElement('span');
      unitWrap.style.display = 'flex';
      unitWrap.style.gap = '.4rem';
      customUnit.style.width = '6rem';
      unitWrap.append(unitChoice, customUnit);
      box.appendChild(field('Unit', unitWrap));

      // A value with a progression lets the page force one of its steps; auto
      // lets Paperboy choose it from the value (ADR 0122).
      const steps = (info && info.progression) || [];
      if (steps.length || (c.scale && c.scale !== 'auto')) {
        const scale = document.createElement('select');
        const auto = document.createElement('option');
        auto.value = 'auto';
        auto.textContent = 'auto';
        scale.appendChild(auto);
        if (info && info.unit) {
          const base = document.createElement('option');
          base.value = info.unit;
          base.textContent = info.unit + ' (base)';
          scale.appendChild(base);
        }
        for (const step of steps) {
          const option = document.createElement('option');
          option.value = step.unit;
          option.textContent = step.unit;
          scale.appendChild(option);
        }
        if (c.scale && c.scale !== 'auto' && !steps.some((s) => s.unit === c.scale) && c.scale !== (info && info.unit)) {
          const option = document.createElement('option');
          option.value = c.scale;
          option.textContent = c.scale + ' (not declared)';
          scale.appendChild(option);
        }
        scale.value = c.scale || 'auto';
        scale.addEventListener('change', () => {
          c.scale = scale.value === 'auto' ? '' : scale.value;
          changed();
        });
        box.appendChild(field('Scale', scale));
      }

      // A signed number may show its direction as < or >, or as words that
      // belong to the page (issue #399).
      if (info && info.type === 'number') {
        const flow = document.createElement('select');
        const arrow = c.flow === 'arrow';
        const words = c.flow && typeof c.flow === 'object';
        for (const [value, text] of [['', 'none'], ['arrow', 'arrow < >'], ['words', 'words']]) {
          const option = document.createElement('option');
          option.value = value;
          option.textContent = text;
          flow.appendChild(option);
        }
        flow.value = arrow ? 'arrow' : words ? 'words' : '';

        const out = document.createElement('input');
        out.type = 'text';
        out.autocomplete = 'off';
        out.placeholder = 'out';
        out.value = words ? (c.flow.out || '') : '';
        const into = document.createElement('input');
        into.type = 'text';
        into.autocomplete = 'off';
        into.placeholder = 'in';
        into.value = words ? (c.flow.in || '') : '';
        const wordWrap = document.createElement('span');
        wordWrap.style.display = 'flex';
        wordWrap.style.gap = '.4rem';
        out.style.width = '6rem';
        into.style.width = '6rem';
        wordWrap.append(out, into);
        wordWrap.hidden = flow.value !== 'words';

        const editWords = () => {
          c.flow = { out: out.value, in: into.value };
          changed('inspector');
        };
        out.addEventListener('input', editWords);
        into.addEventListener('input', editWords);
        flow.addEventListener('change', () => {
          if (flow.value === 'arrow') c.flow = 'arrow';
          else if (flow.value === 'words') c.flow = { out: out.value, in: into.value };
          else c.flow = null;
          wordWrap.hidden = flow.value !== 'words';
          changed('inspector');
        });
        box.appendChild(field('Flow', flow));
        box.appendChild(field('Out / in', wordWrap));
      }

      renderArgFields(box, c, (info && info.args) || {});
    }

    const align = document.createElement('select');
    for (const [value, text] of [['left', 'left'], ['right', 'right'], ['decimal', 'decimal']]) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = text;
      align.appendChild(option);
    }
    align.value = c.align === 'right' || c.align === 'decimal' ? c.align : 'left';
    align.addEventListener('change', () => {
      c.align = align.value;
      changed();
    });
    box.appendChild(field('Align', align));

    const overflow = document.createElement('select');
    for (const [value, text] of [['', 'page default'], ['strip', 'strip'], ['wrap', 'wrap'], ['scroll', 'scroll']]) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = text;
      overflow.appendChild(option);
    }
    overflow.value = c.overflow || '';
    overflow.addEventListener('change', () => {
      c.overflow = overflow.value;
      changed();
    });
    box.appendChild(field('Overflow', overflow));
  }

  function removeCell() {
    if (!state.template || state.selected < 0) return;
    state.template.cells.splice(state.selected, 1);
    state.selected = -1;
    changed();
    renderInspector();
  }

  // ---- widgets page ----

  // cellSnippet writes the widgets page's cell the way a page file has it: YAML
  // flow style with the args that differ from their defaults, like the gallery
  // snippet the server builds (ADR 0055).
  function cellSnippet(c) {
    if (!c) return '';
    const [plugin, widget] = c.widget.split('/');
    const info = widgetInfo(plugin, widget);
    const known = (info && info.args) || {};
    const args = [];
    for (const [name, value] of Object.entries(c.args || {})) {
      if (isDefaultArg(value, known[name])) continue;
      args.push(name + ': ' + yamlValue(value));
    }
    args.sort();
    let s = '{ at: [0, 0], span: [' + c.span[0] + ', ' + c.span[1] + '], widget: ' + plugin + '/' + widget;
    if (args.length) s += ', args: { ' + args.join(', ') + ' }';
    return s + ' }';
  }

  // isDefaultArg keeps an arg out of the snippet when it already has the
  // value the plugin declares.
  function isDefaultArg(value, arg) {
    if (!arg || arg.default === undefined || arg.default === null) return false;
    return String(value) === String(arg.default);
  }

  function yamlValue(value) {
    if (typeof value === 'string') return plainYAML(value) ? value : JSON.stringify(value);
    if (Array.isArray(value)) return '[' + value.map(yamlValue).join(', ') + ']';
    return JSON.stringify(value);
  }

  // plainYAML reports whether a string can stand unquoted and still read back
  // as the same string.
  function plainYAML(s) {
    if (s === '' || jsonOrString(s) !== s) return false;
    if (['yes', 'no', 'on', 'off', 'null', '~', 'true', 'false', 'y', 'n'].includes(s.toLowerCase())) return false;
    return /^[A-Za-z0-9_\-\/.]+$/.test(s);
  }

  function updateSnippet() {
    const pre = el('snippet');
    if (!pre) return;
    const c = state.template && state.template.cells[0];
    pre.textContent = c ? cellSnippet(c) : '';
  }

  // updateURL keeps the widgets page's link in step with the widget, so the view
  // can be shared and a reload comes back to it, and so does the display type
  // switch, which opens the same cell on another display type. Args travel as
  // the plain text the gallery forms send, not as JSON: the script reads them
  // back by their declared type, so a JSON-quoted string would keep its quotes.
  function updateURL() {
    if (!state.widgetsPage) return;
    const c = state.template && state.template.cells[0];
    const params = widgetParams(c);
    const links = el('display-switch');
    if (links) {
      for (const a of links.querySelectorAll('a[data-display]')) {
        const own = new URLSearchParams(params);
        own.set('display', a.dataset.display);
        a.href = '/ui/playground?' + own.toString();
      }
    }
    history.replaceState(null, '', '/ui/playground?' + params.toString());
  }

  // widgetParams is the query of a widgets page link: the display type, the
  // widget and its size on the reference grid, and its args.
  function widgetParams(c) {
    const params = new URLSearchParams({ display: state.display });
    if (!c) return params;
    params.set('widget', c.widget);
    params.set('span', c.span[0] + 'x' + c.span[1]);
    const grid = cellGrid();
    params.set('grid', grid[0] + 'x' + grid[1]);
    for (const [name, value] of Object.entries(c.args || {})) {
      params.set('arg.' + name, Array.isArray(value) ? value.join(', ') : String(value));
    }
    return params;
  }

  async function copyCell() {
    const c = state.template && state.template.cells[0];
    if (!c) return;
    const text = cellSnippet(c);
    try {
      await navigator.clipboard.writeText(text);
      setStatus('cell copied', 'ok');
    } catch (e) {
      setStatus('copy failed — the cell text on the right is selected for copying', 'error');
    }
  }

  // ---- preview ----

  // setRendering marks the board while a preview is on its way; only the page
  // editor has the hint, so the widgets page skips it.
  function setRendering(on) {
    const hint = el('render-hint');
    if (hint) hint.textContent = on ? 'rendering…' : '';
  }

  function schedulePreview() {
    if (state.signedOut) return;
    clearTimeout(state.previewTimer);
    setRendering(true);
    state.previewTimer = setTimeout(preview, 350);
  }

  // signedOut reports a stale UI password (ADR 0096): the credentials the
  // browser cached no longer match, so every preview would fail the same way.
  // The page has to be reloaded, which asks for the new password.
  function signedOut() {
    state.signedOut = true;
    setRendering(false);
    return 'signed out — the UI password changed, reload the page to sign in again';
  }

  // showPanelResult puts the widgets page's answer into the preview panel: an
  // image, text lines or the error.
  async function showPanelResult(res) {
    const img = el('preview-img');
    const pre = el('preview-text');
    const error = el('preview-error');
    setRendering(false);
    if (res.status === 401) {
      img.hidden = pre.hidden = true;
      error.hidden = false;
      error.textContent = signedOut();
      return;
    }
    if (!res.ok) {
      let message = (await res.text()).trim();
      try {
        message = JSON.parse(message).error || message;
      } catch (e) {
        // not JSON: keep the text
      }
      img.hidden = pre.hidden = true;
      error.hidden = false;
      error.textContent = message || 'preview failed (' + res.status + ')';
      return;
    }
    error.hidden = true;
    if ((res.headers.get('Content-Type') || '').startsWith('text/plain')) {
      pre.textContent = (await res.text()).replace(/\n$/, '');
      pre.hidden = false;
      img.hidden = true;
      return;
    }
    const blob = await res.blob();
    if (state.previewURL) URL.revokeObjectURL(state.previewURL);
    state.previewURL = URL.createObjectURL(blob);
    img.src = state.previewURL;
    img.hidden = false;
    pre.hidden = true;
  }

  // clearBoardPreview drops a preview that no longer belongs to the page, so an
  // empty or broken draft does not keep the last picture on the board.
  function clearBoardPreview() {
    state.previewKind = '';
    state.previewLines = [];
    if (state.previewURL) {
      URL.revokeObjectURL(state.previewURL);
      state.previewURL = '';
    }
    applyBoardPreview();
  }

  // showBoardResult paints the draft preview into the board: an image becomes
  // its background, text lines fill the character slots. The cells are not
  // rebuilt, so the selection survives the answer.
  async function showBoardResult(res) {
    const error = el('preview-error');
    setRendering(false);
    if (res.status === 401) {
      clearBoardPreview();
      if (error) {
        error.hidden = false;
        error.textContent = signedOut();
      }
      return;
    }
    if (!res.ok) {
      let message = (await res.text()).trim();
      try {
        message = JSON.parse(message).error || message;
      } catch (e) {
        // not JSON: keep the text
      }
      clearBoardPreview();
      if (error) {
        error.hidden = false;
        error.textContent = message || 'preview failed (' + res.status + ')';
      }
      return;
    }
    if (error) error.hidden = true;
    if ((res.headers.get('Content-Type') || '').startsWith('text/plain')) {
      const body = (await res.text()).replace(/\n$/, '');
      state.previewLines = body.split('\n');
      state.previewKind = 'text';
      if (state.previewURL) {
        URL.revokeObjectURL(state.previewURL);
        state.previewURL = '';
      }
      applyBoardPreview();
      return;
    }
    const blob = await res.blob();
    if (state.previewURL) URL.revokeObjectURL(state.previewURL);
    state.previewURL = URL.createObjectURL(blob);
    state.previewKind = 'image';
    state.previewLines = [];
    applyBoardPreview();
  }

  async function preview() {
    if (state.widgetsPage) return previewPanel();
    const error = el('preview-error');
    if (!state.template) {
      clearBoardPreview();
      if (error) error.hidden = true;
      setRendering(false);
      return;
    }
    if (!state.template || !Array.isArray(state.template.cells) || state.template.cells.length === 0) {
      clearBoardPreview();
      if (error) {
        error.hidden = false;
        error.textContent = rowsPage() ? 'Place a cell to see a preview.' : 'Add a widget to see a preview.';
      }
      setRendering(false);
      return;
    }
    let res;
    try {
      res = await fetch('/api/preview/' + encodeURIComponent(state.display), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ template: previewBody() }),
        cache: 'no-store',
      });
    } catch (e) {
      clearBoardPreview();
      if (error) {
        error.hidden = false;
        error.textContent = 'preview failed: ' + e;
      }
      setRendering(false);
      return;
    }
    await showBoardResult(res);
  }

  // previewPanel is the widgets page's preview: one widget in its own size, not
  // the page around it.
  async function previewPanel() {
    const img = el('preview-img');
    const pre = el('preview-text');
    const error = el('preview-error');
    if (!state.template) {
      img.hidden = pre.hidden = error.hidden = true;
      setRendering(false);
      return;
    }
    if (!state.template.cells.length) {
      img.hidden = pre.hidden = true;
      error.hidden = false;
      error.textContent = 'Pick a widget from the palette.';
      setRendering(false);
      return;
    }
    await previewWidget();
  }

  // previewWidget renders the widgets page's widget in its own size, not the page
  // around it.
  async function previewWidget() {
    const img = el('preview-img');
    const pre = el('preview-text');
    const error = el('preview-error');
    const c = state.template.cells[0];
    const [plugin, widget] = c.widget.split('/');
    const grid = state.template.grid;
    const params = new URLSearchParams({ display: state.display, span: c.span[0] + 'x' + c.span[1],
      grid: grid[0] + 'x' + grid[1] });
    for (const [name, value] of Object.entries(c.args || {})) params.set('arg.' + name, JSON.stringify(value));
    let res;
    try {
      res = await fetch('/api/widget/' + encodeURIComponent(plugin) + '/' + encodeURIComponent(widget) + '?' + params.toString(), { cache: 'no-store' });
    } catch (e) {
      img.hidden = pre.hidden = true;
      error.hidden = false;
      error.textContent = 'preview failed: ' + e;
      setRendering(false);
      return;
    }
    await showPanelResult(res);
  }

  // ---- load and save ----

  async function loadPlugins() {
    try {
      const res = await fetch('/api/plugins', { cache: 'no-store' });
      if (!res.ok) return [];
      return (await res.json()).plugins || [];
    } catch (e) {
      return [];
    }
  }

  async function load() {
    setStatus('');
    state.gridRatio = null;
    if (state.widgetsPage) {
      loadWidgetsPage();
      return;
    }
    // A new page has no file yet; it starts from the preview display type and
    // never from another file of the page (ADR 0110).
    if (start.new) {
      state.template = blank();
      state.hash = '';
      state.readOnly = false;
      state.dirty = false;
      state.selected = -1;
      renderAll();
      return;
    }
    // The open file is named exactly: without a label the page file, else the
    // variant with that label (ADR 0110).
    const res = await fetch('/api/pages/' + encodeURIComponent(state.name) + '?file=' + encodeURIComponent(state.file), { cache: 'no-store' });
    if (res.status === 401) {
      setStatus(signedOut(), 'error');
      return;
    }
    if (res.status === 404) {
      state.template = blank();
      state.hash = '';
      state.readOnly = false;
      state.dirty = false;
      state.selected = -1;
      renderAll();
      return;
    }
    if (!res.ok) {
      setStatus('reading the page failed (' + res.status + ')', 'error');
      return;
    }
    const doc = await res.json();
    state.file = doc.file || '';
    state.hash = doc.hash || '';
    if (!state.file) state.pageHash = state.hash;
    state.selected = -1;
    state.dirty = false;
    if (doc.error) {
      // A broken file answers with its error and no model. Editing it would
      // write over the text the operator still has to repair.
      state.template = null;
      state.readOnly = true;
      setStatus('this file does not read as a page: ' + doc.error, 'error');
    } else {
      state.template = normalize(doc.template || blank());
      state.readOnly = false;
    }
    renderAll();
  }

  // loadWidgetsPage starts the widgets page: it fills the single cell from the
  // link when it names a widget, else with the first fitting widget, so the
  // preview never opens on an error.
  function loadWidgetsPage() {
    state.template = blank();
    state.hash = '';
    state.readOnly = false;
    state.dirty = false;
    state.selected = -1;
    if (start.widget) {
      const [plugin, widget] = String(start.widget).split('/');
      if (plugin && widget) {
        setWidget(plugin, widget);
        const c = state.template.cells[0];
        const info = widgetInfo(plugin, widget);
        const known = (info && info.args) || {};
        for (const [name, raw] of Object.entries(start.args || {})) c.args[name] = argValue(raw, known[name]);
        if (Array.isArray(start.span) && start.span[0] > 0) {
          const grid = state.template.grid;
          c.span = [Math.max(1, Math.min(start.span[0], grid[0])), Math.max(1, Math.min(start.span[1], grid[1]))];
        }
      }
    } else {
      // The Playground offers image display types only (ADR 0124).
      const first = firstWidget('image');
      if (first) setWidget(first.plugin, first.widget);
    }
    renderAll();
    updateURL();
  }

  async function save() {
    if (!state.template) return;
    if (state.widgetsPage) return copyCell();
    setStatus('saving…', '');
    let res;
    try {
      res = await fetch('/api/pages/' + encodeURIComponent(state.name) + '?file=' + encodeURIComponent(state.file), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hash: state.hash, template: serialize() }),
      });
    } catch (e) {
      setStatus('save failed: ' + e, 'error');
      return;
    }
    if (res.status === 401) {
      setStatus(signedOut(), 'error');
      return;
    }
    let doc = {};
    const body = await res.text();
    try {
      doc = JSON.parse(body);
    } catch (e) {
      doc.error = body.trim();
    }
    if (res.status === 409) {
      setStatus('the file changed on disk — Reload shows the newest version, Save would overwrite it', 'error');
      return;
    }
    if (!res.ok) {
      setStatus(doc.error || 'save failed (' + res.status + ')', 'error');
      return;
    }
    const firstSave = !state.pageExists && !state.file;
    state.hash = doc.hash || '';
    state.file = doc.file || '';
    if (!state.file) {
      state.pageHash = state.hash;
      state.pageExists = true;
    }
    if (doc.template) state.template = normalize(doc.template);
    state.dirty = false;
    setStatus('saved ' + new Date().toLocaleTimeString(), 'ok');
    if (firstSave) {
      // The coverage, the rename and the variant actions appear with the file.
      location.reload();
      return;
    }
    renderAll();
  }

  // createVariant writes the layout of a display type in one action (ADR 0110).
  // The variant starts from the display type: kind and grid come from the type,
  // never from the open file, which is the defect of issue #238.
  async function createVariant(display, label) {
    const info = state.displays.find((d) => d.name === display) || {};
    const kind = info.kind === 'text' ? 'text' : 'image';
    const grid = kind === 'text' ? [info.width, info.height]
      : (info.grid && info.grid.length ? info.grid.slice() : state.reference.slice());
    const template = kind === 'text'
      ? { kind: 'text', overflow: 'strip', cells: [{ at: [0, 0], span: [Math.max(1, info.width), 1], text: 'Text' }], style: {} }
      : { kind: kind, grid: grid, style: {}, cells: [blankCell(kind, grid)] };
    setStatus('creating the variant for ' + display + '…', '');
    let res;
    try {
      res = await fetch('/api/pages/' + encodeURIComponent(state.name) + '/variant?display=' + encodeURIComponent(display) +
        '&file=' + encodeURIComponent(label), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hash: state.pageHash, template: template }),
      });
    } catch (e) {
      setStatus('creating the variant failed: ' + e, 'error');
      return;
    }
    if (res.status === 401) {
      setStatus(signedOut(), 'error');
      return;
    }
    let doc = {};
    const body = await res.text();
    try {
      doc = JSON.parse(body);
    } catch (e) {
      doc.error = body.trim();
    }
    if (res.status === 409) {
      setStatus('the page changed on disk — Reload shows the newest version', 'error');
      return;
    }
    if (!res.ok) {
      setStatus(doc.error || 'creating the variant failed (' + res.status + ')', 'error');
      return;
    }
    state.dirty = false;
    const params = new URLSearchParams({ display: display });
    if (doc.file) params.set('file', doc.file);
    location.href = pageHref(state.name, params);
  }

  // changeServes writes the file that serves one display type (ADR 0110). The
  // empty label is the page file; the server refuses a file that does not fit
  // the type and a page that changed since it was read.
  async function changeServes(select) {
    const before = select.dataset.value || '';
    // The map is written right away and the view reloads, which would drop an
    // unsaved layout. Ask for a save first (ADR 0110).
    if (state.dirty) {
      select.value = before;
      setStatus('save the layout first; changing a variant reloads the page', 'error');
      return;
    }
    const display = select.dataset.serves;
    const file = select.value === '__none__' ? '' : select.value;
    setStatus('changing the file for ' + display + '…', '');
    let res;
    try {
      res = await fetch('/api/pages/' + encodeURIComponent(state.name) + '/serves?display=' + encodeURIComponent(display), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hash: state.pageHash, file: file }),
      });
    } catch (e) {
      select.value = before;
      setStatus('changing the file failed: ' + e, 'error');
      return;
    }
    if (res.status === 401) {
      select.value = before;
      setStatus(signedOut(), 'error');
      return;
    }
    let doc = {};
    const body = await res.text();
    try {
      doc = JSON.parse(body);
    } catch (e) {
      doc.error = body.trim();
    }
    if (!res.ok) {
      select.value = before;
      setStatus(doc.error || 'changing the file failed (' + res.status + ')', 'error');
      return;
    }
    select.dataset.value = file;
    if (doc.hash) state.pageHash = doc.hash;
    // The preview stays on a type the open file still serves; otherwise the
    // next view starts from the first served type.
    const params = new URLSearchParams(location.search);
    const covered = (doc.coverage || []).find((c) => c.display === display);
    const mine = covered && !covered.missing && !covered.error && (covered.file || '') === (state.file || '');
    if (display === state.display && !mine) params.delete('display');
    const query = params.toString();
    location.href = location.pathname + (query ? '?' + query : '');
  }

  // blankCell starts a variant with the first widget the palette offers for the
  // kind of the display type.
  function blankCell(kind, grid) {
    const first = firstWidget(kind);
    const info = first ? widgetInfo(first.plugin, first.widget) : null;
    let span = kind === 'text'
      ? [grid[0], 1]
      : (info && info.sizes && info.sizes.length ? [info.sizes[0].columns, info.sizes[0].rows] : [2, 1]);
    span = [Math.max(1, Math.min(span[0], grid[0])), Math.max(1, Math.min(span[1], grid[1]))];
    return { at: [0, 0], span: span, widget: first ? first.plugin + '/' + first.widget : 'paperboy/header', args: {}, style: {} };
  }

  // variantURL is one action on the open variant file: a new label or another
  // page (ADR 0110).
  function variantURL(action) {
    return '/api/pages/' + encodeURIComponent(state.name) + '/variant/' + action +
      '?from=' + encodeURIComponent(state.file);
  }

  // showDialogError puts the answer of a refused action into its dialog, so the
  // operator reads it where the form is.
  function showDialogError(form, message) {
    const box = form.querySelector('[data-error]');
    if (!box) return;
    box.textContent = message || '';
    box.hidden = !message;
  }

  // postAction sends one variant action and answers its JSON body, or the reason
  // the server named.
  async function postAction(url, body) {
    let res;
    try {
      res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
    } catch (e) {
      return { error: 'request failed: ' + e };
    }
    if (res.status === 401) return { error: signedOut() };
    let doc = {};
    const text = await res.text();
    try {
      doc = JSON.parse(text);
    } catch (e) {
      doc = { error: text.trim() };
    }
    if (!res.ok) return { error: doc.error || '(' + res.status + ')' };
    return { doc: doc };
  }

  // openMovedFile follows a variant action: the answer names the editor link of
  // the file at its new place, with a display type it renders (ADR 0110).
  function openMovedFile(doc, name, label) {
    state.dirty = false;
    if (doc && doc.url) {
      location.href = doc.url;
      return;
    }
    const params = new URLSearchParams({ display: state.display, file: label });
    location.href = pageHref(name, params);
  }

  // renameVariant gives the open variant a new label; the page rewrites every
  // entry that names the old one (ADR 0110).
  async function renameVariant(form, label) {
    if (!label || !state.file) return;
    setStatus('renaming the variant…', '');
    const res = await postAction(variantURL('rename'), { name: label, hash: state.pageHash });
    if (res.error) {
      setStatus('rename failed: ' + res.error, 'error');
      showDialogError(form, res.error);
      return;
    }
    openMovedFile(res.doc, res.doc.name || state.name, res.doc.file || label);
  }

  // reassignVariant moves the open variant to another page and takes its entries
  // with it (ADR 0110).
  async function reassignVariant(form, page) {
    if (!page || !state.file) return;
    setStatus('moving the variant…', '');
    const res = await postAction(variantURL('move'), { page: page, hash: state.pageHash });
    if (res.error) {
      setStatus('reassign failed: ' + res.error, 'error');
      showDialogError(form, res.error);
      return;
    }
    openMovedFile(res.doc, res.doc.name || page, res.doc.file || state.file);
  }

  // rename moves the page file and its variants, so the open page keeps its
  // files under the new name; the plans that name the page keep the old name,
  // so the confirmation names them (ADR 0079, 0113).
  async function rename() {
    if (!state.rename) return;
    if (state.dirty && !(await pbAsk.confirm({
      title: 'Discard unsaved changes?',
      text: 'The file keeps what is on disk.',
      ok: 'Discard',
    }))) return;
    const name = await pbAsk.text({
      title: 'Rename page',
      label: 'New name for ' + state.name,
      value: state.name,
      pattern: '[a-z0-9-]{1,40}',
      hint: 'Lower case letters, digits and -.',
    });
    if (!name || name === state.name) return;
    setStatus('renaming…', '');
    let res;
    try {
      res = await fetch(state.rename.url + (state.pageHash ? '?hash=' + encodeURIComponent(state.pageHash) : ''), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
    } catch (e) {
      setStatus('rename failed: ' + e, 'error');
      return;
    }
    if (!res.ok) {
      let msg = '(' + res.status + ')';
      try {
        msg = (await res.json()).error || msg;
      } catch (e) { /* not JSON */ }
      setStatus('rename failed: ' + msg, 'error');
      return;
    }
    state.dirty = false;
    location.href = pageHref(name, new URLSearchParams({ display: state.display }));
  }

  // remove deletes the one file that was opened, after naming the plans that use
  // the page; the server refuses one that changed since it was read (ADR 0073,
  // 0110).
  async function remove() {
    if (!state.hash) return;
    const used = state.usedBy || [];
    let text = (used.length ? 'Plans that name it: ' + used.join(', ') + '.' : 'No plan names this page.') +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '') +
      '\n\nA copy stays under state/editor/.';
    if (state.file && state.named) {
      text += '\n\nThe page still names this file: its display type has no page until the file is back or the entry is removed.';
    }
    if (!(await pbAsk.confirm({ title: 'Delete ' + targetPath() + '?', text: text, ok: 'Delete', danger: true }))) return;
    setStatus('deleting…', '');
    let res;
    try {
      res = await fetch('/api/pages/' + encodeURIComponent(state.name) +
        '?file=' + encodeURIComponent(state.file) +
        '&hash=' + encodeURIComponent(state.hash), { method: 'DELETE' });
    } catch (e) {
      setStatus('delete failed: ' + e, 'error');
      return;
    }
    if (!res.ok) {
      const body = await res.text();
      let doc = {};
      try {
        doc = JSON.parse(body);
      } catch (e) {
        doc.error = body.trim();
      }
      setStatus(doc.error || 'delete failed (' + res.status + ')', 'error');
      return;
    }
    state.dirty = false;
    location.href = '/ui/pages';
  }

  function renderAll() {
    renderSettings();
    renderPalette();
    renderBoard();
    renderInspector();
    updateFile();
    updateWhere();
    updateSave();
    schedulePreview();
  }

  // ---- wiring ----

  // The widgets page shares this script but not every panel, so each hook asks
  // whether its element is there.
  const on = (id, event, handler) => {
    const node = el(id);
    if (node) node.addEventListener(event, handler);
  };

  on('save', 'click', save);
  on('copy-cell', 'click', copyCell);
  on('reload', 'click', () => location.reload());
  on('delete', 'click', remove);
  on('rename', 'click', rename);
  // The Variants table writes the page's variants map: the select names the
  // file of one display type (ADR 0110).
  for (const select of document.querySelectorAll('select[data-serves]')) {
    select.addEventListener('change', () => changeServes(select));
  }
  // Create variant asks for a display type and a file label, then writes an
  // empty layout that starts from the type (ADR 0110, issue #238).
  const variantForm = document.querySelector('form[data-create-variant]');
  if (variantForm) {
    const types = variantForm.querySelector('select[name=display]');
    const label = variantForm.querySelector('input[name=label]');
    types.addEventListener('change', () => {
      const option = types.options[types.selectedIndex];
      if (option) label.value = option.dataset.label || option.value;
    });
    variantForm.addEventListener('submit', (e) => {
      e.preventDefault();
      const display = types.value;
      const file = label.value.trim();
      if (!display || !file) return;
      createVariant(display, file);
    });
  }
  // A variant renames its label or moves to another page; both rewrite the
  // page's variants map and move the file (ADR 0110).
  const renameVariantForm = document.querySelector('form[data-rename-variant]');
  if (renameVariantForm) {
    renameVariantForm.addEventListener('submit', (e) => {
      e.preventDefault();
      renameVariant(renameVariantForm, renameVariantForm.querySelector('input[name=label]').value.trim());
    });
  }
  const reassignForm = document.querySelector('form[data-reassign]');
  if (reassignForm) {
    reassignForm.addEventListener('submit', (e) => {
      e.preventDefault();
      const page = reassignForm.querySelector('select[name=page]');
      if (page) reassignVariant(reassignForm, page.value);
    });
  }
  // The Create variant button opens its dialog; a click beside the form closes
  // it again.
  for (const b of document.querySelectorAll('button[data-dialog]')) {
    b.addEventListener('click', () => {
      const d = document.getElementById(b.dataset.dialog);
      if (!d || b.disabled) return;
      for (const box of d.querySelectorAll('[data-error]')) {
        box.textContent = '';
        box.hidden = true;
      }
      d.showModal();
      const first = d.querySelector('select[name], input[name]');
      if (first) first.focus();
    });
  }
  for (const d of document.querySelectorAll('dialog')) {
    for (const b of d.querySelectorAll('button[data-close]')) {
      b.addEventListener('click', () => d.close());
    }
    d.addEventListener('click', (e) => {
      if (e.target === d) d.close();
    });
  }
  on('palette-filter', 'input', () => {
    state.filter = el('palette-filter').value;
    renderPalette();
  });
  // The palette answers to the keyboard: / focuses the filter, the arrow keys
  // walk the entries and Enter places the focused one (ADR 0152).
  const paletteButtons = () => Array.from(el('palette').querySelectorAll('button.widget'));
  on('palette-filter', 'keydown', (e) => {
    if (e.key !== 'ArrowDown') return;
    const first = paletteButtons()[0];
    if (!first) return;
    e.preventDefault();
    first.focus();
  });
  on('palette', 'keydown', (e) => {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    const list = paletteButtons();
    const at = list.indexOf(document.activeElement);
    if (at < 0) return;
    const next = list[at + (e.key === 'ArrowDown' ? 1 : -1)];
    if (!next) return;
    e.preventDefault();
    next.focus();
  });
  document.addEventListener('keydown', (e) => {
    const tag = (e.target.tagName || '').toLowerCase();
    if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
    if (!el('palette-filter')) return;
    e.preventDefault();
    el('palette-filter').focus();
  });
  // The Cells switch over the board; the choice is remembered (ADR 0074).
  if (el('show-cells')) {
    let saved = '1';
    try {
      saved = localStorage.getItem('paperboy.editor.cells') || '1';
    } catch (e) {
      // storage may be off; the boxes stay visible
    }
    el('show-cells').checked = saved !== '0';
    on('show-cells', 'change', () => setCellsVisible(el('show-cells').checked));
  }
  // The board takes every drop: a palette widget is placed on the cell under
  // the pointer, a dragged box moves there.
  on('board', 'dragover', boardDragOver);
  on('board', 'drop', boardDrop);
  window.addEventListener('resize', () => applyBoardPreview());
  on('title', 'input', () => {
    if (!state.template) return;
    state.template.title = el('title').value;
    changed();
  });
  on('cols', 'change', () => applyGrid('cols'));
  on('rows', 'change', () => applyGrid('rows'));
  // The grid lock is a preference like the Cells switch; it is not part of the
  // page file (ADR 0128).
  if (el('grid-lock')) {
    try {
      state.gridLock = localStorage.getItem('paperboy.editor.gridlock') === '1';
    } catch (e) {
      state.gridLock = false;
    }
    on('grid-lock', 'click', () => setGridLock(!state.gridLock));
  }
  for (const [id, key] of [['padding', 'padding'], ['rule', 'rule'], ['text-size', 'text_size'], ['divider', 'divider']]) {
    on(id, 'input', () => {
      if (!state.template) return;
      state.template.style = state.template.style || {};
      const v = num(el(id).value);
      if (v === null) delete state.template.style[key];
      else state.template.style[key] = v;
      changed();
    });
  }
  on('notices', 'change', () => {
    if (!state.template) return;
    if (el('notices').value === 'none') state.template.notices = 'none';
    else delete state.template.notices;
    changed();
  });
  on('overflow', 'change', () => {
    if (!state.template || !rowsPage()) return;
    state.template.overflow = el('overflow').value || 'strip';
    changed();
  });
  document.addEventListener('keydown', (e) => {
    const typing = /^(INPUT|SELECT|TEXTAREA)$/.test(e.target.tagName);
    if (e.key === 'Escape') {
      if (state.placing) {
        state.placing = null;
        renderPalette();
        renderBoard();
      } else if (state.selected >= 0) select(-1);
      return;
    }
    if (!typing && (e.key === 'Delete' || e.key === 'Backspace') && state.selected >= 0) {
      e.preventDefault();
      removeCell();
    }
  });
  window.addEventListener('beforeunload', (e) => {
    if (!state.dirty) return;
    e.preventDefault();
    e.returnValue = '';
  });

  (async () => {
    state.plugins = await loadPlugins();
    await load();
  })();
})();
