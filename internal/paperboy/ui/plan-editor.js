// Plan editor for Paperboy (ADR 0069). Vanilla JavaScript, no build step: the
// plan model comes from the plan API (ADR 0066), the actions from the plugin
// descriptions, and edits stay in the browser until Save writes the file with
// the hash it was read with.
(() => {
  'use strict';

  const el = (id) => document.getElementById(id);
  const start = JSON.parse(el('plan-data').textContent);
  const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];

  const state = {
    name: start.name,
    devices: start.devices || [],
    displays: start.displays || [], // display types of the devices that use this plan
    pages: start.pages || [],
    plugins: [],
    plan: null,
    hash: '',
    isNew: !!start.new, // no file yet: the first save writes it (ADR 0073)
    rename: start.rename || null, // endpoint and confirmation for a rename (ADR 0079)
    readOnly: false,
    dirty: false,
  };

  let ruleNodes = [];

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

  function ruleNames() {
    return state.plan ? state.plan.rules.map((r) => r.name).filter((n) => n) : [];
  }

  function normalize(p) {
    p.rules = (p.rules || []).map((r) => {
      const w = r.when || {};
      return {
        name: r.name || '',
        when: { from: w.from || '', to: w.to || '', days: (w.days || []).filter((d) => DAYS.includes(d)) },
        pages: (r.pages || []).slice(),
        rotate: !!r.rotate,
        refresh: r.refresh || '',
      };
    });
    p.fallback = p.fallback || '';
    p.actions = (p.actions || []).map((a) => ({
      action: a.action || '',
      args: a.args || {},
      id: a.id || '',
      title: a.title || '',
    }));
    return p;
  }

  // serialize builds the body of PUT /api/plans: only what the API knows, with
  // empty windows, days and durations left out.
  function serialize() {
    const p = state.plan;
    const out = {
      rules: p.rules.map((r) => {
        const rule = { name: r.name, when: {}, pages: r.pages.slice() };
        if (r.when.from && r.when.to) {
          rule.when.from = r.when.from;
          rule.when.to = r.when.to;
        }
        const days = DAYS.filter((d) => r.when.days.includes(d));
        if (days.length) rule.when.days = days;
        if (r.rotate) rule.rotate = true;
        if (r.refresh) rule.refresh = r.refresh;
        return rule;
      }),
    };
    if (p.fallback) out.fallback = p.fallback;
    if (p.actions.length) {
      out.actions = p.actions.map((a) => {
        const action = { action: a.action };
        if (a.args && Object.keys(a.args).length) action.args = a.args;
        if (a.id) action.id = a.id;
        if (a.title) action.title = a.title;
        return action;
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

  function small(label, action) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'small';
    b.textContent = label;
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

  // ---- rules ----

  function renderRules() {
    const box = el('rules');
    box.innerHTML = '';
    ruleNodes = [];
    if (state.readOnly || !state.plan) {
      box.innerHTML = '<p class="hint">The file does not read as a plan; fix it by hand and reload.</p>';
      return;
    }
    if (!state.plan.rules.length) {
      box.innerHTML = '<p class="hint">No rules yet; a plan needs at least one rule with a page.</p>';
      return;
    }
    state.plan.rules.forEach((rule, i) => box.appendChild(ruleCard(rule, i)));
    renderWarnings();
  }

  function ruleCard(rule, i) {
    const card = document.createElement('section');
    card.className = 'rule';

    const head = document.createElement('div');
    head.className = 'rule-head';
    const num = document.createElement('span');
    num.className = 'num';
    num.textContent = '#' + (i + 1);
    const badge = document.createElement('span');
    badge.className = 'badge';
    badge.textContent = 'shadowed';
    badge.hidden = true;
    const buttons = document.createElement('div');
    buttons.className = 'buttons';
    buttons.append(
      small('↑', i > 0 ? () => moveRule(i, -1) : null),
      small('↓', i < state.plan.rules.length - 1 ? () => moveRule(i, 1) : null),
      small('×', () => removeRule(i)),
    );
    head.append(num, badge, buttons);
    card.appendChild(head);

    const grid = document.createElement('div');
    grid.className = 'rule-grid';
    const name = textInput(rule.name, 'e.g. day');
    name.pattern = '[a-z0-9-]{1,40}';
    name.addEventListener('input', () => {
      const previous = rule.name;
      const next = name.value;
      if (previous && state.plan.fallback === previous) state.plan.fallback = next;
      rule.name = next;
      renderFallback();
      changed();
    });
    grid.appendChild(field('Name', name));

    const from = document.createElement('input');
    from.type = 'time';
    from.value = rule.when.from;
    from.addEventListener('change', () => {
      rule.when.from = from.value;
      changed();
    });
    grid.appendChild(field('From', from));
    const to = document.createElement('input');
    to.type = 'time';
    to.value = rule.when.to;
    to.addEventListener('change', () => {
      rule.when.to = to.value;
      changed();
    });
    grid.appendChild(field('To', to));

    const days = document.createElement('span');
    days.className = 'days';
    for (const d of DAYS) {
      const label = document.createElement('label');
      const box = document.createElement('input');
      box.type = 'checkbox';
      box.checked = rule.when.days.includes(d);
      box.addEventListener('change', () => {
        rule.when.days = DAYS.filter((x) => (x === d ? box.checked : rule.when.days.includes(x)));
        changed();
      });
      const text = document.createElement('span');
      text.textContent = d;
      label.append(box, text);
      days.appendChild(label);
    }
    grid.appendChild(field('Days', days));

    const refresh = textInput(rule.refresh, 'e.g. 10m');
    refresh.addEventListener('input', () => {
      rule.refresh = refresh.value.trim();
      changed();
    });
    grid.appendChild(field('Refresh', refresh));

    const rotate = document.createElement('input');
    rotate.type = 'checkbox';
    rotate.checked = rule.rotate;
    rotate.addEventListener('change', () => {
      rule.rotate = rotate.checked;
      changed();
    });
    grid.appendChild(field('Rotate', rotate));
    card.appendChild(grid);

    const pages = document.createElement('div');
    pages.className = 'pages';
    if (!rule.pages.length) {
      const empty = document.createElement('p');
      empty.className = 'empty';
      empty.textContent = 'No pages; a rule needs at least one.';
      pages.appendChild(empty);
    }
    rule.pages.forEach((page, j) => pages.appendChild(pageRow(rule, j, page)));
    card.appendChild(pages);

    const add = document.createElement('div');
    add.className = 'add-page';
    const select = document.createElement('select');
    const free = state.pages.filter((p) => !rule.pages.includes(p.name));
    const none = document.createElement('option');
    none.value = '';
    none.textContent = free.length ? 'add a page…' : 'no other pages';
    select.appendChild(none);
    for (const p of free) {
      const option = document.createElement('option');
      option.value = p.name;
      option.textContent = p.name + (p.kind ? ' · ' + p.kind : '');
      select.appendChild(option);
    }
    const addButton = document.createElement('button');
    addButton.type = 'button';
    addButton.textContent = 'Add';
    addButton.disabled = !free.length;
    addButton.addEventListener('click', () => {
      if (!select.value) return;
      rule.pages.push(select.value);
      changed();
      renderRules();
    });
    add.append(select, addButton);
    card.appendChild(add);

    const warnings = document.createElement('div');
    card.appendChild(warnings);
    ruleNodes[i] = { card, badge, warnings };
    return card;
  }

  function pageRow(rule, j, page) {
    const row = document.createElement('div');
    row.className = 'page-row';
    const info = pageInfo(page);
    if (!info) row.classList.add('missing');
    const label = document.createElement('span');
    label.className = 'name';
    label.textContent = page;
    const meta = document.createElement('span');
    meta.className = 'muted';
    meta.textContent = info
      ? [info.kind, info.grid ? info.grid[0] + '×' + info.grid[1] : ''].filter(Boolean).join(' ')
      : 'no file';
    row.append(
      label,
      meta,
      small('↑', j > 0 ? () => movePage(rule, j, -1) : null),
      small('↓', j < rule.pages.length - 1 ? () => movePage(rule, j, 1) : null),
      small('×', () => {
        rule.pages.splice(j, 1);
        changed();
        renderRules();
      }),
    );
    return row;
  }

  function movePage(rule, j, step) {
    const to = j + step;
    if (to < 0 || to >= rule.pages.length) return;
    const [page] = rule.pages.splice(j, 1);
    rule.pages.splice(to, 0, page);
    changed();
    renderRules();
  }

  function moveRule(i, step) {
    const to = i + step;
    if (to < 0 || to >= state.plan.rules.length) return;
    const [rule] = state.plan.rules.splice(i, 1);
    state.plan.rules.splice(to, 0, rule);
    changed();
    renderRules();
  }

  function removeRule(i) {
    state.plan.rules.splice(i, 1);
    changed();
    renderRules();
  }

  // ---- warnings ----

  function alwaysMatches(w) {
    return !w.from && !w.to && !w.days.length;
  }

  function windowKey(w) {
    return w.from + '|' + w.to + '|' + DAYS.filter((d) => w.days.includes(d)).join(',');
  }

  // shadowReason names the earlier rule that keeps a rule from ever matching:
  // one that matches at any time, or one with the same time window.
  function shadowReason(i) {
    const rules = state.plan.rules;
    for (let j = 0; j < i; j++) {
      const name = rules[j].name || 'rule #' + (j + 1);
      if (alwaysMatches(rules[j].when)) return 'never matches: ' + name + ' matches at any time and comes first';
      if (windowKey(rules[j].when) === windowKey(rules[i].when)) {
        return 'never matches: ' + name + ' has the same time window and comes first';
      }
    }
    return '';
  }

  function renderWarnings() {
    if (state.readOnly || !state.plan) return;
    const seen = {};
    state.plan.rules.forEach((rule, i) => {
      const messages = [];
      if (!/^[a-z0-9-]{1,40}$/.test(rule.name)) messages.push('name: only a-z, 0-9 and -');
      if (seen[rule.name]) messages.push('duplicate rule name ' + rule.name);
      seen[rule.name] = true;
      const { from, to } = rule.when;
      if ((from && !to) || (!from && to)) messages.push('from and to must be given together');
      if (from && to && from === to) messages.push('from and to must differ');
      if (!rule.pages.length) messages.push('no pages');
      for (const page of new Set(rule.pages)) {
        if (rule.pages.filter((p) => p === page).length > 1) messages.push('page ' + page + ' is listed twice');
        if (!pageInfo(page)) messages.push(page + ': no page file');
      }
      for (const display of state.displays) {
        for (const page of rule.pages) {
          const info = pageInfo(page);
          const why = info && info.fits && info.fits[display];
          if (why) messages.push(display + ' skips ' + page + ' (' + why + ')');
        }
      }
      const shadow = shadowReason(i);
      if (shadow) messages.push(shadow);
      const node = ruleNodes[i];
      if (!node) return;
      node.card.classList.toggle('shadowed', !!shadow);
      node.badge.hidden = !shadow;
      renderMessages(node.warnings, messages);
    });
    renderFallbackWarnings();
  }

  function renderFallbackWarnings() {
    const messages = [];
    const fallback = state.plan.fallback;
    if (fallback && !ruleNames().includes(fallback)) messages.push('fallback ' + fallback + ': no such rule');
    renderMessages(el('fallback-warnings'), messages);
  }

  function renderFallback() {
    const select = el('fallback');
    select.innerHTML = '';
    const none = document.createElement('option');
    none.value = '';
    none.textContent = 'none (the device gets a notice)';
    select.appendChild(none);
    for (const name of ruleNames()) {
      const option = document.createElement('option');
      option.value = name;
      option.textContent = name;
      select.appendChild(option);
    }
    select.value = state.plan && ruleNames().includes(state.plan.fallback) ? state.plan.fallback : '';
  }

  // ---- actions ----

  function allActions() {
    const out = [];
    for (const plugin of state.plugins) {
      for (const action of (plugin.description && plugin.description.actions) || []) {
        out.push({ value: plugin.name + '/' + action.name, spec: action });
      }
    }
    return out.sort((a, b) => a.value.localeCompare(b.value));
  }

  function actionSpec(value) {
    const [plugin, name] = value.split('/');
    const p = state.plugins.find((x) => x.name === plugin);
    const actions = (p && p.description && p.description.actions) || [];
    return actions.find((a) => a.name === name) || null;
  }

  function renderActions() {
    const box = el('actions');
    box.innerHTML = '';
    if (state.readOnly || !state.plan) {
      box.innerHTML = '<p class="hint">The file does not read as a plan; fix it by hand and reload.</p>';
      return;
    }
    if (!state.plan.actions.length) {
      box.innerHTML = '<p class="hint">No actions; the device menu then has no entries.</p>';
      return;
    }
    state.plan.actions.forEach((action, i) => box.appendChild(actionCard(action, i)));
    renderActionWarnings();
  }

  function actionCard(action, i) {
    const card = document.createElement('section');
    card.className = 'action';

    const head = document.createElement('div');
    head.className = 'action-head';
    const select = document.createElement('select');
    const none = document.createElement('option');
    none.value = '';
    none.textContent = 'choose an action…';
    select.appendChild(none);
    const offered = allActions();
    for (const a of offered) {
      const option = document.createElement('option');
      option.value = a.value;
      option.textContent = a.value + (a.spec.title ? ' — ' + a.spec.title : '');
      select.appendChild(option);
    }
    if (action.action && !offered.some((a) => a.value === action.action)) {
      const option = document.createElement('option');
      option.value = action.action;
      option.textContent = action.action + ' (not offered by a configured plugin)';
      select.appendChild(option);
    }
    select.value = action.action;
    select.addEventListener('change', () => {
      action.action = select.value;
      action.args = {};
      changed();
      renderActions();
    });
    head.append(select, small('×', () => {
      state.plan.actions.splice(i, 1);
      changed();
      renderActions();
    }));
    card.appendChild(head);

    const grid = document.createElement('div');
    grid.className = 'rule-grid';
    const id = textInput(action.id, 'id');
    id.pattern = '[a-z0-9-]{1,40}';
    id.addEventListener('input', () => {
      action.id = id.value.trim();
      changed();
    });
    grid.appendChild(field('ID', id));
    const spec = actionSpec(action.action);
    const title = textInput(action.title, spec && spec.title ? spec.title : 'menu title');
    title.addEventListener('input', () => {
      action.title = title.value;
      changed();
    });
    grid.appendChild(field('Title', title));
    card.appendChild(grid);

    if (!spec && action.action) {
      const note = document.createElement('p');
      note.className = 'note';
      note.textContent = 'Unknown action: a configured plugin has to offer it.';
      card.appendChild(note);
    }
    if (spec && spec.confirm && spec.confirm.required) {
      const note = document.createElement('p');
      note.className = 'note';
      note.textContent = 'Asks for a confirmation before it runs' + (spec.confirm.text ? ': ' + spec.confirm.text : '.');
      card.appendChild(note);
    }
    const args = (spec && spec.args) || {};
    for (const name of Object.keys(args).sort()) card.appendChild(argField(action, name, args[name]));
    return card;
  }

  function argField(action, name, arg) {
    const wrap = document.createElement('div');
    wrap.className = 'arg';
    const label = document.createElement('span');
    label.className = 'name';
    label.textContent = name + (arg.required ? ' *' : '');
    wrap.appendChild(label);
    const current = action.args[name];
    let control;
    if (arg.type === 'bool') {
      control = document.createElement('input');
      control.type = 'checkbox';
      control.checked = current === undefined ? arg.default === true : current === true;
      control.addEventListener('change', () => {
        if (control.checked === (arg.default === true)) delete action.args[name];
        else action.args[name] = control.checked;
        changed();
      });
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
        if (control.value === '') delete action.args[name];
        else {
          const chosen = arg.enum.find((v) => String(v) === control.value);
          action.args[name] = chosen === undefined ? control.value : chosen;
        }
        changed();
      });
    } else if (arg.type === 'list' && Array.isArray(arg.enum) && arg.enum.length) {
      // A list with a fixed set: one check box per value.
      control = document.createElement('div');
      control.className = 'checks';
      const chosen = (Array.isArray(current) ? current : Array.isArray(arg.default) ? arg.default : []).map(String);
      const pick = () => {
        const picked = [...control.querySelectorAll('input')].filter((i) => i.checked).map((i) => i.value);
        if (picked.length === 0) delete action.args[name];
        else action.args[name] = picked;
        changed();
      };
      for (const value of arg.enum) {
        const item = document.createElement('label');
        const box = document.createElement('input');
        box.type = 'checkbox';
        box.value = String(value);
        box.checked = chosen.includes(String(value));
        box.addEventListener('change', pick);
        item.appendChild(box);
        item.appendChild(document.createTextNode(String(value)));
        control.appendChild(item);
      }
    } else {
      control = document.createElement('input');
      control.type = 'text';
      control.autocomplete = 'off';
      if (arg.default !== undefined && arg.default !== null) control.placeholder = String(arg.default);
      if (Array.isArray(current)) control.value = current.join(', ');
      else if (current !== undefined) control.value = String(current);
      control.addEventListener('input', () => {
        const raw = control.value;
        if (raw === '') delete action.args[name];
        else action.args[name] = argValue(raw, arg);
        changed();
      });
    }
    wrap.appendChild(control);
    if (arg.description) {
      const desc = document.createElement('span');
      desc.className = 'desc';
      desc.textContent = arg.description;
      wrap.appendChild(desc);
    }
    return wrap;
  }

  function argValue(raw, arg) {
    if (arg && arg.type === 'list') {
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

  function argsKey(args) {
    return Object.keys(args || {})
      .sort()
      .map((k) => k + '=' + JSON.stringify(args[k]))
      .join('&');
  }

  // renderActionWarnings checks what the API rejects: an action that appears
  // more than once needs an id on every entry, ids are unique, and equal
  // action plus args appear once (ADR 0057).
  function renderActionWarnings() {
    if (state.readOnly || !state.plan) return;
    const messages = [];
    const count = {};
    for (const a of state.plan.actions) if (a.action) count[a.action] = (count[a.action] || 0) + 1;
    const ids = {};
    const seen = {};
    for (const a of state.plan.actions) {
      if (!a.action) {
        messages.push('an action is missing');
        continue;
      }
      if (count[a.action] > 1 && !a.id) messages.push(a.action + ' appears more than once: every entry needs an id');
      if (a.id) {
        if (!/^[a-z0-9-]{1,40}$/.test(a.id)) messages.push('id ' + a.id + ': only a-z, 0-9 and -');
        else if (ids[a.id]) messages.push('duplicate id ' + a.id);
        ids[a.id] = true;
      }
      const key = a.action + ' ' + argsKey(a.args);
      if (seen[key]) messages.push('duplicate action and arguments: ' + a.action);
      seen[key] = true;
    }
    renderMessages(el('action-warnings'), messages);
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
      a.href = '/ui/plan?device=' + encodeURIComponent(d.id);
      a.textContent = d.id + ' · ' + d.display + ' · preview';
      const edit = document.createElement('a');
      edit.className = 'edit';
      edit.href = '/ui/editor/device?id=' + encodeURIComponent(d.id);
      edit.textContent = 'edit';
      row.append(a, edit);
      box.appendChild(row);
    }
  }

  // ---- load and save ----

  function addRule() {
    if (!state.plan) return;
    const taken = new Set(ruleNames());
    let n = state.plan.rules.length + 1;
    while (taken.has('rule-' + n)) n++;
    state.plan.rules.push({ name: 'rule-' + n, when: { from: '', to: '', days: [] }, pages: [], rotate: false, refresh: '' });
    changed();
    renderRules();
    renderFallback();
  }

  function addAction() {
    if (!state.plan) return;
    state.plan.actions.push({ action: '', args: {}, id: '', title: '' });
    changed();
    renderActions();
  }

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
      state.plan = normalize({ rules: [] });
      state.hash = '';
      state.readOnly = false;
      state.dirty = false;
      setStatus('a new plan; add a rule with its pages, then Save writes plans/' + state.name + '.yaml', '');
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
      state.plan = normalize(doc.plan || { rules: [] });
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
    location.href = '/ui/editor/plan?name=' + encodeURIComponent(name);
  }

  // remove deletes the file after naming the devices that use it; the server
  // refuses a file that changed since it was read (ADR 0073).
  async function remove() {
    if (state.isNew || !state.plan) return;
    const used = state.devices.map((d) => d.id);
    const warn = 'Delete plans/' + state.name + '.yaml?' +
      (used.length ? '\n\nDevices that use it: ' + used.join(', ') + '.' : '\n\nNo device uses this plan.') +
      (state.dirty ? '\n\nUnsaved changes are discarded.' : '') +
      '\n\nA copy stays under state/editor/.';
    if (!confirm(warn)) return;
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
    location.href = '/ui/editor?tab=plans';
  }

  function renderAll() {
    renderFallback();
    renderRules();
    renderActions();
    renderDevices();
    updateWhere();
    updateSave();
  }

  // ---- wiring ----

  el('save').addEventListener('click', save);
  el('delete').addEventListener('click', remove);
  el('rename').addEventListener('click', rename);
  el('reload').addEventListener('click', () => location.reload());
  el('add-rule').addEventListener('click', addRule);
  el('add-action').addEventListener('click', addAction);
  el('fallback').addEventListener('change', () => {
    if (!state.plan) return;
    state.plan.fallback = el('fallback').value;
    changed();
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
