// Display type editor for Paperboy (ADR 0071). Vanilla JavaScript, no build
// step: the model comes from the display type API, and edits stay in the
// browser until Save writes displays/<name>.yaml with the hash it was read
// with.
(() => {
  'use strict';

  const el = (id) => document.getElementById(id);
  const start = JSON.parse(el('display-data').textContent);

  const state = {
    name: start.name,
    isNew: !!start.new,
    pages: start.pages || [], // page variants that use the type, for the delete warning
    devices: start.devices || [], // devices that use the type
    display: empty('image'),
    hash: '',
    yaml: '',
    readOnly: false,
    dirty: false,
    rename: start.rename || null,
  };

  function empty(kind) {
    return kind === 'text'
      ? { kind: 'text', size: [20, 4], gray_levels: 0, ppi: 0, charset: 'utf8', min_refresh: '', power: '', rotation: 0 }
      : { kind: 'image', size: [800, 480], gray_levels: 4, ppi: 125, charset: '', min_refresh: '', power: '', rotation: 0 };
  }

  // ---- status ----

  function setStatus(message, kind) {
    const s = el('status');
    s.textContent = message || '';
    s.className = 'status' + (kind ? ' ' + kind : '');
  }

  function updateWhere() {
    el('where').textContent =
      'displays/' + state.name + '.yaml' + (state.dirty ? ' · unsaved changes' : '');
  }

  function changed() {
    state.dirty = true;
    setStatus('unsaved changes', '');
    updateWhere();
  }

  // ---- form ----

  function buildSelects() {
    const kind = el('kind');
    for (const [value, label] of [['image', 'image (ePaper, pixels)'], ['text', 'text (character display)']]) {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = label;
      kind.appendChild(o);
    }

    const gray = el('gray');
    for (const value of ['1', '4']) {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = value === '4' ? '4 (four gray levels)' : '1 (black and white)';
      gray.appendChild(o);
    }

    const charset = el('charset');
    for (const value of ['utf8', 'ascii', 'hd44780']) {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = value;
      charset.appendChild(o);
    }

    const power = el('power');
    for (const [value, label] of [['', 'default (auto)'], ['auto', 'auto'], ['battery', 'battery'], ['mains', 'mains']]) {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = label;
      power.appendChild(o);
    }

    const rotation = el('rotation');
    for (const [value, label] of [
      ['0', '0° (as the firmware is built)'],
      ['90', '90° clockwise'],
      ['180', '180°'],
      ['270', '270° clockwise'],
    ]) {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = label;
      rotation.appendChild(o);
    }
  }

  // show fills the form from the model; it is called once per load or save.
  function show() {
    const d = state.display;
    el('kind').value = d.kind || 'image';
    el('width').value = d.size ? d.size[0] : '';
    el('height').value = d.size ? d.size[1] : '';
    el('gray').value = String(d.gray_levels === 4 ? 4 : 1);
    el('ppi').value = d.ppi || '';
    el('charset').value = d.charset || 'utf8';
    el('min_refresh').value = d.min_refresh || '';
    el('power').value = d.power || '';
    el('rotation').value = String(d.rotation || 0);
    el('yaml').textContent = state.yaml || '(no file yet)';
    el('delete').hidden = state.isNew;
    el('rename').hidden = state.isNew || !state.rename;
    updateKind();
    updateWhere();
  }

  // updateKind shows only the fields of the chosen kind and explains the size.
  function updateKind() {
    const text = state.display.kind === 'text';
    for (const node of document.querySelectorAll('.image-only')) node.hidden = text;
    for (const node of document.querySelectorAll('.text-only')) node.hidden = !text;
    el('width-label').firstChild.textContent = text ? 'columns ' : 'width, px ';
    el('height-label').firstChild.textContent = text ? 'rows ' : 'height, px ';
    el('width').max = text ? 80 : 4096;
    el('height').max = text ? 24 : 4096;
    el('size-note').textContent = text
      ? 'A text display grid of at most 80 columns × 24 rows.'
      : 'The page in pixels, as Paperboy composes it; rotation maps it onto the panel.';
    updatePPI();
  }

  // updatePPI names the physical size behind the pixel density, because ppi
  // silently changes the typography of every page (ADR 0050). The diagonal is
  // what a datasheet usually gives, so it comes along.
  function updatePPI() {
    const w = Number(state.display.size[0]);
    const h = Number(state.display.size[1]);
    const ppi = Number(state.display.ppi);
    el('ppi-note').textContent =
      ppi >= 20 && ppi <= 1000 && w > 0 && h > 0
        ? 'Physical size: ' + (w / ppi * 25.4).toFixed(1) + ' × ' + (h / ppi * 25.4).toFixed(1) + ' mm, diagonal ' +
          (Math.hypot(w, h) / ppi).toFixed(2) + ' in. ppi = pixels × 25.4 / mm.'
        : 'Pixels per inch of the active area, 20–1000: ppi = pixels × 25.4 / mm.';
    updatePanelHint();
  }

  // ---- panel size → ppi ----
  //
  // A datasheet gives the diagonal in inches or one side of the active area in
  // millimetres; the pixel size is already in the form. The helper only
  // converts, and shows every computed value, so a rounded inch figure cannot
  // silently win over the exact millimetre side.

  function round1(v) {
    return Math.round(v * 10) / 10;
  }

  function ppiFromDiagonal(inches) {
    const w = Number(state.display.size[0]);
    const h = Number(state.display.size[1]);
    return w > 0 && h > 0 && inches > 0 ? Math.hypot(w, h) / inches : 0;
  }

  function ppiFromMM(px, mm) {
    return px > 0 && mm > 0 ? px * 25.4 / mm : 0;
  }

  function updatePanelHint() {
    const w = Number(state.display.size[0]);
    const h = Number(state.display.size[1]);
    const diagonal = Number(el('panel-diagonal').value);
    const width = Number(el('panel-width').value);
    const height = Number(el('panel-height').value);
    const values = [];
    const parts = [];
    const add = (label, value) => {
      if (!(value > 0)) return;
      values.push(value);
      parts.push(label + ' → ' + round1(value) + ' ppi');
    };
    if (w > 0 && h > 0) add('diagonal ' + diagonal + ' in', ppiFromDiagonal(diagonal));
    add('width ' + width + ' mm', ppiFromMM(w, width));
    add('height ' + height + ' mm', ppiFromMM(h, height));
    let hint = parts.join(' · ');
    if (values.length > 1) {
      const min = Math.min(...values);
      const max = Math.max(...values);
      if (max / min > 1.02) {
        hint += ' — they differ by ' + Math.round((max / min - 1) * 100) +
          ' %: an inch diagonal is usually rounded, prefer the millimetre side.';
      }
    }
    el('panel-hint').textContent = hint;
  }

  // applyPPI takes over a value the helper computed. It stays a form value
  // until Save writes the file.
  function applyPPI(ppi, source) {
    if (state.readOnly) return;
    if (!(ppi > 0)) {
      setStatus('enter ' + source + ' first', 'error');
      return;
    }
    if (ppi < 20 || ppi > 1000) {
      setStatus('that is ' + round1(ppi) + ' ppi, outside 20–1000 — check the panel size', 'error');
      return;
    }
    state.display.ppi = round1(ppi);
    el('ppi').value = state.display.ppi;
    updatePPI();
    changed();
    setStatus('ppi ' + state.display.ppi + ' from ' + source, '');
  }

  // serialize builds the body of PUT /api/displays: only what is set, so the
  // file stays close to what the operator wrote.
  function serialize() {
    const d = state.display;
    const out = {
      kind: d.kind,
      size: [Number(d.size[0]) || 0, Number(d.size[1]) || 0],
    };
    if (d.kind === 'text') {
      out.charset = d.charset || 'utf8';
    } else {
      if (d.gray_levels) out.gray_levels = Number(d.gray_levels);
      if (d.ppi) out.ppi = Number(d.ppi);
      if (d.rotation) out.rotation = Number(d.rotation);
    }
    if (d.min_refresh) out.min_refresh = d.min_refresh;
    if (d.power) out.power = d.power;
    return out;
  }

  // normalize keeps the model in the shape serialize expects.
  function normalize(doc) {
    const d = Object.assign(empty('image'), doc || {});
    d.kind = d.kind === 'text' ? 'text' : 'image';
    d.size = d.size && d.size.length === 2 ? d.size : [0, 0];
    d.gray_levels = d.gray_levels === 4 ? 4 : 1;
    d.ppi = Number(d.ppi) || 0;
    d.charset = d.charset || '';
    d.min_refresh = d.min_refresh || '';
    d.power = d.power || '';
    d.rotation = Number(d.rotation) || 0;
    return d;
  }

  // ---- load and save ----

  async function load() {
    setStatus('');
    let res;
    try {
      res = await fetch('/api/displays/' + encodeURIComponent(state.name), { cache: 'no-store' });
    } catch (e) {
      setStatus('reading the display type failed: ' + e, 'error');
      return;
    }
    if (res.status === 404 && state.isNew) {
      state.display = empty('image');
      state.hash = '';
      state.yaml = '';
      state.dirty = false;
      state.readOnly = false;
      show();
      return;
    }
    if (!res.ok) {
      setStatus(res.status === 404 ? 'the display type is gone' : 'reading the display type failed (' + res.status + ')', 'error');
      state.readOnly = true;
      show();
      return;
    }
    const doc = await res.json();
    state.hash = doc.hash || '';
    state.yaml = doc.yaml || '';
    state.dirty = false;
    state.isNew = false;
    if (doc.error) {
      state.display = empty('image');
      state.readOnly = true;
      setStatus('this file does not read as a display type: ' + doc.error, 'error');
    } else {
      state.display = normalize(doc.display);
      state.readOnly = false;
    }
    show();
  }

  async function save() {
    if (state.readOnly) return;
    const bad = check();
    if (bad) {
      setStatus(bad, 'error');
      return;
    }
    setStatus('saving…', '');
    let res;
    try {
      res = await fetch('/api/displays/' + encodeURIComponent(state.name), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hash: state.hash, display: serialize() }),
      });
    } catch (e) {
      setStatus('save failed: ' + e, 'error');
      return;
    }
    const body = await res.text();
    let doc = {};
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
    state.yaml = doc.yaml || '';
    state.dirty = false;
    state.isNew = false;
    if (doc.display) state.display = normalize(doc.display);
    setStatus('saved ' + new Date().toLocaleTimeString(), 'ok');
    show();
  }

  // rename moves only the display type file; the page variants and devices that
  // use the name keep it, and the confirmation names them (ADR 0079).
  async function rename() {
    if (state.isNew || !state.rename) return;
    const warn = state.rename.warn +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '') +
      '\n\nThe new name is yours to propagate: nothing is rewritten.';
    if (!confirm(warn)) return;
    const name = (prompt('New name for ' + state.name + ': (lower case letters, digits and -)', '') || '').trim();
    if (!name || name === state.name) return;
    setStatus('renaming…', '');
    let res;
    try {
      res = await fetch(state.rename.url + '?hash=' + encodeURIComponent(state.hash), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
    } catch (e) {
      setStatus('rename failed: ' + e, 'error');
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
      setStatus(doc.error || 'rename failed (' + res.status + ')', 'error');
      return;
    }
    state.dirty = false;
    location.href = '/ui/editor/display?name=' + encodeURIComponent(name);
  }

  // remove deletes the file after naming the pages and devices that reference
  // the type; the server refuses one that changed since it was read (ADR 0073).
  async function remove() {
    if (state.isNew) return;
    const refs = [];
    if (state.pages.length) refs.push('page variants: ' + state.pages.join(', '));
    if (state.devices.length) refs.push('devices: ' + state.devices.join(', '));
    const warn = 'Delete displays/' + state.name + '.yaml?' +
      (refs.length ? '\n\nAlso uses it → ' + refs.join('; ') + '.' : '\n\nNo page variant or device uses this display type.') +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '') +
      '\n\nA copy stays under state/editor/.';
    if (!confirm(warn)) return;
    setStatus('deleting…', '');
    let res;
    try {
      res = await fetch('/api/displays/' + encodeURIComponent(state.name) + '?hash=' + encodeURIComponent(state.hash), { method: 'DELETE' });
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
    location.href = '/ui/editor?tab=displays';
  }

  // check catches what the API would reject anyway, so the operator sees it
  // without a round trip.
  function check() {
    const d = state.display;
    const w = Number(d.size[0]);
    const h = Number(d.size[1]);
    if (!(w > 0 && h > 0)) return 'the size needs positive numbers';
    if (d.kind === 'text') {
      if (w > 80 || h > 24) return 'a text display is at most 80 × 24 characters';
    } else {
      if (w > 4096 || h > 4096) return 'the size is too large';
      const ppi = Number(d.ppi);
      if (!(ppi >= 20 && ppi <= 1000)) return 'an image display needs a pixel density between 20 and 1000 ppi';
    }
    if (d.min_refresh && !/^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))*$/.test(d.min_refresh)) {
      return 'min_refresh is written like 60s or 1m' + (d.min_refresh ? ' (got ' + d.min_refresh + ')' : '');
    }
    return '';
  }

  // ---- wiring ----

  function onSize() {
    const set = () => {
      if (state.readOnly) return;
      state.display.size = [Number(el('width').value) || 0, Number(el('height').value) || 0];
      updatePPI();
      changed();
    };
    el('width').addEventListener('input', set);
    el('height').addEventListener('input', set);
  }

  function onPPI() {
    el('ppi').addEventListener('input', () => {
      if (state.readOnly) return;
      state.display.ppi = Number(el('ppi').value) || 0;
      updatePPI();
      changed();
    });
  }

  // onPanel wires the panel size helper: the fields only preview the value,
  // "use" puts it into the ppi field like a typed number.
  function onPanel() {
    for (const id of ['panel-diagonal', 'panel-width', 'panel-height']) {
      el(id).addEventListener('input', updatePanelHint);
    }
    el('use-diagonal').addEventListener('click', () => {
      applyPPI(ppiFromDiagonal(Number(el('panel-diagonal').value)), 'the diagonal');
    });
    el('use-width').addEventListener('click', () => {
      applyPPI(ppiFromMM(Number(state.display.size[0]), Number(el('panel-width').value)), 'the active width');
    });
    el('use-height').addEventListener('click', () => {
      applyPPI(ppiFromMM(Number(state.display.size[1]), Number(el('panel-height').value)), 'the active height');
    });
  }

  el('save').addEventListener('click', save);
  el('delete').addEventListener('click', remove);
  el('rename').addEventListener('click', rename);
  el('reload').addEventListener('click', () => location.reload());
  el('kind').addEventListener('change', () => {
    if (state.readOnly) return;
    const d = state.display;
    d.kind = el('kind').value === 'text' ? 'text' : 'image';
    // A size that cannot work for the new kind starts from that kind's default
    // instead of leaving an invalid form behind.
    if (d.kind === 'text' && (d.size[0] > 80 || d.size[1] > 24)) {
      d.size = [20, 4];
      if (!d.charset) d.charset = 'utf8';
    } else if (d.kind === 'image' && (d.size[0] < 20 || d.size[1] < 20)) {
      d.size = [800, 480];
      if (!(d.ppi >= 20)) d.ppi = 125;
    }
    show();
    changed();
  });
  el('gray').addEventListener('change', () => {
    if (state.readOnly) return;
    state.display.gray_levels = Number(el('gray').value) || 1;
    changed();
  });
  el('charset').addEventListener('change', () => {
    if (state.readOnly) return;
    state.display.charset = el('charset').value;
    changed();
  });
  el('min_refresh').addEventListener('change', () => {
    if (state.readOnly) return;
    state.display.min_refresh = el('min_refresh').value;
    changed();
  });
  el('power').addEventListener('change', () => {
    if (state.readOnly) return;
    state.display.power = el('power').value;
    changed();
  });
  el('rotation').addEventListener('change', () => {
    if (state.readOnly) return;
    state.display.rotation = Number(el('rotation').value) || 0;
    changed();
  });
  onSize();
  onPPI();
  onPanel();
  window.addEventListener('beforeunload', (e) => {
    if (!state.dirty) return;
    e.preventDefault();
    e.returnValue = '';
  });

  buildSelects();
  load();
})();
