// OBIE console (ADR 0019): keeps the health indicator of every page
// current and tells the operator when the session ended or the console
// can no longer be reached, e.g. after a configuration reload. Every page
// works without this script. It builds text with textContent only.
(function () {
  'use strict';

  var INTERVAL_MS = 5000;
  var MODE_LABELS = { observe: 'Observe', enforce: 'Enforce' };

  var health = document.querySelector('[data-health]');
  if (!health) {
    return; // the sign-in page
  }
  var label = health.querySelector('[data-health-label]');
  var problems = document.querySelector('[data-health-problems]');
  var mode = document.querySelector('[data-mode]');
  var modeLabel = document.querySelector('[data-mode-label]');
  var banner = document.querySelector('[data-banner]');
  var timer = 0;
  var signedOut = false;

  function showBanner(text, link) {
    var p = document.createElement('p');
    p.textContent = text;
    var children = [p];
    if (link) {
      var a = document.createElement('a');
      a.href = link.href;
      a.textContent = link.text;
      children.push(a);
    }
    banner.replaceChildren.apply(banner, children);
    banner.hidden = false;
  }

  function hideBanner() {
    banner.hidden = true;
    banner.replaceChildren();
  }

  function update(h) {
    health.setAttribute('data-state', h.state);
    label.textContent = h.label;
    var items = (h.problems || []).map(function (text) {
      var li = document.createElement('li');
      li.textContent = text;
      return li;
    });
    var list = problems.querySelector('ul');
    list.replaceChildren.apply(list, items);
    problems.hidden = items.length === 0;
    mode.setAttribute('data-mode', h.mode);
    modeLabel.textContent = MODE_LABELS[h.mode] || h.mode;
  }

  function signedOutBanner() {
    signedOut = true;
    showBanner(
      'You were signed out: the console token was replaced (obiectl console --rotate) or obied restarted.',
      { href: '/login?next=' + encodeURIComponent(location.pathname + location.search), text: 'Sign in again' });
  }

  function poll() {
    timer = 0;
    fetch('/api/health', { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } })
      .then(function (resp) {
        if (resp.status === 401) {
          signedOutBanner();
          return;
        }
        if (!resp.ok) {
          showBanner('The console answered HTTP ' + resp.status + '. The obied log says why; this page keeps trying.');
          return;
        }
        return resp.json().then(function (h) {
          update(h);
          hideBanner();
        });
      })
      .catch(function () {
        showBanner('The console is not reachable. A configuration reload switched it off or moved it to another ' +
          'address, or obied is stopping. This page keeps trying; on the node, obiectl console shows where the ' +
          'console is.');
      })
      .then(schedule);
  }

  function schedule() {
    if (!signedOut && !document.hidden && !timer) {
      timer = window.setTimeout(poll, INTERVAL_MS);
    }
  }

  document.addEventListener('visibilitychange', function () {
    if (document.hidden) {
      window.clearTimeout(timer);
      timer = 0;
    } else {
      schedule();
    }
  });
  schedule();
})();
