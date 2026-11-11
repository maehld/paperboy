// Device editor for Paperboy (ADR 0070). Vanilla JavaScript, no build step: the
// device model comes from the device API, and edits stay in the browser until
// Save writes devices/<id>.yaml with the hash it was read with.
(() => {
  'use strict';

  const el = (id) => document.getElementById(id);
  const start = JSON.parse(el('device-data').textContent);

  const state = {
    id: start.id,
    isNew: !!start.new,
    displays: start.displays || [],
    plans: start.plans || [],
    defaults: { refresh: start.refresh || '', menu_show: start.menu_show || '' },
    device: empty(),
    hash: '',
    yaml: '',
    readOnly: false,
    dirty: false,
    rename: start.rename || null,
  };

  function empty() {
    return {
      display_type: '',
      protocol: '',
      mac: '',
      plan: '',
      refresh: '',
      actions: '',
      menu_show_duration: '',
      power: '',
      firmware: '',
      battery: null,
    };
  }

  // ---- status ----

  function setStatus(message, kind) {
    const s = el('status');
    s.textContent = message || '';
    s.className = 'status' + (kind ? ' ' + kind : '');
  }

  function updateWhere() {
    el('where').textContent =
      'devices/' + state.id + '.yaml' + (state.dirty ? ' · unsaved changes' : '');
  }

  function updateSave() {
    el('save').disabled = state.readOnly || (state.isNew && !state.device.mac);
    el('delete').hidden = state.isNew;
    el('rename').hidden = state.isNew || !state.rename;
  }

  function changed() {
    state.dirty = true;
    setStatus('unsaved changes', '');
    updateWhere();
    updateSave();
  }

  // ---- form ----

  function option(select, value, label) {
    const o = document.createElement('option');
    o.value = value;
    o.textContent = label;
    select.appendChild(o);
    return o;
  }

  function buildSelects() {
    const actions = el('actions');
    option(actions, '', 'default (allow)');
    option(actions, 'allow', 'allow');
    option(actions, 'deny', 'deny');

    const power = el('power');
    option(power, '', "the display type's setting");
    option(power, 'auto', 'auto');
    option(power, 'battery', 'battery');
    option(power, 'mains', 'mains');

    const firmware = el('firmware');
    option(firmware, '', 'offer updates');
    option(firmware, 'hold', 'hold');

    el('refresh').placeholder = 'default ' + (state.defaults.refresh || '5m');
    el('refresh-note').textContent =
      'Default: ' + (state.defaults.refresh || '5m') + '; a plan rule may override it.';
    el('menu').placeholder = 'default ' + (state.defaults.menu_show || '30s');
  }

  function durations() {
    return [state.device.refresh, state.device.menu_show_duration];
  }

  // show fills the form from the model; it is called once per load or save.
  function show() {
    const d = state.device;
    fillDisplays(d.display_type);
    el('mac').value = d.mac || '';
    fillPlans(d.plan);
    el('refresh').value = d.refresh || '';
    el('actions').value = d.actions || '';
    el('menu').value = d.menu_show_duration || '';
    el('power').value = d.power || '';
    el('firmware').value = d.firmware || '';
    const b = d.battery || {};
    el('warn').value = b.warn_volts || '';
    el('critical').value = b.critical_volts || '';
    el('hysteresis').value = b.hysteresis_volts || '';
    el('yaml').textContent = state.yaml || '(no file yet)';
    renderLinks();
    updateWhere();
    updateSave();
  }

  // fillPlans rebuilds the plan list; a plan that is not a file any more stays
  // visible instead of silently vanishing from the form.
  function fillPlans(current) {
    const plan = el('plan');
    plan.innerHTML = '';
    option(plan, '', '— no plan: notice page —');
    for (const name of state.plans) option(plan, name, name);
    if (current && !state.plans.includes(current)) option(plan, current, current + ' (no such file)');
    plan.value = current || '';
  }

  // fillDisplays rebuilds the display type list; a display type that is not a
  // file any more stays visible instead of silently vanishing from the form, so
  // its disappearance is what the operator sees (ADR 0079).
  function fillDisplays(current) {
    const display = el('display');
    display.innerHTML = '';
    option(display, '', '— choose a display type —');
    for (const name of state.displays) option(display, name, name);
    if (current && !state.displays.includes(current)) option(display, current, current + ' (no such display type)');
    display.value = current || '';
  }

  function renderLinks() {
    const box = el('links');
    box.innerHTML = '';
    if (state.device.plan) {
      const a = document.createElement('a');
      a.href = '/ui/plan?device=' + encodeURIComponent(state.id);
      a.textContent = 'Plan preview of ' + state.id;
      box.appendChild(a);
    }
    const b = document.createElement('a');
    b.href = '/ui';
    b.textContent = 'Dashboard';
    box.appendChild(b);
  }

  // serialize builds the body of PUT /api/devices: only what is set, so the
  // file stays close to what the operator wrote.
  function serialize() {
    const d = state.device;
    const out = { display_type: d.display_type, mac: d.mac };
    for (const key of ['protocol', 'plan', 'actions', 'power', 'firmware']) {
      if (d[key]) out[key] = d[key];
    }
    if (d.refresh) out.refresh = d.refresh;
    if (d.menu_show_duration) out.menu_show_duration = d.menu_show_duration;
    const b = {};
    for (const key of ['warn_volts', 'critical_volts', 'hysteresis_volts']) {
      const v = Number(d.battery && d.battery[key]);
      if (v > 0) b[key] = v;
    }
    if (Object.keys(b).length) out.battery = b;
    return out;
  }

  // normalize keeps the model in the shape serialize expects.
  function normalize(doc) {
    const d = Object.assign(empty(), doc || {});
    d.battery =
      d.battery && (d.battery.warn_volts || d.battery.critical_volts || d.battery.hysteresis_volts)
        ? {
            warn_volts: d.battery.warn_volts || 0,
            critical_volts: d.battery.critical_volts || 0,
            hysteresis_volts: d.battery.hysteresis_volts || 0,
          }
        : null;
    return d;
  }

  // ---- load and save ----

  async function load() {
    setStatus('');
    let res;
    try {
      res = await fetch('/api/devices/' + encodeURIComponent(state.id), { cache: 'no-store' });
    } catch (e) {
      setStatus('reading the device failed: ' + e, 'error');
      return;
    }
    if (res.status === 404 && state.isNew) {
      state.device = empty();
      state.hash = '';
      state.yaml = '';
      state.dirty = false;
      state.readOnly = false;
      show();
      return;
    }
    if (!res.ok) {
      setStatus(res.status === 404 ? 'the device is gone' : 'reading the device failed (' + res.status + ')', 'error');
      state.readOnly = true;
      state.device = empty();
      show();
      return;
    }
    const doc = await res.json();
    state.hash = doc.hash || '';
    state.yaml = doc.yaml || '';
    state.dirty = false;
    state.isNew = false;
    if (doc.error) {
      state.device = empty();
      state.readOnly = true;
      setStatus('this file does not read as a device: ' + doc.error, 'error');
    } else {
      state.device = normalize(doc.device);
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
      res = await fetch('/api/devices/' + encodeURIComponent(state.id), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hash: state.hash, device: serialize() }),
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
    if (doc.device) state.device = normalize(doc.device);
    setStatus('saved ' + new Date().toLocaleTimeString(), 'ok');
    show();
  }

  // rename moves the device file and its state, so the cursor, battery reading
  // and menu survive; the firmware registers by MAC and does not notice
  // (ADR 0079).
  async function rename() {
    if (state.isNew || !state.rename) return;
    const warn = state.rename.warn +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '');
    if (!confirm(warn)) return;
    const id = (prompt('New ID for ' + state.id + ': (lower case letters, digits and -)', '') || '').trim();
    if (!id || id === state.id) return;
    setStatus('renaming…', '');
    let res;
    try {
      res = await fetch(state.rename.url + '?hash=' + encodeURIComponent(state.hash), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: id }),
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
    location.href = '/ui/editor/device?id=' + encodeURIComponent(id);
  }

  // remove deletes the file; the server refuses one that changed since it was
  // read (ADR 0073).
  async function remove() {
    if (state.isNew) return;
    const warn = 'Delete devices/' + state.id + '.yaml?' +
      '\n\nThe device cannot sync until a device file with ' + (state.device.mac || 'its MAC') + ' exists again.' +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '') +
      '\n\nA copy stays under state/editor/.';
    if (!confirm(warn)) return;
    setStatus('deleting…', '');
    let res;
    try {
      res = await fetch('/api/devices/' + encodeURIComponent(state.id) + '?hash=' + encodeURIComponent(state.hash), { method: 'DELETE' });
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
    location.href = '/ui/editor?tab=devices';
  }

  // check catches what the API would reject anyway, so the operator sees it
  // without a round trip.
  function check() {
    const d = state.device;
    if (!d.display_type) return 'choose a display type';
    if (!/^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$/.test(d.mac || '')) return 'the MAC address must look like AA:BB:CC:DD:EE:01';
    if (!state.displays.includes(d.display_type)) return 'display type ' + d.display_type + ' is not configured';
    for (const s of durations()) {
      if (s && !/^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))*$/.test(s)) {
        return 'durations are written like 5m, 30s or 1h' + (s ? ' (got ' + s + ')' : '');
      }
    }
    return '';
  }

  // ---- wiring ----

  function onField(id, key) {
    el(id).addEventListener('change', () => {
      if (state.readOnly) return;
      state.device[key] = el(id).value;
      changed();
      if (key === 'plan') renderLinks();
    });
  }

  function onBattery(id, key) {
    el(id).addEventListener('change', () => {
      if (state.readOnly) return;
      state.device.battery = state.device.battery || { warn_volts: 0, critical_volts: 0, hysteresis_volts: 0 };
      state.device.battery[key] = Number(el(id).value) || 0;
      changed();
    });
  }

  el('save').addEventListener('click', save);
  el('delete').addEventListener('click', remove);
  el('rename').addEventListener('click', rename);
  el('reload').addEventListener('click', () => location.reload());
  onField('display', 'display_type');
  onField('mac', 'mac');
  onField('plan', 'plan');
  onField('refresh', 'refresh');
  onField('actions', 'actions');
  onField('menu', 'menu_show_duration');
  onField('power', 'power');
  onField('firmware', 'firmware');
  onBattery('warn', 'warn_volts');
  onBattery('critical', 'critical_volts');
  onBattery('hysteresis', 'hysteresis_volts');
  window.addEventListener('beforeunload', (e) => {
    if (!state.dirty) return;
    e.preventDefault();
    e.returnValue = '';
  });

  buildSelects();
  load();
})();
