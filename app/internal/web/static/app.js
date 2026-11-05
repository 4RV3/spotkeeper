(function () {
  'use strict';
  // Applied before first paint (script is loaded in <head> without defer).
  try {
    var saved = localStorage.getItem('theme');
    if (saved === 'light' || saved === 'dark') document.documentElement.setAttribute('data-theme', saved);
  } catch (e) { /* ignore */ }
  function init() {
  document.addEventListener('click', function (e) {
    var el = e.target.closest ? e.target.closest('[data-dismiss]') : null;
    var toast = el && el.closest('.toast');
    if (toast) toast.remove();
  });

  document.addEventListener('submit', function (e) {
    var msg = e.target.getAttribute && e.target.getAttribute('data-confirm');
    if (msg && !window.confirm(msg)) e.preventDefault();
  });

  // Auto-dismiss the flash toast.
  var first = document.querySelector('.toast');
  if (first) setTimeout(function () { if (first.parentNode) first.remove(); }, parseInt(first.getAttribute('data-autohide') || '6000', 10));
  if (location.search.indexOf('msg=') !== -1 && window.history && history.replaceState) {
    history.replaceState(null, '', location.pathname);
  }

  // Theme toggle: system -> light -> dark.
  (function () {
    var btn = document.getElementById('theme-toggle');
    if (!btn) return;
    var ORDER = ['system', 'light', 'dark'];
    var NAMES = { system: 'systemowy', light: 'jasny', dark: 'ciemny' };
    function get() { try { var t = localStorage.getItem('theme'); return t === 'light' || t === 'dark' ? t : 'system'; } catch (e) { return 'system'; } }
    function apply(t) {
      var root = document.documentElement;
      if (t === 'system') root.removeAttribute('data-theme'); else root.setAttribute('data-theme', t);
      var label = 'Motyw: ' + NAMES[t];
      btn.setAttribute('aria-label', label + ' (zmień)');
      btn.setAttribute('title', label);
    }
    apply(get());
    btn.addEventListener('click', function () {
      var next = ORDER[(ORDER.indexOf(get()) + 1) % ORDER.length];
      try { if (next === 'system') localStorage.removeItem('theme'); else localStorage.setItem('theme', next); } catch (e) { /* ignore */ }
      apply(next);
    });
  })();

  // Level control: the options come from the chosen parking (/settings/levels).
  (function () {
    var parking = document.getElementById('parking_id');
    var level = document.getElementById('level');
    var note = document.getElementById('level-note');
    if (!parking || !level || !parking.getAttribute('data-levels-url')) return;
    var url = parking.getAttribute('data-levels-url');
    var seq = 0;
    function say(text) { if (text) { note.textContent = text; note.hidden = false; } else { note.textContent = ''; note.hidden = true; } }
    // Mirrors levelOptions in parkings.go: an exact match wins, then a unique
    // case-insensitive substring match; otherwise one labelled extra option.
    function fill(levels, keep) {
      while (level.firstChild) level.removeChild(level.firstChild);
      function add(value, label) {
        var o = document.createElement('option');
        o.value = value; o.textContent = label;
        level.appendChild(o);
      }
      add('', 'dowolny');
      keep = (keep || '').trim();
      var sel = '', extra = '';
      if (keep) {
        if (!levels.length) extra = keep;
        else if (levels.indexOf(keep) !== -1) sel = keep;
        else {
          var hits = levels.filter(function (l) { return l.toLowerCase().indexOf(keep.toLowerCase()) !== -1; });
          if (hits.length === 1) sel = hits[0]; else extra = keep;
        }
      }
      levels.forEach(function (l) { add(l, l); });
      if (extra) add(extra, levels.length ? extra + ' (zapisany, nie występuje na parkingu)' : extra);
      level.value = sel || extra || '';
    }
    function load(keep) {
      var id = ++seq;
      if (!/^[0-9a-fA-F-]{36}$/.test(parking.value)) { fill([], keep); say(''); return; }
      fetch(url + '?parking=' + encodeURIComponent(parking.value), { credentials: 'same-origin', headers: { 'Accept': 'application/json' } })
        .then(function (r) {
          if (r.redirected || !r.ok) throw new Error('levels');
          return r.json();
        })
        .then(function (data) {
          if (id !== seq) return;
          var levels = (data && data.levels) || [];
          fill(levels, keep);
          say(levels.length ? '' : 'Poziomy niedostępne — brak wolnych miejsc do sprawdzenia; ' + (keep ? 'zachowano zapisany poziom.' : 'zostanie użyty dowolny poziom.'));
        })
        .catch(function () {
          if (id !== seq) return;
          fill([], keep);
          say('Nie udało się pobrać poziomów parkingu.');
        });
    }
    parking.addEventListener('change', function () { load(''); });
    load(level.getAttribute('data-current') || '');
  })();

  // Live configuration check: POST /check/stream returns NDJSON progress lines.
  (function () {
    var form = document.getElementById('check-form');
    var live = document.getElementById('check-live');
    var msg = document.getElementById('check-msg');
    if (!form || !live || !window.fetch || !window.TextDecoder) return;
    var btn = form.querySelector('button');
    var rows = live.querySelectorAll('li');
    var timer = null, started = 0, current = -1;
    var ICON = { pending: '·', running: '', ok: '✓', fail: '✗', skipped: '–' };
    function setRow(i, state, detail, help) {
      var li = rows[i];
      if (!li) return;
      li.setAttribute('data-state', state);
      li.querySelector('.st-ic').textContent = ICON[state] || '';
      var d = li.querySelector('.st-detail');
      if (detail != null) d.textContent = detail;
      var old = li.querySelector('a.st-help');
      if (old) old.remove();
      if (help && state === 'fail') {
        var a = document.createElement('a');
        a.className = 'st-help'; a.href = help; a.textContent = 'Zobacz, jak to naprawić';
        li.appendChild(a);
      }
    }
    function say(text, err) {
      msg.textContent = text; msg.hidden = !text;
      msg.className = err ? 'alert err' : 'muted';
    }
    function tickElapsed() {
      if (current < 0) return;
      var sec = Math.floor((Date.now() - started) / 1000);
      var d = rows[current] && rows[current].querySelector('.st-detail');
      if (d) d.textContent = 'Sprawdzam… ' + sec + ' s';
    }
    function stop() {
      if (timer) { clearInterval(timer); timer = null; }
      current = -1; btn.disabled = false; btn.removeAttribute('aria-busy');
    }
    function handle(o) {
      if (o.done) {
        stop();
        say(o.allOk ? 'Konfiguracja poprawna.' : 'Kontrola wykryła problem – popraw ustawienia i sprawdź ponownie.', !o.allOk);
        btn.textContent = 'Sprawdź ponownie';
        return;
      }
      if (o.state === 'running') {
        current = o.i; started = Date.now(); setRow(o.i, 'running', 'Sprawdzam… 0 s');
      } else {
        if (current === o.i) current = -1;
        setRow(o.i, o.state, o.detail || '', o.help || '');
      }
    }
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (btn.disabled) return;
      var old = document.querySelectorAll('ul.check-steps:not(.live)');
      for (var k = 0; k < old.length; k++) old[k].hidden = true;
      live.hidden = false; say('');
      for (var i = 0; i < rows.length; i++) setRow(i, 'pending', 'oczekuje');
      btn.disabled = true; btn.setAttribute('aria-busy', 'true');
      timer = setInterval(tickElapsed, 1000);
      var finished = false;
      fetch(live.getAttribute('data-stream'), { method: 'POST', credentials: 'same-origin', headers: { 'Accept': 'application/x-ndjson' } })
        .then(function (r) {
          if (r.redirected || !r.ok || !r.body) throw new Error('http ' + r.status);
          var reader = r.body.getReader(), dec = new TextDecoder(), buf = '';
          function lines(final) {
            var parts = buf.split('\n');
            buf = final ? '' : parts.pop();
            parts.forEach(function (ln) {
              if (!ln.trim()) return;
              var o; try { o = JSON.parse(ln); } catch (x) { return; }
              if (o.done) finished = true;
              handle(o);
            });
          }
          function pump() {
            return reader.read().then(function (res) {
              if (res.done) { buf += dec.decode(); lines(true); return; }
              buf += dec.decode(res.value, { stream: true });
              lines(false);
              return pump();
            });
          }
          return pump();
        })
        .then(function () { if (!finished) throw new Error('interrupted'); })
        .catch(function () {
          stop();
          say('Nie udało się wykonać kontroli: połączenie z serwerem zostało przerwane. Spróbuj ponownie.', true);
        });
    });
  })();

  // Live countdown.
  function tick() {
    var els = document.querySelectorAll('[data-countdown]');
    for (var i = 0; i < els.length; i++) {
      var ts = Date.parse(els[i].getAttribute('data-countdown'));
      if (isNaN(ts)) continue;
      var mins = Math.max(0, Math.floor((ts - Date.now()) / 60000));
      var d = Math.floor(mins / 1440), h = Math.floor(mins % 1440 / 60), m = mins % 60;
      els[i].textContent = mins < 1 ? 'mniej niż minutę' : (d ? d + ' d ' : '') + (d || h ? h + ' h ' : '') + m + ' min';
    }
  }
  tick();
  setInterval(tick, 30000);

  // Auto-refresh of the dashboard sections.
  var IDS = ['kpis', 'calendar', 'activity'];
  var busy = false;
  function formFocused() {
    var a = document.activeElement;
    return !!a && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName);
  }
  function refresh() {
    if (busy || document.hidden || formFocused() || !document.getElementById('kpis')) return;
    busy = true;
    fetch(location.pathname + location.search, { credentials: 'same-origin', headers: { 'X-Requested-With': 'fetch' } })
      .then(function (r) {
        if (r.redirected || !r.ok) { throw new Error('skip'); }
        return r.text();
      })
      .then(function (html) {
        if (formFocused()) return;
        var doc = new DOMParser().parseFromString(html, 'text/html');
        IDS.forEach(function (id) {
          var fresh = doc.getElementById(id), cur = document.getElementById(id);
          if (fresh && cur) cur.replaceWith(document.importNode(fresh, true));
        });
        tick();
      })
      .catch(function () { /* keep the current view */ })
      .then(function () { busy = false; });
  }
  setInterval(refresh, 20000);

  // Calendar month navigation: swap only the calendar and keep the URL (?m=) in sync;
  // without JS the links reload the page at #calendar.
  function loadCalendar(href, push) {
    var url = href.split('#')[0];
    return fetch(url, { credentials: 'same-origin', headers: { 'X-Requested-With': 'fetch' } })
      .then(function (r) {
        if (r.redirected || !r.ok) { throw new Error('skip'); }
        return r.text();
      })
      .then(function (html) {
        var fresh = new DOMParser().parseFromString(html, 'text/html').getElementById('calendar');
        var cur = document.getElementById('calendar');
        if (!fresh || !cur) { throw new Error('skip'); }
        cur.replaceWith(document.importNode(fresh, true));
        if (push) { history.pushState({ cal: true }, '', url); }
      });
  }
  document.addEventListener('click', function (e) {
    var a = e.target.closest ? e.target.closest('a[data-cal-nav]') : null;
    if (!a || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    loadCalendar(a.getAttribute('href'), true).catch(function () { location.href = a.href; });
  });
  window.addEventListener('popstate', function () {
    loadCalendar(location.pathname + location.search, false).catch(function () { location.reload(); });
  });
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init); else init();
})();
