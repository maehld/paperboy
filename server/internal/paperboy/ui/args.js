// The argument fields of one cell or header part, built from the plugin
// description. The page editor and the display type editor share this file, so
// an argument looks and behaves the same in both (ADR 0168).
(() => {
  'use strict';

  // enumLabel is the display text of an enum value; the stored value stays
  // the key, and plugins without labels keep the key as the label.
  function enumLabel(arg, value) {
    const key = String(value);
    return (arg && arg.enum_labels && arg.enum_labels[key]) || key;
  }

  function jsonOrString(s) {
    try {
      return JSON.parse(s);
    } catch (e) {
      return s;
    }
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

  function setArg(cell, name, raw, arg, change) {
    if (!arg || arg.type !== 'bool') {
      if (raw === '' || raw === null || (Array.isArray(raw) && raw.length === 0)) delete cell.args[name];
      else cell.args[name] = argValue(raw, arg);
      (change || (() => {}))();
      return;
    }
    // A bool that differs from its declared default is written out, so an
    // unchecked box can still mean false for a default of true.
    const value = raw === true;
    const fallback = arg.default === true;
    if (value === fallback) delete cell.args[name];
    else cell.args[name] = value;
    (change || (() => {}))();
  }

  // render draws one field per declared argument. A bool is a checkbox, an
  // enum a select, a fixed list a row of checkboxes, everything else a text
  // field. change() records the edit; rerender() rebuilds the surrounding
  // panel when a control needs the new value in its own markup.
  function render(box, c, args, opts) {
    const actionArg = !!(opts && opts.action);
    const rebuild = !!(opts && opts.rebuild);
    const change = (opts && opts.change) || (() => {});
    const rerender = (opts && opts.rerender) || (() => {});
    const argChanged = () => { if (rebuild) rerender(); };
    c.args = c.args || {};
    const names = Object.keys(args).sort();
    if (names.length && !(opts && opts.heading === false)) {
      const h = document.createElement('h4');
      h.textContent = (opts && opts.heading) || 'Arguments';
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
        control.addEventListener('change', () => {
          setArg(c, name, control.checked, arg, change);
          argChanged();
        });
      } else if (Array.isArray(arg.enum) && arg.enum.length && arg.type !== 'list') {
        control = document.createElement('select');
        const none = document.createElement('option');
        none.value = '';
        // An argument may label its empty value, e.g. the device that shows
        // the page (ADR 0123). An empty required enum of an action is the
        // choice: the device offers its values (ADR 0164).
        none.textContent = actionArg && arg.required
          ? 'the device offers the values'
          : enumLabel(arg, '') ||
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
          if (control.value === '') setArg(c, name, '', arg, change);
          else {
            const chosen = arg.enum.find((v) => String(v) === control.value);
            c.args[name] = chosen === undefined ? control.value : chosen;
            change();
          }
          argChanged();
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
          const box2 = document.createElement('input');
          box2.type = 'checkbox';
          box2.value = String(value);
          box2.checked = chosen.includes(String(value));
          const text = document.createElement('span');
          text.textContent = enumLabel(arg, value);
          if (!arg.enum.some((v) => String(v) === String(value))) text.textContent += ' (not configured)';
          text.title = String(value);
          item.append(box2, text);
          grid.appendChild(item);
          boxes.push(box2);
        }
        const pick = () => {
          const picked = boxes.filter((i) => i.checked).map((i) => i.value);
          if (count) count.textContent = picked.length + ' of ' + boxes.length;
          // An empty pick stays an empty list when the arg has a default, so
          // "None" means none and not "show the defaults again".
          if (picked.length === 0 && Array.isArray(arg.default)) {
            c.args[name] = [];
            change();
            argChanged();
            return;
          }
          setArg(c, name, picked, arg, change);
          argChanged();
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
              for (const box2 of boxes) box2.checked = on;
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
        for (const box2 of boxes) box2.addEventListener('change', pick);
      } else {
        control = document.createElement('input');
        control.type = 'text';
        control.autocomplete = 'off';
        if (arg.default !== undefined && arg.default !== null) control.placeholder = String(arg.default);
        if (Array.isArray(current)) control.value = current.join(', ');
        else if (current !== undefined) control.value = String(current);
        control.addEventListener('input', () => setArg(c, name, control.value, arg, change));
        if (rebuild) control.addEventListener('change', argChanged);
      }
      wrap.appendChild(control);
      if (actionArg && arg.required && Array.isArray(arg.enum) && arg.enum.length && arg.type !== 'list') {
        const note = document.createElement('span');
        note.className = 'desc';
        note.textContent = 'Empty: the device offers the values.';
        wrap.appendChild(note);
      }
      if (arg.description) {
        const desc = document.createElement('span');
        desc.className = 'desc';
        desc.textContent = arg.description;
        wrap.appendChild(desc);
      }
      box.appendChild(wrap);
    }
  }

  window.pbArgs = { render: render, argValue: argValue, setArg: setArg, enumLabel: enumLabel, jsonOrString: jsonOrString };
})();
