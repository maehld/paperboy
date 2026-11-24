// Plan editor for Paperboy (ADR 0069). Vanilla JavaScript, no build step: the
// plan model comes from the plan API (ADR 0066), the plugin values from the
// plugin descriptions, and edits stay in the browser until Save writes the
// file with the hash it was read with.
(() => {
  'use strict';

  const el = (id) => document.getElementById(id);
  const start = JSON.parse(el('plan-data').textContent);
  const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];

  const state = {
    name: start.name,
    devices: start.devices || [],
    displays: start.displays || [], // display types of the devices that use this plan
    pages: start.pages || [], // page files of the instance
    plugins: [],
    plan: null,
    hash: '',
    isNew: !!start.new, // no file yet: the first save writes it (ADR 0073)
    rename: start.rename || null, // endpoint and confirmation for a rename (ADR 0079)
    readOnly: false,
    dirty: false,
  };

  let triggerNodes = [];

  // ---- status ----

  function setStatus(message, kind) {
    const s = el('status');
    s.textContent = message || '';
    s.className = 'status' + (kind ? ' ' + kind : '');
  }

  function updateWhere() {
    el('where').textContent = 'plans/' + state.name + '.yaml' +
      (state.isNew ? ' · not saved yet' : '') +
      (state.dirty ? ' · unsaved changes' : '');
  }

  function updateSave() {
    el('save').disabled = !state.plan || state.readOnly;
    el('delete').hidden = state.isNew || !state.plan;
    el('rename').hidden = state.isNew || !state.plan || !state.rename;
  }

  function changed() {
    state.dirty = true;
    setStatus('unsaved changes', '');
    updateWhere();
    updateSave();
    renderWarnings();
  }

  // ---- model ----

  function pageInfo(name) {
    return state.pages.find((p) => p.name === name) || null;
  }

  function planPages() {
    return state.plan ? state.plan.pages.map((p) => p.page) : [];
  }

  function asList(v) {
    if (Array.isArray(v)) return v;
    if (v === undefined || v === null || v === '') return [];
    return [v];
  }

  // normalizeCondition keeps the fields of all three forms. The value names
  // what the condition tests; the others stay so that switching back keeps
  // them.
  function normalizeCondition(c) {
    const cond = c && typeof c === 'object' ? c : {};
    return {
      value: cond.value || '',
      is: asList(cond.is).map(String),
      from: cond.from || '',
      to: cond.to || '',
    };
  }

  function normalize(p) {
    p.pages = (p.pages || []).map((entry) => {
      if (typeof entry === 'string') return { page: entry, hidden: false };
      return { page: (entry && entry.page) || '', hidden: !!(entry && entry.hidden) };
    });
    p.on_refresh = p.on_refresh || 'current';
    const refresh = p.refresh;
    if (!refresh) p.refresh = { default: '', windows: [] };
    else if (typeof refresh === 'string') p.refresh = { default: refresh, windows: [] };
    else p.refresh = {
      default: refresh.default || '',
      windows: (refresh.windows || []).map((w) => ({ from: w.from || '', to: w.to || '', interval: w.interval || '' })),
    };
    p.triggers = (p.triggers || []).map((t) => {
      const when = Array.isArray(t.when) ? t.when : t.when ? [t.when] : [];
      return {
        when: when.map(normalizeCondition),
        page: t.page || '',
        alert: t.alert || '',
      };
    });
    return p;
  }

  // serializeCondition sends the value and the fields of its form.
  function serializeCondition(c) {
    if (c.value === 'time' || c.value === 'date') {
      const out = { value: c.value };
      if (c.from) out.from = c.from;
      if (c.to) out.to = c.to;
      return out;
    }
    return { value: c.value, is: c.is.slice() };
  }

  // serialize builds the body of PUT /api/plans: only what the API knows, with
  // empty windows, durations and optional keys left out.
  function serialize() {
    const p = state.plan;
    const out = { pages: p.pages.map((e) => (e.hidden ? { page: e.page, hidden: true } : e.page)) };
    if (p.on_refresh && p.on_refresh !== 'current') out.on_refresh = p.on_refresh;
    const windows = p.refresh.windows
      .filter((w) => w.from && w.to && w.interval)
      .map((w) => ({ from: w.from, to: w.to, interval: w.interval }));
    if (windows.length) {
      const refresh = { windows };
      if (p.refresh.default) refresh.default = p.refresh.default;
      out.refresh = refresh;
    } else if (p.refresh.default) {
      out.refresh = p.refresh.default;
    }
    if (p.triggers.length) {
      out.triggers = p.triggers.map((t) => {
        const trigger = { when: t.when.map(serializeCondition), page: t.page };
        if (t.alert) trigger.alert = t.alert;
        return trigger;
      });
    }
    return out;
  }

  // ---- small builders ----

  function field(label, control) {
    const l = document.createElement('label');
    l.className = 'field';
    const span = document.createElement('span');
    span.textContent = label;
    l.append(span, control);
    return l;
  }

  function textInput(value, placeholder) {
    const input = document.createElement('input');
    input.type = 'text';
    input.autocomplete = 'off';
    input.value = value || '';
    if (placeholder) input.placeholder = placeholder;
    return input;
  }

  function timeInput(value) {
    const input = document.createElement('input');
    input.type = 'time';
    input.value = value || '';
    return input;
  }

  function dateInput(value) {
    const input = document.createElement('input');
    input.type = 'date';
    input.value = value || '';
    return input;
  }

  function small(label, action, title) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'small';
    b.textContent = label;
    if (title) b.title = title;
    if (action) b.addEventListener('click', action);
    else b.disabled = true;
    return b;
  }

  function renderMessages(box, messages) {
    box.innerHTML = '';
    const unique = [...new Set(messages)];
    if (!unique.length) return;
    const warn = document.createElement('div');
    warn.className = 'warn';
    if (unique.length === 1) {
      warn.textContent = unique[0];
    } else {
      const ul = document.createElement('ul');
      for (const m of unique) {
        const li = document.createElement('li');
        li.textContent = m;
        ul.appendChild(li);
      }
      warn.appendChild(ul);
    }
    box.appendChild(warn);
  }

  // ---- pages ----

  function renderPages() {
    const box = el('pages');
    box.innerHTML = '';
    if (state.readOnly || !state.plan) {
      box.innerHTML = '<p class="hint">The file does not read as a plan; fix it by hand and reload.</p>';
      renderAddPage();
      return;
    }
    if (!state.plan.pages.length) {
      box.innerHTML = '<p class="hint">No pages yet; a plan needs at least one visible page.</p>';
    }
    state.plan.pages.forEach((entry, i) => box.appendChild(pageRow(entry, i)));
    renderAddPage();
    renderWarnings();
  }

  function pageRow(entry, i) {
    const row = document.createElement('div');
    row.className = 'page-row' + (entry.hidden ? ' hidden' : '');
    const info = pageInfo(entry.page);
    if (entry.page && !info) row.classList.add('missing');
    const labels = document.createElement('span');
    labels.className = 'labels';
    const name = document.createElement('span');
    name.className = 'name';
    name.textContent = entry.page || '(no page)';
    labels.appendChild(name);
    if (entry.hidden) {
      const flag = document.createElement('span');
      flag.className = 'flag';
      flag.textContent = 'hidden';
      labels.appendChild(flag);
    }
    const meta = document.createElement('span');
    meta.className = 'muted';
    meta.textContent = info
      ? info.error || (info.serves && info.serves.length ? info.serves.join(' · ') : 'no display type')
      : 'no page file';
    labels.appendChild(meta);
    row.append(
      labels,
      small(entry.hidden ? 'Show' : 'Hide', () => {
        entry.hidden = !entry.hidden;
        changed();
        renderPages();
      }, entry.hidden ? 'Put the page back into the rotation' : 'Take the page out of the rotation; only a trigger shows it'),
      small('↑', i > 0 ? () => movePage(i, -1) : null),
      small('↓', i < state.plan.pages.length - 1 ? () => movePage(i, 1) : null),
      small('×', () => {
        state.plan.pages.splice(i, 1);
        changed();
        renderPages();
        renderTriggers();
      }, 'Remove the page from the plan'),
    );
    return row;
  }

  function movePage(i, step) {
    const to = i + step;
    if (to < 0 || to >= state.plan.pages.length) return;
    const [entry] = state.plan.pages.splice(i, 1);
    state.plan.pages.splice(to, 0, entry);
    changed();
    renderPages();
  }

  // renderAddPage offers the page files the plan does not name yet.
  function renderAddPage() {
    const select = el('add-page-select');
    select.innerHTML = '';
    const free = state.pages.filter((p) => !planPages().includes(p.name));
    const none = document.createElement('option');
    none.value = '';
    none.textContent = free.length ? 'add a page…' : 'no other page files';
    select.appendChild(none);
    for (const p of free) {
      const option = document.createElement('option');
      option.value = p.name;
      option.textContent = p.name;
      select.appendChild(option);
    }
    el('add-page').disabled = !free.length || !state.plan;
  }

  if (el('add-page')) {
    el('add-page').addEventListener('click', () => {
      const name = el('add-page-select').value;
      if (!name || !state.plan) return;
      state.plan.pages.push({ page: name, hidden: false });
      changed();
      renderPages();
      renderTriggers();
    });
  }

  // ---- refresh ----

  function renderRefresh() {
    if (state.readOnly || !state.plan) return;
    el('refresh-default').value = state.plan.refresh.default;
    el('on-refresh').value = state.plan.on_refresh;
    const box = el('windows');
    box.innerHTML = '';
    if (!state.plan.refresh.windows.length) {
      box.innerHTML = '<p class="hint">No window; the default applies all day.</p>';
    }
    state.plan.refresh.windows.forEach((w, i) => box.appendChild(windowRow(w, i)));
    renderWarnings();
  }

  function windowRow(w, i) {
    const row = document.createElement('div');
    row.className = 'entry';
    const head = document.createElement('div');
    head.className = 'entry-head';
    const num = document.createElement('span');
    num.className = 'num';
    num.textContent = '#' + (i + 1);
    const buttons = document.createElement('div');
    buttons.className = 'buttons';
    buttons.append(
      small('↑', i > 0 ? () => moveWindow(i, -1) : null),
      small('↓', i < state.plan.refresh.windows.length - 1 ? () => moveWindow(i, 1) : null),
      small('×', () => {
        state.plan.refresh.windows.splice(i, 1);
        changed();
        renderRefresh();
      }),
    );
    head.append(num, buttons);
    row.appendChild(head);

    const grid = document.createElement('div');
    grid.className = 'entry-grid';
    const from = timeInput(w.from);
    from.addEventListener('change', () => {
      w.from = from.value;
      changed();
    });
    grid.appendChild(field('From', from));
    const to = timeInput(w.to);
    to.addEventListener('change', () => {
      w.to = to.value;
      changed();
    });
    grid.appendChild(field('To', to));
    const interval = textInput(w.interval, 'e.g. 10m');
    interval.addEventListener('input', () => {
      w.interval = interval.value.trim();
      changed();
    });
    grid.appendChild(field('Interval', interval));
    row.appendChild(grid);
    return row;
  }

  function moveWindow(i, step) {
    const to = i + step;
    if (to < 0 || to >= state.plan.refresh.windows.length) return;
    const [w] = state.plan.refresh.windows.splice(i, 1);
    state.plan.refresh.windows.splice(to, 0, w);
    changed();
    renderRefresh();
  }

  if (el('add-window')) {
    el('add-window').addEventListener('click', () => {
      if (!state.plan) return;
      state.plan.refresh.windows.push({ from: '08:00', to: '16:00', interval: '10m' });
      changed();
      renderRefresh();
    });
    el('refresh-default').addEventListener('input', () => {
      if (!state.plan) return;
      state.plan.refresh.default = el('refresh-default').value.trim();
      changed();
    });
    el('on-refresh').addEventListener('change', () => {
      if (!state.plan) return;
      state.plan.on_refresh = el('on-refresh').value;
      changed();
    });
  }

  // ---- triggers ----

  function renderTriggers() {
    const box = el('triggers');
    box.innerHTML = '';
    triggerNodes = [];
    if (state.readOnly || !state.plan) {
      box.innerHTML = '<p class="hint">The file does not read as a plan; fix it by hand and reload.</p>';
      return;
    }
    if (!state.plan.triggers.length) {
      box.innerHTML = '<p class="hint">No triggers; the device only cycles through its pages.</p>';
    }
    state.plan.triggers.forEach((trigger, i) => box.appendChild(triggerCard(trigger, i)));
    renderWarnings();
  }

  function triggerCard(trigger, i) {
    const card = document.createElement('section');
    card.className = 'entry';

    const head = document.createElement('div');
    head.className = 'entry-head';
    const num = document.createElement('span');
    num.className = 'num';
    num.textContent = '#' + (i + 1);
    const buttons = document.createElement('div');
    buttons.className = 'buttons';
    buttons.append(
      small('↑', i > 0 ? () => moveTrigger(i, -1) : null),
      small('↓', i < state.plan.triggers.length - 1 ? () => moveTrigger(i, 1) : null),
      small('×', () => {
        state.plan.triggers.splice(i, 1);
        changed();
        renderTriggers();
      }),
    );
    head.append(num, buttons);
    card.appendChild(head);

    const grid = document.createElement('div');
    grid.className = 'entry-grid trigger-grid';

    // The target page must be one of the plan's pages; a hidden page is fine.
    const page = document.createElement('select');
    const none = document.createElement('option');
    none.value = '';
    none.textContent = 'choose a page…';
    page.appendChild(none);
    for (const entry of state.plan.pages) {
      const option = document.createElement('option');
      option.value = entry.page;
      option.textContent = entry.page + (entry.hidden ? ' (hidden)' : '');
      page.appendChild(option);
    }
    if (trigger.page && !planPages().includes(trigger.page)) {
      const option = document.createElement('option');
      option.value = trigger.page;
      option.textContent = trigger.page + ' (not in pages)';
      page.appendChild(option);
    }
    page.value = trigger.page;
    page.addEventListener('change', () => {
      trigger.page = page.value;
      changed();
    });
    grid.appendChild(field('Page', page));

    const alertSelect = document.createElement('select');
    for (const [value, label] of [['', 'never'], ['once', 'once'], ['refresh', 'on refresh']]) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = label;
      alertSelect.appendChild(option);
    }
    alertSelect.value = trigger.alert;
    alertSelect.addEventListener('change', () => {
      trigger.alert = alertSelect.value;
      changed();
    });
    grid.appendChild(field('Alert', alertSelect));

    // Conditions are value keys, e.g. dexcom/state is low (ADR 0127). Every
    // entry must match; the keys of one entry are alternatives. A value or key
    // a configured plugin does not declare stays visible so that a plan written
    // by hand is not silently broken.
    const conds = document.createElement('div');
    conds.className = 'conditions';
    if (!trigger.when.length) {
      const note = document.createElement('span');
      note.className = 'muted';
      note.textContent = allValues().length ? 'no condition' : 'no plugin declares an enum value';
      conds.appendChild(note);
    }
    trigger.when.forEach((cond) => conds.appendChild(conditionRow(trigger, cond)));
    const addCond = document.createElement('button');
    addCond.type = 'button';
    addCond.textContent = 'Add condition';
    addCond.addEventListener('click', () => {
      const first = allValues()[0];
      trigger.when.push({ value: first ? first.value : '', is: [] });
      changed();
      renderTriggers();
    });
    conds.appendChild(addCond);
    card.appendChild(grid);

    const condField = field('Conditions', conds);
    condField.classList.add('wide');
    card.appendChild(condField);

    const warnings = document.createElement('div');
    card.appendChild(warnings);
    triggerNodes[i] = { card, warnings };
    return card;
  }

  function moveTrigger(i, step) {
    const to = i + step;
    if (to < 0 || to >= state.plan.triggers.length) return;
    const [trigger] = state.plan.triggers.splice(i, 1);
    state.plan.triggers.splice(to, 0, trigger);
    changed();
    renderTriggers();
  }

  function addTrigger() {
    if (!state.plan) return;
    const first = allValues()[0];
    const page = state.plan.pages.find((p) => p.hidden) || state.plan.pages[0];
    state.plan.triggers.push({
      when: first ? [{ value: first.value, is: [], from: '', to: '' }] : [],
      page: page ? page.page : '',
      alert: '',
    });
    changed();
    renderTriggers();
  }

  // ---- warnings ----

  function looksDuration(s) {
    return /^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))*$/.test(s);
  }

  // minuteSet expands a window to the minutes it covers, so overlaps are
  // visible on a day that wraps at midnight.
  function minuteSet(w) {
    const out = new Set();
    if (!w.from || !w.to) return out;
    const from = toMinutes(w.from);
    const to = toMinutes(w.to);
    if (from < 0 || to < 0) return out;
    for (let m = from; m !== to; m = (m + 1) % 1440) out.add(m);
    return out;
  }

  function toMinutes(clock) {
    const match = /^(\d\d):(\d\d)$/.exec(clock || '');
    if (!match) return -1;
    return Number(match[1]) * 60 + Number(match[2]);
  }

  function pageMessages() {
    const messages = [];
    const seen = {};
    let visible = 0;
    for (const entry of state.plan.pages) {
      if (!entry.page) {
        messages.push('a page entry is empty');
        continue;
      }
      if (seen[entry.page]) messages.push(entry.page + ': listed twice');
      seen[entry.page] = true;
      if (!entry.hidden) visible++;
      const info = pageInfo(entry.page);
      if (!info) {
        messages.push(entry.page + ': no page file');
        continue;
      }
      if (info.error) messages.push(entry.page + ': ' + info.error);
      else if (state.displays.length && !state.displays.some((d) => info.serves && info.serves.includes(d))) {
        messages.push(entry.page + ': fits no display type of this plan');
      }
    }
    if (state.plan.pages.length && !visible) messages.push('every page is hidden; the device would only show a trigger page');
    return messages;
  }

  function refreshMessages() {
    const messages = [];
    const refresh = state.plan.refresh;
    if (refresh.default && !looksDuration(refresh.default)) messages.push('default ' + refresh.default + ': expected a duration such as 1h');
    if (!refresh.default && !refresh.windows.length) messages.push('no default and no window: the device keeps its own refresh');
    const sets = refresh.windows.map((w) => minuteSet(w));
    refresh.windows.forEach((w, i) => {
      const label = 'window #' + (i + 1);
      if (!w.from || !w.to) messages.push(label + ': from and to are required');
      else if (w.from === w.to) messages.push(label + ': from and to must differ');
      if (!w.interval) messages.push(label + ': interval is missing');
      else if (!looksDuration(w.interval)) messages.push(label + ': ' + w.interval + ' is not a duration such as 10m');
      for (let j = 0; j < i; j++) {
        if (sets[i] && sets[j] && [...sets[i]].some((m) => sets[j].has(m))) {
          messages.push('window #' + (i + 1) + ' overlaps window #' + (j + 1));
        }
      }
    });
    return messages;
  }

  function triggerKey(t) {
    return JSON.stringify(t.when.map(serializeCondition));
  }

  function triggerMessages(trigger, i) {
    const messages = [];
    if (!trigger.page) {
      messages.push('choose a page');
    } else if (!planPages().includes(trigger.page)) {
      messages.push(trigger.page + ' is not in pages; add it above or choose another page');
    } else {
      const info = pageInfo(trigger.page);
      if (info) {
        for (const display of state.displays) {
          const why = info.fits && info.fits[display];
          if (why) messages.push(display + ' skips ' + trigger.page + ' (' + why + ')');
        }
      }
    }
    if (!trigger.when.length) messages.push('a trigger needs at least one condition');
    const seen = {};
    trigger.when.forEach((cond, ci) => {
      const label = 'condition ' + (ci + 1);
      if (cond.value === 'time') {
        if ((cond.from && !cond.to) || (!cond.from && cond.to)) {
          messages.push(label + ': from and to must be given together');
        }
        if (cond.from && cond.from === cond.to) messages.push(label + ': from and to must differ');
        if (!cond.from && !cond.to) messages.push(label + ': give from and to');
        return;
      }
      if (cond.value === 'date') {
        if (!cond.from) messages.push(label + ': from is missing');
        else if (!/^\d{4}-\d{2}-\d{2}$/.test(cond.from)) messages.push(label + ': from is not YYYY-MM-DD');
        if (cond.to && !/^\d{4}-\d{2}-\d{2}$/.test(cond.to)) messages.push(label + ': to is not YYYY-MM-DD');
        if (cond.from && cond.to && cond.to <= cond.from) messages.push(label + ': from and to must be in order');
        return;
      }
      if (!cond.value) {
        messages.push(label + ': choose a value');
        return;
      }
      if (seen[cond.value]) messages.push('duplicate condition ' + cond.value);
      seen[cond.value] = true;
      const spec = valueSpec(cond.value);
      if (!spec) messages.push(cond.value + ': no plugin declares this value');
      else if (spec.type !== 'enum') messages.push(cond.value + ': not an enum value');
      if (!cond.is.length) messages.push(cond.value + ': choose at least one key');
      for (const key of cond.is) {
        if (spec && Array.isArray(spec.enum) && !spec.enum.includes(key)) {
          messages.push(cond.value + ': no key ' + key);
        }
      }
    });
    for (let j = 0; j < i; j++) {
      const other = state.plan.triggers[j];
      if (other.page === trigger.page && triggerKey(other) === triggerKey(trigger)) {
        messages.push('never fires: #' + (j + 1) + ' has the same condition and page and comes first');
      }
    }
    return messages;
  }

  function renderWarnings() {
    if (state.readOnly || !state.plan) return;
    renderMessages(el('page-warnings'), pageMessages());
    renderMessages(el('refresh-warnings'), refreshMessages());
    state.plan.triggers.forEach((trigger, i) => {
      const node = triggerNodes[i];
      if (node) renderMessages(node.warnings, triggerMessages(trigger, i));
    });
    renderMessages(el('trigger-warnings'), []);
  }

  // ---- values ----

  // allValues lists the enum values a trigger condition may name.
  function allValues() {
    const out = [];
    for (const plugin of state.plugins) {
      for (const v of (plugin.description && plugin.description.values) || []) {
        if (v.type !== 'enum') continue;
        out.push({ value: plugin.name + '/' + v.name, spec: v });
      }
    }
    return out.sort((a, b) => a.value.localeCompare(b.value));
  }

  // The plan's own weekday enum; it needs no plugin (ADR 0156).
  const PLAN_ENUMS = { weekday: ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] };

  function valueSpec(value) {
    if (PLAN_ENUMS[value]) return { type: 'enum', enum: PLAN_ENUMS[value] };
    const found = allValues().find((v) => v.value === value);
    return found ? found.spec : null;
  }

  function conditionKeys(value) {
    if (PLAN_ENUMS[value]) return PLAN_ENUMS[value].slice();
    const spec = valueSpec(value);
    return spec && Array.isArray(spec.enum) ? spec.enum.slice() : [];
  }

  // conditionValues lists what a condition may test: the enum values of the
  // configured plugins and the two plan values for a window and a day.
  function conditionValues() {
    const out = allValues().map((v) => ({ value: v.value, label: v.value }));
    out.push(
      { value: 'weekday', label: 'weekday' },
      { value: 'time', label: 'time' },
      { value: 'date', label: 'date' },
    );
    return out.sort((a, b) => a.value.localeCompare(b.value));
  }

  // conditionRow edits one condition. Its value names what it tests, so the
  // value select is the only switch: an enum shows its keys, time the window,
  // and date its days. Unknown stored values stay visible.
  function conditionRow(trigger, cond) {
    const row = document.createElement('div');
    row.className = 'condition';

    const select = document.createElement('select');
    const offered = conditionValues();
    const none = document.createElement('option');
    none.value = '';
    none.textContent = offered.length ? 'choose a value…' : 'no enum value declared';
    select.appendChild(none);
    for (const v of offered) {
      const option = document.createElement('option');
      option.value = v.value;
      option.textContent = v.label;
      select.appendChild(option);
    }
    if (cond.value && !offered.some((v) => v.value === cond.value)) {
      const option = document.createElement('option');
      option.value = cond.value;
      option.textContent = cond.value + ' (not declared)';
      select.appendChild(option);
    }
    select.value = cond.value || '';
    select.addEventListener('change', () => {
      cond.value = select.value;
      changed();
      renderTriggers();
    });

    const fields = document.createElement('span');
    fields.className = 'fields';
    if (cond.value === 'time') {
      fields.appendChild(rangeFields(cond, 'time'));
    } else if (cond.value === 'date') {
      fields.appendChild(rangeFields(cond, 'date'));
    } else {
      fields.appendChild(valueConditionFields(cond));
    }

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'remove';
    remove.title = 'Remove this condition';
    remove.textContent = '×';
    remove.addEventListener('click', () => {
      const i = trigger.when.indexOf(cond);
      if (i >= 0) trigger.when.splice(i, 1);
      changed();
      renderTriggers();
    });

    row.append(select, fields, remove);
    return row;
  }

  function tinyLabel(text, control) {
    const label = document.createElement('label');
    const span = document.createElement('span');
    span.textContent = text;
    label.append(span, control);
    return label;
  }

  function valueConditionFields(cond) {
    const wrap = document.createElement('span');
    wrap.className = 'keys';
    const declared = conditionKeys(cond.value);
    const all = declared.concat(cond.is.filter((key) => !declared.includes(key)));
    if (!all.length) {
      const note = document.createElement('span');
      note.className = 'muted';
      note.textContent = cond.value === 'paperboy/weekday' ? 'this value declares no keys' : cond.value ? 'this value declares no keys' : 'choose a value first';
      wrap.appendChild(note);
    }
    for (const key of all) {
      const label = document.createElement('label');
      const box = document.createElement('input');
      box.type = 'checkbox';
      box.checked = cond.is.includes(key);
      box.addEventListener('change', () => {
        cond.is = all.filter((k) => (k === key ? box.checked : cond.is.includes(k)));
        changed();
      });
      const text = document.createElement('span');
      text.textContent = declared.includes(key) ? key : key + ' (not declared)';
      label.append(box, text);
      wrap.appendChild(label);
    }
    return wrap;
  }

  // rangeFields edits the from and to of a window of the day or a range of
  // days. Both write plain strings; the server checks the format.
  function rangeFields(cond, kind) {
    const wrap = document.createElement('span');
    wrap.className = 'cond-time';
    const make = kind === 'time' ? timeInput : dateInput;
    const from = make(cond.from);
    from.addEventListener('change', () => {
      cond.from = from.value;
      changed();
    });
    wrap.appendChild(tinyLabel('From', from));
    const to = make(cond.to);
    to.addEventListener('change', () => {
      cond.to = to.value;
      changed();
    });
    wrap.appendChild(tinyLabel('To', to));
    return wrap;
  }

  // ---- devices ----

  function renderDevices() {
    const box = el('devices');
    box.innerHTML = '';
    if (!state.devices.length) {
      const p = document.createElement('p');
      p.textContent = 'No device uses this plan.';
      box.appendChild(p);
      return;
    }
    for (const d of state.devices) {
      const row = document.createElement('div');
      row.className = 'device';
      const a = document.createElement('a');
      a.href = '/ui/devices/' + encodeURIComponent(d.id) + '/plan';
      a.textContent = d.id + ' · ' + d.display + ' · preview';
      const edit = document.createElement('a');
      edit.className = 'edit';
      edit.href = '/ui/devices/' + encodeURIComponent(d.id);
      edit.textContent = 'edit';
      row.append(a, edit);
      box.appendChild(row);
    }
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
    if (state.isNew) {
      state.plan = normalize({ pages: [] });
      state.hash = '';
      state.readOnly = false;
      state.dirty = false;
      setStatus('a new plan; add its pages, then Save writes plans/' + state.name + '.yaml', '');
      renderAll();
      return;
    }
    let res;
    try {
      res = await fetch('/api/plans/' + encodeURIComponent(state.name), { cache: 'no-store' });
    } catch (e) {
      setStatus('reading the plan failed: ' + e, 'error');
      return;
    }
    if (!res.ok) {
      setStatus(res.status === 404 ? 'the plan is gone' : 'reading the plan failed (' + res.status + ')', 'error');
      state.readOnly = true;
      state.plan = null;
      renderAll();
      return;
    }
    const doc = await res.json();
    state.hash = doc.hash || '';
    state.dirty = false;
    if (doc.error) {
      state.plan = null;
      state.readOnly = true;
      setStatus('this file does not read as a plan: ' + doc.error, 'error');
    } else {
      state.plan = normalize(doc.plan || { pages: [] });
      state.readOnly = false;
    }
    renderAll();
  }

  async function save() {
    if (!state.plan) return;
    setStatus('saving…', '');
    let res;
    try {
      res = await fetch('/api/plans/' + encodeURIComponent(state.name), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ hash: state.hash, plan: serialize() }),
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
    if (doc.plan) state.plan = normalize(doc.plan);
    state.isNew = false;
    state.dirty = false;
    setStatus('saved ' + new Date().toLocaleTimeString(), 'ok');
    renderAll();
  }

  // rename moves the plan file; the devices that use it keep the old name, so
  // the confirmation names them (ADR 0079).
  async function rename() {
    if (state.isNew || !state.plan || !state.rename) return;
    if (state.dirty && !(await pbAsk.confirm({
      title: 'Discard unsaved changes?',
      text: 'The file keeps what is on disk.',
      ok: 'Discard',
    }))) return;
    const name = await pbAsk.text({
      title: 'Rename plan',
      label: 'New name for ' + state.name,
      value: state.name,
      pattern: '[a-z0-9-]{1,40}',
      hint: 'Lower case letters, digits and -.',
    });
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
    location.href = '/ui/plans/' + encodeURIComponent(name);
  }

  // remove deletes the file after naming the devices that use it; the server
  // refuses a file that changed since it was read (ADR 0073).
  async function remove() {
    if (state.isNew || !state.plan) return;
    const used = state.devices.map((d) => d.id);
    const text = (used.length ? 'Devices that use it: ' + used.join(', ') + '.' : 'No device uses this plan.') +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '') +
      '\n\nA copy stays under state/editor/.';
    if (!(await pbAsk.confirm({ title: 'Delete plans/' + state.name + '.yaml?', text: text, ok: 'Delete', danger: true }))) return;
    setStatus('deleting…', '');
    let res;
    try {
      res = await fetch('/api/plans/' + encodeURIComponent(state.name) + '?hash=' + encodeURIComponent(state.hash), { method: 'DELETE' });
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
    location.href = '/ui/plans';
  }

  function renderAll() {
    renderPages();
    renderRefresh();
    renderTriggers();
    renderDevices();
    updateWhere();
    updateSave();
  }

  // ---- wiring ----

  el('save').addEventListener('click', save);
  el('delete').addEventListener('click', remove);
  el('rename').addEventListener('click', rename);
  el('reload').addEventListener('click', () => location.reload());
  el('add-trigger').addEventListener('click', addTrigger);
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
