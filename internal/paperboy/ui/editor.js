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
    display: start.display,
    kind: start.kind, // image or text
    width: start.width, // characters of a text display, pixels of an image display
    height: start.height,
    reference: start.reference, // grid plugins declare their sizes on
    grid: start.grid, // grid a new page starts with
    shared: !!start.shared, // a new page is written as pages/<name>.yaml
    fileDisplay: '', // display variant the file belongs to; empty: shared page
    hash: '',
    template: null,
    readOnly: false,
    selected: -1,
    placing: null, // {plugin, widget} waiting for a cell
    plugins: [],
    dirty: false,
    previewURL: '',
    previewTimer: 0,
  };

  const text = () => state.kind === 'text';
  const targetPath = () => 'pages/' + state.name + (state.fileDisplay ? '.' + state.fileDisplay : '') + '.yaml';

  // ---- status and where ----

  function setStatus(message, kind) {
    const s = el('status');
    s.textContent = message || '';
    s.className = 'status' + (kind ? ' ' + kind : '');
  }

  function updateWhere() {
    const parts = [targetPath()];
    parts.push(state.fileDisplay === '' ? 'shared by all display types' : 'only for ' + state.fileDisplay);
    if (state.dirty) parts.push('unsaved changes');
    el('where').textContent = parts.join(' · ');
  }

  function updateSave() {
    const canSave = !!state.template && !state.readOnly;
    el('save').disabled = !canSave;
    const specialize = el('specialize');
    specialize.hidden = !(canSave && state.fileDisplay === '');
    if (!specialize.hidden) specialize.textContent = 'Save as ' + state.display + ' variant';
  }

  // Rebuilding the board and the preview after a change, without touching the
  // inputs the operator is typing in.
  function changed() {
    state.dirty = true;
    setStatus('unsaved changes', '');
    updateWhere();
    updateSave();
    renderBoard();
    schedulePreview();
  }

  // ---- model ----

  function blank() {
    const grid = text() ? [state.width, state.height] : state.grid.slice();
    return { title: state.name, kind: state.kind, grid: grid, style: {}, cells: [] };
  }

  // normalize keeps the model in the shape serialize writes back: two-element
  // coordinates, args and styles present.
  function normalize(t) {
    t.kind = t.kind || state.kind;
    t.grid = Array.isArray(t.grid) && t.grid.length === 2 ? [t.grid[0], t.grid[1]] : state.grid.slice();
    t.style = t.style || {};
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
  // knows, with the defaults Page.Encode leaves out.
  function serialize() {
    const t = state.template;
    const out = { title: t.title || '', kind: t.kind, grid: [t.grid[0], t.grid[1]], style: styleOut(t.style || {}), cells: [] };
    if (t.notices === 'none') out.notices = 'none';
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

  function overlaps(x, y, w, h, skip) {
    return state.template.cells.some((c, i) => i !== skip &&
      x < c.at[0] + c.span[0] && c.at[0] < x + w &&
      y < c.at[1] + c.span[1] && c.at[1] < y + h);
  }

  // ---- page settings ----

  function renderSettings() {
    const t = state.template;
    const off = !t || state.readOnly;
    for (const id of ['title', 'cols', 'rows', 'padding', 'rule', 'text-size', 'divider', 'notices']) el(id).disabled = off;
    el('cols').disabled = off || text();
    el('rows').disabled = off || text();
    el('grid-note').textContent = text()
      ? 'The grid is the character grid of the display and cannot be changed.'
      : 'Cells of the page; plugins declare their sizes on a ' + state.reference[0] + '×' + state.reference[1] + ' reference grid.';
    if (!t) {
      for (const id of ['title', 'cols', 'rows', 'padding', 'rule', 'text-size', 'divider']) el(id).value = '';
      return;
    }
    const s = t.style || {};
    el('title').value = t.title || '';
    el('cols').value = t.grid[0];
    el('rows').value = t.grid[1];
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

  function renderPalette() {
    const box = el('palette');
    box.innerHTML = '';
    if (state.readOnly || !state.template) {
      box.innerHTML = '<p class="hint">The file does not read as a page; fix it by hand and reload.</p>';
      return;
    }
    if (!state.plugins.length) {
      box.innerHTML = '<p class="hint">No plugin answered.</p>';
      return;
    }
    const kind = text() ? 'text' : 'image';
    for (const p of state.plugins) {
      const section = document.createElement('div');
      section.className = 'plugin';
      const head = document.createElement('h4');
      head.textContent = p.name + (p.description && p.description.title ? ' · ' + p.description.title : '');
      section.appendChild(head);
      if (p.error) {
        const note = document.createElement('span');
        note.className = 'note';
        note.textContent = p.error;
        section.appendChild(note);
      }
      const widgets = (p.description && p.description.widgets) || [];
      const fitting = widgets.filter((w) => !w.kinds || !w.kinds.length || w.kinds.includes(kind));
      for (const w of fitting) {
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
  }

  function pick(plugin, widget) {
    if (!state.template) return;
    const same = state.placing && state.placing.plugin === plugin && state.placing.widget === widget;
    state.placing = same ? null : { plugin: plugin, widget: widget };
    renderPalette();
    renderBoard();
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

  // ---- board ----

  function renderBoard() {
    const board = el('board');
    board.innerHTML = '';
    board.className = state.placing ? 'placing' : '';
    const t = state.template;
    if (!t) {
      el('board-note').textContent = 'The file cannot be edited until it reads as a page again.';
      return;
    }
    board.style.gridTemplateColumns = 'repeat(' + t.grid[0] + ', 1fr)';
    board.style.gridTemplateRows = 'repeat(' + t.grid[1] + ', 1fr)';
    board.style.aspectRatio = state.width + ' / ' + state.height;
    for (let y = 0; y < t.grid[1]; y++) {
      for (let x = 0; x < t.grid[0]; x++) {
        const slot = document.createElement('div');
        slot.className = 'slot';
        slot.style.gridArea = (y + 1) + ' / ' + (x + 1);
        slot.addEventListener('click', () => {
          if (state.placing) place(state.placing.plugin, state.placing.widget, x, y);
          else select(-1);
        });
        slot.addEventListener('dragover', (e) => e.preventDefault());
        slot.addEventListener('drop', (e) => {
          e.preventDefault();
          const id = e.dataTransfer.getData('text/plain');
          const [plugin, widget] = id.split('/');
          if (plugin && widget) place(plugin, widget, x, y);
        });
        board.appendChild(slot);
      }
    }
    t.cells.forEach((c, i) => {
      const cell = document.createElement('div');
      cell.className = 'cell' + (i === state.selected ? ' selected' : '');
      cell.style.gridArea = (c.at[1] + 1) + ' / ' + (c.at[0] + 1) + ' / span ' + c.span[1] + ' / span ' + c.span[0];
      const name = document.createElement('span');
      name.className = 'w';
      name.textContent = c.widget;
      const pos = document.createElement('span');
      pos.className = 'pos';
      pos.textContent = c.at[0] + ',' + c.at[1] + ' · ' + c.span[0] + '×' + c.span[1];
      cell.append(name, pos);
      cell.addEventListener('click', (e) => {
        e.stopPropagation();
        select(i);
      });
      board.appendChild(cell);
    });
    let used = 0;
    for (const c of t.cells) used += c.span[0] * c.span[1];
    let note = t.grid[0] + '×' + t.grid[1] + ' cells, ' + used + ' of ' + t.grid[0] * t.grid[1] + ' covered';
    if (state.placing) note += ' · click a free cell to place ' + state.placing.plugin + '/' + state.placing.widget;
    el('board-note').textContent = note;
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

  function argValue(raw, arg) {
    const type = arg && arg.type;
    if (type === 'string' || type === 'duration') return raw;
    if (type === 'list') return raw.split(',').map((s) => s.trim()).filter((s) => s !== '').map(jsonOrString);
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
      if (raw === '' || raw === null) delete cell.args[name];
      else cell.args[name] = argValue(raw, arg);
      changed();
      return;
    }
    // A bool that differs from its declared default is written out, so an
    // unchecked box can still mean false for a default of true.
    const value = raw === true;
    const fallback = arg.default === true;
    if (value === fallback) delete cell.args[name];
    else cell.args[name] = value;
    changed();
  }

  function renderInspector() {
    const box = el('inspector');
    box.innerHTML = '';
    const head = document.createElement('h3');
    head.textContent = 'Cell';
    box.appendChild(head);
    const t = state.template;
    const c = t && state.selected >= 0 ? t.cells[state.selected] : null;
    if (!c) {
      const p = document.createElement('p');
      p.className = 'empty';
      p.textContent = 'Select a cell to change its size, its arguments and its style.';
      box.appendChild(p);
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
        changed();
      }
    };
    cols.addEventListener('change', applySpan);
    rows.addEventListener('change', applySpan);

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
          option.textContent = String(value);
          control.appendChild(option);
        }
        control.value = current === undefined ? '' : String(current);
        control.addEventListener('change', () => {
          if (control.value === '') setArg(c, name, '', arg);
          else {
            const chosen = arg.enum.find((v) => String(v) === control.value);
            c.args[name] = chosen === undefined ? control.value : chosen;
            changed();
          }
        });
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
        changed();
      });
      box.appendChild(field(key, input));
    }

    const buttons = document.createElement('div');
    buttons.className = 'row-buttons';
    const remove = document.createElement('button');
    remove.type = 'button';
    remove.textContent = 'Delete cell';
    remove.addEventListener('click', removeCell);
    buttons.appendChild(remove);
    box.appendChild(buttons);
    updateSave();
  }

  function removeCell() {
    if (!state.template || state.selected < 0) return;
    state.template.cells.splice(state.selected, 1);
    state.selected = -1;
    changed();
    renderInspector();
  }

  // ---- preview ----

  function schedulePreview() {
    clearTimeout(state.previewTimer);
    state.previewTimer = setTimeout(preview, 350);
  }

  async function preview() {
    const img = el('preview-img');
    const pre = el('preview-text');
    const error = el('preview-error');
    if (!state.template) {
      img.hidden = pre.hidden = error.hidden = true;
      return;
    }
    if (!state.template.cells.length) {
      img.hidden = pre.hidden = true;
      error.hidden = false;
      error.textContent = 'Add a widget to see a preview.';
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
      img.hidden = pre.hidden = true;
      error.hidden = false;
      error.textContent = 'preview failed: ' + e;
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
    const res = await fetch('/api/pages/' + encodeURIComponent(state.name) + '?display=' + encodeURIComponent(state.display), { cache: 'no-store' });
    if (res.status === 404) {
      state.template = blank();
      state.fileDisplay = state.shared ? '' : state.display;
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
    state.fileDisplay = doc.display || '';
    state.hash = doc.hash || '';
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

  async function save() {
    if (!state.template) return;
    setStatus('saving…', '');
    let res;
    try {
      res = await fetch('/api/pages/' + encodeURIComponent(state.name) + '?display=' + encodeURIComponent(state.fileDisplay), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hash: state.hash, template: serialize() }),
      });
    } catch (e) {
      setStatus('save failed: ' + e, 'error');
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
    state.hash = doc.hash || '';
    state.fileDisplay = doc.display || '';
    if (doc.template) state.template = normalize(doc.template);
    state.dirty = false;
    setStatus('saved ' + new Date().toLocaleTimeString(), 'ok');
    renderAll();
  }

  // A shared page can become the variant of this display type; the shared file
  // then stays as it is (ADR 0068).
  function specialize() {
    if (!state.template) return;
    state.fileDisplay = state.display;
    state.hash = '';
    setStatus('the next save writes the variant; the shared page stays unchanged', '');
    updateWhere();
    updateSave();
  }

  function renderAll() {
    renderSettings();
    renderPalette();
    renderBoard();
    renderInspector();
    updateWhere();
    updateSave();
    schedulePreview();
  }

  // ---- wiring ----

  el('save').addEventListener('click', save);
  el('reload').addEventListener('click', () => location.reload());
  el('specialize').addEventListener('click', specialize);
  el('title').addEventListener('input', () => {
    if (!state.template) return;
    state.template.title = el('title').value;
    changed();
  });
  for (const id of ['cols', 'rows']) el(id).addEventListener('change', applyGrid);
  for (const [id, key] of [['padding', 'padding'], ['rule', 'rule'], ['text-size', 'text_size'], ['divider', 'divider']]) {
    el(id).addEventListener('input', () => {
      if (!state.template) return;
      state.template.style = state.template.style || {};
      const v = num(el(id).value);
      if (v === null) delete state.template.style[key];
      else state.template.style[key] = v;
      changed();
    });
  }
  el('notices').addEventListener('change', () => {
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
