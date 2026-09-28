// OBIE console (ADR 0019, ADR 0020): keeps the health indicator of every
// page and the regions a view marks with data-refresh current, and tells
// the operator when the session ended or the console can no longer be
// reached, e.g. after a configuration reload. Every page works without
// this script. It builds its own text with textContent only; a region's
// new content is the console's escaped template output, parsed into an
// inert document that runs no script.
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
  var regions = Array.prototype.slice.call(document.querySelectorAll('[data-refresh]'));
  var liveHints = Array.prototype.slice.call(document.querySelectorAll('[data-live]'));
  var timer = 0;
  var signedOut = false;

  function showLiveHints(shown) {
    liveHints.forEach(function (hint) {
      hint.hidden = !shown;
    });
  }

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
    showLiveHints(false);
    showBanner(
      'You were signed out: the console token was replaced (obiectl console --rotate) or obied restarted.',
      { href: '/login?next=' + encodeURIComponent(location.pathname + location.search), text: 'Sign in again' });
  }

  // answered reports whether resp succeeded, and shows why when it did not.
  function answered(resp) {
    if (resp.status === 401) {
      signedOutBanner();
      return false;
    }
    if (!resp.ok) {
      showBanner('The console answered HTTP ' + resp.status + '. The obied log says why; this page keeps trying.');
      return false;
    }
    return true;
  }

  // withoutUpdateTime returns the markup of root without its update time,
  // which changes with every refresh.
  function withoutUpdateTime(root) {
    var copy = root.cloneNode(true);
    Array.prototype.forEach.call(copy.querySelectorAll('[data-updated]'), function (el) {
      el.remove();
    });
    return copy.innerHTML.trim();
  }

  // focusedLink returns the href of the focused link inside region, or null.
  function focusedLink(region) {
    var el = document.activeElement;
    return el && region.contains(el) && el.hasAttribute('href') ? el.getAttribute('href') : null;
  }

  function focusLink(region, href) {
    var links = region.querySelectorAll('a[href]');
    for (var i = 0; i < links.length; i++) {
      if (links[i].getAttribute('href') === href) {
        links[i].focus();
        return;
      }
    }
  }

  // swap shows the fragment html in region: all of it when more than the
  // update time changed, keeping the focused link focused, else only the
  // update time.
  function swap(region, html) {
    var incoming = new DOMParser().parseFromString(html, 'text/html').body;
    var time = region.querySelector('[data-updated]');
    var newTime = incoming.querySelector('[data-updated]');
    if (time && newTime && withoutUpdateTime(region) === withoutUpdateTime(incoming)) {
      time.replaceChildren.apply(time, Array.prototype.slice.call(newTime.childNodes));
      return;
    }
    var focused = focusedLink(region);
    region.replaceChildren.apply(region, Array.prototype.slice.call(incoming.childNodes));
    if (focused !== null) {
      focusLink(region, focused);
    }
  }

  // refresh brings region up to date from its fragment and resolves to
  // whether it did.
  function refresh(region) {
    return fetch(region.getAttribute('data-refresh'), { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'text/html' } })
      .then(function (resp) {
        if (!answered(resp)) {
          return false;
        }
        return resp.text().then(function (html) {
          swap(region, html);
          return true;
        });
      });
  }

  function poll() {
    timer = 0;
    fetch('/api/health', { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } })
      .then(function (resp) {
        if (!answered(resp)) {
          return;
        }
        return resp.json()
          .then(function (h) {
            update(h);
            return Promise.all(regions.map(refresh));
          })
          .then(function (refreshed) {
            if (refreshed.every(Boolean)) {
              hideBanner();
            }
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
  showLiveHints(true);
  schedule();
})();
