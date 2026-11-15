// Page editor for Paperboy (ADR 0068). Vanilla JavaScript, no build step: the
// page model and the plugin schema come from the page API (ADR 0066), edits
// stay in the browser until Save writes the file with the hash it was read
// with.
(() => {
  'use strict';

  const el = (id) => document.getElementById(id);
  const start = JSON.parse(el('editor-data').textContent);

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
    placing: null, // {plugin, widget} waiting for a cell
    filter: '', // text of the palette filter
    collapsed: new Set(), // plugins whose widgets the palette hides
    plugins: [],
    dirty: false,
    previewURL: '',
    previewTimer: 0,
    signedOut: false, // the UI password changed: stop previews until the page reloads (ADR 0096)
    previewKind: '', // '' none, 'image' or 'text'
    previewLines: [], // the text lines of a text display, one char per cell
  };

  const text = () => state.kind === 'text';
  // A text page is a layout of rows of values (ADR 0111). The palette offers
  // the declared values, and the inspector edits rows and cells.
  const rowsPage = () => !state.widgetsPage && text();
  const hasRows = () => !!(state.template && Array.isArray(state.template.rows) && state.template.rows.length);
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
    el('where').textContent = [
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
    const content = rowsPage() ? hasRows() : !!state.template && (!state.widgetsPage || state.template.cells.length > 0);
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
    else renderInspector();
    schedulePreview();
  }

  // ---- model ----

  function blank() {
    if (rowsPage()) {
      return {
        title: state.file ? '' : state.name,
        kind: 'text',
        rows: [[{ value: firstValueKey(), label: '', unit: '', width: Math.max(1, state.width), align: 'left' }]],
        style: {},
      };
    }
    const grid = text() ? [state.width, state.height] : (state.widgetsPage ? state.reference.slice() : state.grid.slice());
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
      t.rows = (t.rows || []).map((row) => (row || []).map((c) => ({
        value: c.value || '',
        label: c.label || '',
        unit: c.unit || '',
        width: Math.max(1, Math.round(Number(c.width) || 1)),
        align: c.align === 'right' || c.align === 'decimal' ? c.align : 'left',
      })));
      t.cells = [];
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
      ? { kind: 'text', style: styleOut(t.style || {}), rows: t.rows.map((row) => row.map((c) => {
        const cell = { value: c.value, width: c.width };
        if (c.label) cell.label = c.label;
        if (c.unit) cell.unit = c.unit;
        if (c.align && c.align !== 'left') cell.align = c.align;
        return cell;
      })) }
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
        out.push({ plugin: p.name, name: v.name, title: v.title || v.name, type: v.type || '', unit: v.unit || '' });
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

  // defaultTextCell is a new cell for a row; width stays within the display.
  function defaultTextCell(width) {
    return {
      value: firstValueKey(),
      label: '',
      unit: '',
      width: Math.max(1, Math.min(Math.round(width || 6), state.width)),
      align: 'left',
    };
  }

  function textRowWidth(row) {
    return (row || []).reduce((sum, c) => sum + (Number(c.width) || 0), 0);
  }

  function addTextRow() {
    if (!state.template) return;
    state.template.rows = state.template.rows || [];
    state.template.rows.push([defaultTextCell(state.width)]);
    changed();
  }

  function addTextCell(rowIndex) {
    const row = state.template && state.template.rows && state.template.rows[rowIndex];
    if (!row) return;
    const room = Math.max(1, state.width - textRowWidth(row));
    row.push(defaultTextCell(Math.min(6, room)));
    changed();
  }

  function removeTextRow(rowIndex) {
    if (!state.template || !state.template.rows) return;
    state.template.rows.splice(rowIndex, 1);
    changed();
  }

  function moveTextRow(rowIndex, delta) {
    const rows = state.template && state.template.rows;
    if (!rows) return;
    const to = rowIndex + delta;
    if (to < 0 || to >= rows.length) return;
    const [row] = rows.splice(rowIndex, 1);
    rows.splice(to, 0, row);
    changed();
  }

  function removeTextCell(rowIndex, cellIndex) {
    const row = state.template && state.template.rows && state.template.rows[rowIndex];
    if (!row) return;
    row.splice(cellIndex, 1);
    changed();
  }

  // addValueCell appends a palette value to the last row, or starts a row.
  function addValueCell(plugin, name) {
    if (!state.template) return;
    state.template.rows = state.template.rows || [];
    if (!state.template.rows.length || textRowWidth(state.template.rows[state.template.rows.length - 1]) >= state.width) {
      state.template.rows.push([]);
    }
    const row = state.template.rows[state.template.rows.length - 1];
    const room = Math.max(1, state.width - textRowWidth(row));
    const cell = defaultTextCell(Math.min(6, room));
    cell.value = plugin + '/' + name;
    row.push(cell);
    changed();
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
    el('title').disabled = off || state.file !== '';
    el('cols').disabled = off || text();
    el('rows').disabled = off || text();
    el('grid-note').textContent = rowsPage()
      ? 'Text page: ' + state.width + '×' + state.height + ' characters, one slot per character. Edit the rows on the right.'
      : text()
        ? 'The grid is the character grid of the display and cannot be changed.'
        : 'Cells of the page; plugins declare their sizes on a ' + state.reference[0] + '×' + state.reference[1] + ' reference grid.';
    if (!t) {
      for (const id of ['title', 'cols', 'rows', 'padding', 'rule', 'text-size', 'divider']) el(id).value = '';
      return;
    }
    const s = t.style || {};
    el('title').value = t.title || state.page_title || '';
    el('cols').value = rowsPage() ? state.width : t.grid[0];
    el('rows').value = rowsPage() ? state.height : t.grid[1];
    el('padding').value = s.padding === undefined || s.padding === null ? '' : s.padding;
    el('rule').value = s.rule || '';
    el('text-size').value = s.text_size || '';
    el('divider').value = s.divider || '';
    el('notices').value = t.notices === 'none' ? 'none' : 'show';
  }

  function applyGrid() {
    const t = state.template;
    if (!t || text()) return;
    const cols = Math.max(1, Math.min(80, Math.round(num(el('cols').value) || t.grid[0])));
    const rows = Math.max(1, Math.min(80, Math.round(num(el('rows').value) || t.grid[1])));
    for (const c of t.cells) {
      if (c.at[0] + c.span[0] > cols || c.at[1] + c.span[1] > rows) {
        setStatus('a widget leaves the ' + cols + '×' + rows + ' grid; move or delete it first', 'error');
        el('cols').value = t.grid[0];
        el('rows').value = t.grid[1];
        return;
      }
    }
    if (cols === t.grid[0] && rows === t.grid[1]) {
      el('cols').value = cols;
      el('rows').value = rows;
      return;
    }
    t.grid = [cols, rows];
    changed();
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
        b.className = 'widget';
        b.textContent = v.name + ' — ' + (v.title || v.name);
        const note = document.createElement('span');
        note.className = 'note';
        note.textContent = [v.type, v.unit].filter(Boolean).join(' · ');
        if (note.textContent) b.appendChild(note);
        b.addEventListener('click', () => addValueCell(p.name, v.name));
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

  function renderPalette() {
    const box = el('palette');
    box.innerHTML = '';
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
    const kind = text() ? 'text' : 'image';
    const filter = state.filter.trim().toLowerCase();
    let shown = 0;
    for (const p of state.plugins) {
      const widgets = (p.description && p.description.widgets) || [];
      const fitting = widgets.filter((w) => !w.kinds || !w.kinds.length || w.kinds.includes(kind));
      const matches = fitting.filter((w) => !filter ||
        (p.name + ' ' + w.widget + ' ' + (w.title || '')).toLowerCase().includes(filter));
      if (filter && !matches.length) continue;
      const section = document.createElement('div');
      section.className = 'plugin';
      const head = document.createElement('div');
      head.className = 'plugin-head';
      head.title = 'Show or hide the widgets of this plugin';
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
      for (const w of matches) {
        shown++;
        const b = document.createElement('button');
        b.type = 'button';
        b.className = 'widget' + (state.placing && state.placing.plugin === p.name && state.placing.widget === w.widget ? ' active' : '');
        b.textContent = w.widget + (w.title ? ' — ' + w.title : '');
        if (w.sizes && w.sizes.length) {
          const s = document.createElement('span');
          s.className = 'note';
          s.textContent = w.sizes.map((size) => size.columns + '×' + size.rows).join(', ');
          b.appendChild(s);
        }
        b.addEventListener('click', () => pick(p.name, w.widget));
        b.draggable = true;
        b.addEventListener('dragstart', (e) => {
          e.dataTransfer.setData('text/plain', p.name + '/' + w.widget);
          e.dataTransfer.effectAllowed = 'copy';
        });
        section.appendChild(b);
      }
      if (!fitting.length) {
        const note = document.createElement('span');
        note.className = 'note';
        note.textContent = 'no widget for ' + kind + ' displays';
        section.appendChild(note);
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
  // size its plugin declares (the whole display for text).
  function setWidget(plugin, widget) {
    const info = widgetInfo(plugin, widget);
    let span = text()
      ? [state.width, state.height]
      : (info && info.sizes && info.sizes.length ? [info.sizes[0].columns, info.sizes[0].rows] : [2, 1]);
    const grid = state.template.grid;
    span = [Math.max(1, Math.min(span[0], grid[0])), Math.max(1, Math.min(span[1], grid[1]))];
    state.template.cells = [{ at: [0, 0], span: span, widget: plugin + '/' + widget, args: {}, style: {} }];
    state.selected = 0;
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
    let span = info && info.sizes && info.sizes.length ? [info.sizes[0].columns, info.sizes[0].rows] : [1, 1];
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
  function selectCell(i) {
    state.selected = i;
    const board = el('board');
    if (board) {
      for (const node of board.querySelectorAll('.cell')) {
        node.classList.toggle('selected', Number(node.dataset.i) === i);
      }
    }
    renderInspector();
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

  // boardDrop places a widget that was dragged out of the palette: the cell
  // under the pointer is the top-left corner of the new box.
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
          if (state.placing) place(state.placing.plugin, state.placing.widget, x, y);
          else select(-1);
        });
        board.appendChild(slot);
      }
    }
    if (rowsPage()) {
      el('board-note').textContent = t.rows.length + ' rows, edited by hand in ' + targetPath() + '.';
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
    let used = 0;
    for (const c of t.cells) used += c.span[0] * c.span[1];
    let note = t.grid[0] + '×' + t.grid[1] + ' cells, ' + used + ' of ' + t.grid[0] * t.grid[1] + ' covered';
    if (state.placing) note += ' · click a free cell to place ' + state.placing.plugin + '/' + state.placing.widget;
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
    // The pixel font fills the glyph box of one slot (ADR 0115).
    const cell = board.querySelector('.slot');
    if (cell) {
      const h = cell.getBoundingClientRect().height;
      if (h > 0) board.style.setProperty('--lcd-glyph', (h * 1.1) + 'px');
    }
  }

  function select(i) {
    state.selected = i;
    renderBoard();
    renderInspector();
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
    head.textContent = rowsPage() ? 'Rows' : state.widgetsPage ? 'Widget' : 'Cell';
    box.appendChild(head);
    if (rowsPage()) {
      renderRowsEditor(box);
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
    const args = (info && info.args) || {};
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
        none.textContent = arg.default === undefined || arg.default === null ? 'default' : 'default (' + arg.default + ')';
        control.appendChild(none);
        for (const value of arg.enum) {
          const option = document.createElement('option');
          option.value = String(value);
          option.textContent = enumLabel(arg, value);
          control.appendChild(option);
        }
        control.value = current === undefined ? '' : String(current);
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
        const grid = document.createElement('div');
        grid.className = 'checks-grid';
        const boxes = [];
        let count = null;
        for (const value of arg.enum) {
          const item = document.createElement('label');
          item.className = 'check';
          const box = document.createElement('input');
          box.type = 'checkbox';
          box.value = String(value);
          box.checked = chosen.includes(String(value));
          const text = document.createElement('span');
          text.textContent = enumLabel(arg, value);
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
        if (arg.enum.length > 3) {
          const bar = document.createElement('div');
          bar.className = 'checks-bar';
          count = document.createElement('span');
          count.className = 'checks-count';
          count.textContent = chosen.length + ' of ' + arg.enum.length;
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

    const buttons = document.createElement('div');
    buttons.className = 'row-buttons';
    const remove = document.createElement('button');
    remove.type = 'button';
    remove.textContent = state.widgetsPage ? 'Clear' : 'Delete cell';
    remove.addEventListener('click', removeCell);
    buttons.appendChild(remove);
    box.appendChild(buttons);
    updateSnippet();
    updateSave();
  }

  // renderRowsEditor edits the rows and cells of a text page (ADR 0111). The
  // inputs keep their focus because changed('inspector') does not rebuild them.
  function renderRowsEditor(box) {
    const t = state.template;
    if (!t) {
      const broken = document.createElement('p');
      broken.className = 'empty';
      broken.textContent = 'The file does not read as a page; fix it by hand and reload.';
      box.appendChild(broken);
      return;
    }
    const hint = document.createElement('p');
    hint.className = 'hint';
    hint.textContent = 'One character per slot, ' + state.width + ' columns and ' + state.height + ' rows. A row may be shorter than the display.';
    box.appendChild(hint);
    if (!t.rows || !t.rows.length) {
      const empty = document.createElement('p');
      empty.className = 'empty';
      empty.textContent = 'No rows yet.';
      box.appendChild(empty);
    }
    const values = textValues();
    t.rows.forEach((row, y) => {
      const rowBox = document.createElement('div');
      rowBox.className = 'row-editor';
      const head = document.createElement('div');
      head.className = 'row-head';
      const title = document.createElement('strong');
      title.textContent = 'Row ' + (y + 1);
      const width = document.createElement('span');
      width.className = 'muted' + (textRowWidth(row) > state.width ? ' over' : '');
      width.textContent = textRowWidth(row) + '/' + state.width;
      const spacer = document.createElement('span');
      spacer.className = 'spacer';
      head.append(title, width, spacer);
      const up = document.createElement('button');
      up.type = 'button';
      up.textContent = '↑';
      up.title = 'Move this row up';
      up.disabled = y === 0;
      up.addEventListener('click', () => moveTextRow(y, -1));
      const down = document.createElement('button');
      down.type = 'button';
      down.textContent = '↓';
      down.title = 'Move this row down';
      down.disabled = y === t.rows.length - 1;
      down.addEventListener('click', () => moveTextRow(y, 1));
      const remove = document.createElement('button');
      remove.type = 'button';
      remove.className = 'danger';
      remove.textContent = 'Remove row';
      remove.addEventListener('click', () => removeTextRow(y));
      head.append(up, down, remove);
      rowBox.appendChild(head);
      row.forEach((c, i) => rowBox.appendChild(textCellEditor(c, y, i, values)));
      const add = document.createElement('button');
      add.type = 'button';
      add.textContent = 'Add cell';
      add.addEventListener('click', () => addTextCell(y));
      rowBox.appendChild(add);
      box.appendChild(rowBox);
    });
    const addRow = document.createElement('button');
    addRow.type = 'button';
    addRow.className = 'primary';
    addRow.textContent = 'Add row';
    addRow.addEventListener('click', addTextRow);
    box.appendChild(addRow);
    if (!values.length) {
      const note = document.createElement('p');
      note.className = 'hint';
      note.textContent = 'No plugin declares a value. Configure a plugin that speaks protocol 7 (ADR 0111).';
      box.appendChild(note);
    }
  }

  // textCellEditor is one editable cell: value, label, unit, width and align.
  function textCellEditor(c, rowIndex, cellIndex, values) {
    const wrap = document.createElement('div');
    wrap.className = 'text-cell';
    const head = document.createElement('div');
    head.className = 'row-head';
    const name = document.createElement('strong');
    name.textContent = 'Cell ' + (cellIndex + 1);
    const spacer = document.createElement('span');
    spacer.className = 'spacer';
    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'danger';
    remove.textContent = 'Remove';
    remove.addEventListener('click', () => removeTextCell(rowIndex, cellIndex));
    head.append(name, spacer, remove);
    wrap.appendChild(head);

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
    // A value a plugin no longer declares stays visible and is preserved.
    if (c.value && !values.some((v) => valueKey(v) === c.value)) {
      const option = document.createElement('option');
      option.value = c.value;
      option.textContent = c.value + ' — not declared';
      select.appendChild(option);
    }
    select.value = c.value || '';
    select.addEventListener('change', () => {
      c.value = select.value;
      changed();
    });
    wrap.appendChild(field('Value', select));
    const info = valueInfo(c.value);
    if (info) {
      const note = document.createElement('span');
      note.className = 'desc';
      note.textContent = [info.type, info.unit].filter(Boolean).join(' · ');
      wrap.appendChild(note);
    }

    const label = document.createElement('input');
    label.type = 'text';
    label.autocomplete = 'off';
    label.value = c.label || '';
    label.addEventListener('input', () => {
      c.label = label.value;
      changed('inspector');
    });
    wrap.appendChild(field('Label', label));

    const unit = document.createElement('input');
    unit.type = 'text';
    unit.autocomplete = 'off';
    unit.value = c.unit || '';
    unit.placeholder = info && info.unit ? info.unit : '';
    unit.addEventListener('input', () => {
      c.unit = unit.value;
      changed('inspector');
    });
    wrap.appendChild(field('Unit', unit));

    const width = document.createElement('input');
    width.type = 'number';
    width.min = '1';
    width.max = String(state.width);
    width.value = String(c.width || 1);
    width.addEventListener('change', () => {
      c.width = Math.max(1, Math.min(state.width, Math.round(Number(width.value) || 1)));
      width.value = String(c.width);
      changed();
    });
    wrap.appendChild(field('Width', width));

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
    wrap.appendChild(field('Align', align));
    return wrap;
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
        a.href = '/ui/plugins?' + own.toString();
      }
    }
    history.replaceState(null, '', '/ui/plugins?' + params.toString());
  }

  // widgetParams is the query of a widgets page link: the display type, the
  // widget and its size on the reference grid, and its args.
  function widgetParams(c) {
    const params = new URLSearchParams({ display: state.display });
    if (!c) return params;
    params.set('widget', c.widget);
    params.set('span', c.span[0] + 'x' + c.span[1]);
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
    if (rowsPage() ? !hasRows() : !state.template.cells.length) {
      clearBoardPreview();
      if (error) {
        error.hidden = false;
        error.textContent = rowsPage() ? 'Add a row to see a preview.' : 'Add a widget to see a preview.';
      }
      setRendering(false);
      return;
    }
    let res;
    try {
      res = await fetch('/api/preview/' + encodeURIComponent(state.display), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ template: serialize() }),
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
    const params = new URLSearchParams({ display: state.display, span: c.span[0] + 'x' + c.span[1] });
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
      const first = firstWidget(text() ? 'text' : 'image');
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
    const grid = kind === 'text' ? [info.width, info.height] : state.reference.slice();
    const template = kind === 'text'
      ? { kind: 'text', rows: [[{ value: firstValueKey(), width: Math.max(1, info.width), align: 'left' }]], style: {} }
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
    const params = new URLSearchParams({ name: state.name, display: display });
    if (doc.file) params.set('file', doc.file);
    location.href = '/ui/editor/page?' + params.toString();
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
    const params = new URLSearchParams({ name: name, display: state.display, file: label });
    location.href = '/ui/editor/page?' + params.toString();
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
    if (state.dirty && !confirm('Discard unsaved changes?')) return;
    const name = (prompt('New name for ' + state.name + ':', '') || '').trim();
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
    location.href = '/ui/editor/page?name=' + encodeURIComponent(name) + '&display=' + encodeURIComponent(state.display);
  }

  // remove deletes the one file that was opened, after naming the plans that use
  // the page; the server refuses one that changed since it was read (ADR 0073,
  // 0110).
  async function remove() {
    if (!state.hash) return;
    const used = state.usedBy || [];
    let warn = 'Delete ' + targetPath() + '?' +
      (used.length ? '\n\nPlans that name it: ' + used.join(', ') + '.' : '\n\nNo plan names this page.') +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '') +
      '\n\nA copy stays under state/editor/.';
    if (state.file && state.named) {
      warn += '\n\nThe page still names this file: its display type has no page until the file is back or the entry is removed.';
    }
    if (!confirm(warn)) return;
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
    location.href = '/ui/editor?tab=pages';
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
  on('board', 'dragover', (e) => e.preventDefault());
  on('board', 'drop', boardDrop);
  window.addEventListener('resize', () => applyBoardPreview());
  on('title', 'input', () => {
    if (!state.template) return;
    state.template.title = el('title').value;
    changed();
  });
  for (const id of ['cols', 'rows']) on(id, 'change', applyGrid);
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
