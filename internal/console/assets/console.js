// OBIE console (ADR 0019, ADR 0020): keeps the health indicator of every
// page and the regions a view marks with data-refresh current, and tells
// the operator when the session ended or the console can no longer be
// reached, e.g. after a configuration reload. It adds a Copy button to
// every element marked data-copy and a Copy link button to the page
// (ADR 0022), and follows the activity timeline's live feed (ADR 0025).
// Every page works without this script. It builds its own text with
// textContent only; a region's new content and new timeline rows are the
// console's escaped template output, parsed into an inert document that
// runs no script.
(function () {
  'use strict';

  var INTERVAL_MS = 5000;
  var LIVE_MS = 1000;
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
  var share = document.querySelector('[data-share]');
  var copyStatus = document.querySelector('[data-copy-status]');
  var timer = 0;
  var polling = false;
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

  // copyText writes text to the clipboard and resolves to whether it did.
  // Loopback origins are secure contexts, so the Clipboard API is there.
  function copyText(text) {
    if (!navigator.clipboard || !window.isSecureContext) {
      return Promise.resolve(false);
    }
    return navigator.clipboard.writeText(text).then(function () {
      return true;
    }, function () {
      return false;
    });
  }

  // selectText selects the text of el, for the operator to copy by hand.
  function selectText(el) {
    var range = document.createRange();
    range.selectNodeContents(el);
    var selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
  }

  // copied tells on the button and to screen readers what copying what
  // did; when it failed, the text in source is selected instead.
  function copied(button, what, ok, source) {
    var label = button.getAttribute('data-label');
    button.textContent = ok ? 'Copied' : 'Press Ctrl+C';
    copyStatus.textContent = ok ? 'Copied ' + what + '.' :
      'Could not copy ' + what + '. It is selected: press Ctrl+C to copy it.';
    if (!ok) {
      selectText(source);
    }
    window.setTimeout(function () {
      button.textContent = label;
    }, 2000);
  }

  function makeButton(label, name) {
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'copy-button';
    button.textContent = label;
    button.setAttribute('data-label', label);
    button.setAttribute('aria-label', name);
    return button;
  }

  // copyButton returns a button that copies the text of el: its data-copy
  // value, or else its text.
  function copyButton(el) {
    var text = el.getAttribute('data-copy') || el.textContent.trim();
    var button = makeButton('Copy', 'Copy ' + text);
    button.setAttribute('data-copy-button', text);
    button.addEventListener('click', function () {
      copyText(text).then(function (ok) {
        copied(button, text, ok, el);
      });
    });
    return button;
  }

  // addCopyButtons puts a copy button after every element marked data-copy
  // inside root, or after the heading it is in, so the heading's name
  // stays the address alone.
  function addCopyButtons(root) {
    Array.prototype.forEach.call(root.querySelectorAll('[data-copy]'), function (el) {
      var anchor = el.closest('h1, h2, h3') || el;
      var next = anchor.nextElementSibling;
      if (!next || !next.hasAttribute('data-copy-button')) {
        anchor.after(copyButton(el));
      }
    });
  }

  // addShare shows the link to this view with a button that copies it.
  // The view is its URL, so the link opens the same view for anyone who
  // may sign in on this host.
  function addShare() {
    if (!share) {
      return;
    }
    var url = share.querySelector('[data-share-url]');
    url.textContent = location.href;
    var button = makeButton('Copy link', 'Copy the link to this view');
    button.addEventListener('click', function () {
      url.textContent = location.href;
      copyText(location.href).then(function (ok) {
        copied(button, 'the link to this view', ok, url);
      });
    });
    share.appendChild(button);
    share.hidden = false;
  }

  // withoutTicks returns the markup of root without the text that changes
  // with the time alone (data-tick: the update time, the uptime) and
  // without the script's copy buttons.
  function withoutTicks(root) {
    var copy = root.cloneNode(true);
    Array.prototype.forEach.call(copy.querySelectorAll('[data-tick]'), function (el) {
      el.remove();
    });
    Array.prototype.forEach.call(copy.querySelectorAll('[data-copy-button]'), function (el) {
      el.remove();
    });
    return copy.innerHTML.trim();
  }

  // linksTo returns the links inside region that point to href.
  function linksTo(region, href) {
    return Array.prototype.filter.call(region.querySelectorAll('a[href]'), function (a) {
      return a.getAttribute('href') === href;
    });
  }

  // focusedLink returns the href of the focused link inside region and
  // which of the links to that href it is (a filter and a column heading
  // may share one), or null.
  function focusedLink(region) {
    var el = document.activeElement;
    if (!el || !region.contains(el) || !el.hasAttribute('href')) {
      return null;
    }
    var href = el.getAttribute('href');
    return { href: href, index: linksTo(region, href).indexOf(el) };
  }

  // focusLink focuses the same link again: the same occurrence of its
  // href, or the first one if there are fewer now.
  function focusLink(region, focused) {
    var links = linksTo(region, focused.href);
    var link = links[focused.index] || links[0];
    if (link) {
      link.focus({ preventScroll: true });
    }
  }

  // copyButtonsFor returns the copy buttons inside region that copy text.
  function copyButtonsFor(region, text) {
    return Array.prototype.filter.call(region.querySelectorAll('[data-copy-button]'), function (b) {
      return b.getAttribute('data-copy-button') === text;
    });
  }

  // focusedCopy returns the text the focused copy button inside region
  // copies and which of the buttons for it it is, or null.
  function focusedCopy(region) {
    var el = document.activeElement;
    if (!el || !region.contains(el) || !el.hasAttribute('data-copy-button')) {
      return null;
    }
    var text = el.getAttribute('data-copy-button');
    return { text: text, index: copyButtonsFor(region, text).indexOf(el) };
  }

  // focusCopy focuses the same copy button again, or the first one for the
  // same text.
  function focusCopy(region, focused) {
    var buttons = copyButtonsFor(region, focused.text);
    var button = buttons[focused.index] || buttons[0];
    if (button) {
      button.focus({ preventScroll: true });
    }
  }

  // swap shows the fragment html in region: all of it when more than the
  // ticking text changed, keeping the focused link focused, else only the
  // ticking text, so a selection or a screen reader's place survives.
  function swap(region, html) {
    var incoming = new DOMParser().parseFromString(html, 'text/html').body;
    if (withoutTicks(region) === withoutTicks(incoming)) {
      var ticks = region.querySelectorAll('[data-tick]');
      var newTicks = incoming.querySelectorAll('[data-tick]');
      for (var i = 0; i < ticks.length && i < newTicks.length; i++) {
        ticks[i].replaceChildren.apply(ticks[i], Array.prototype.slice.call(newTicks[i].childNodes));
      }
      return;
    }
    var focused = focusedLink(region);
    var focusedButton = focusedCopy(region);
    region.replaceChildren.apply(region, Array.prototype.slice.call(incoming.childNodes));
    addCopyButtons(region);
    if (focused !== null) {
      focusLink(region, focused);
    }
    if (focusedButton !== null) {
      focusCopy(region, focusedButton);
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
    polling = true;
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
      .then(function () {
        polling = false;
        schedule();
      });
  }

  // schedule polls once more in INTERVAL_MS unless a poll is pending or
  // running, the page is hidden or the session ended.
  function schedule() {
    if (!signedOut && !document.hidden && !timer && !polling) {
      timer = window.setTimeout(poll, INTERVAL_MS);
    }
  }

  // The activity timeline's live feed (ADR 0025): while the page is
  // visible and not paused, ask every LIVE_MS for the entries after the
  // last one and put them on top, keeping at most data-max-rows rows.
  var feed = document.querySelector('[data-live-feed]');
  var feedRows = document.querySelector('[data-activity-rows]');
  var feedStatus = document.querySelector('[data-live-status]');
  var feedToggle = document.querySelector('[data-live-toggle]');
  var liveTimer = 0;
  var liveBusy = false;
  var paused = false;
  var after = '';

  function liveURL() {
    var url = new URL(feed.getAttribute('data-live-feed'), location.href);
    url.searchParams.set('after', after);
    return url.pathname + url.search;
  }

  function clockNow() {
    return new Date().toISOString().slice(11, 19);
  }

  // trimRows takes the oldest rows off the page beyond its maximum, and
  // says so; the page's pager and notes no longer fit what remains.
  function trimRows() {
    var max = parseInt(feed.getAttribute('data-max-rows'), 10);
    if (feedRows.children.length <= max) {
      return;
    }
    while (feedRows.children.length > max) {
      feedRows.lastElementChild.remove();
    }
    document.querySelector('[data-activity-trimmed]').hidden = false;
    document.querySelector('[data-activity-tail]').hidden = true;
  }

  // addEntries puts the rows of a live answer on top of the timeline.
  function addEntries(html) {
    var incoming = new DOMParser().parseFromString(html, 'text/html');
    var next = incoming.querySelector('[data-next]');
    if (!next) {
      return;
    }
    after = next.getAttribute('data-next');
    var rows = Array.prototype.slice.call(incoming.querySelectorAll('tbody > tr'));
    if (rows.length === 0) {
      return;
    }
    rows.forEach(function (row) {
      row.classList.add('activity-new');
    });
    feedRows.prepend.apply(feedRows, rows);
    document.querySelector('[data-activity-table]').hidden = false;
    document.querySelector('[data-activity-empty]').hidden = true;
    trimRows();
    var entries = rows.filter(function (row) {
      return !row.hasAttribute('data-burst');
    }).length;
    feedStatus.textContent = 'Live: ' + (entries === 1 ? '1 new entry' : entries + ' new entries') + ' at ' +
      clockNow() + ' UTC.';
  }

  function pollLive() {
    liveTimer = 0;
    liveBusy = true;
    fetch(liveURL(), { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'text/html' } })
      .then(function (resp) {
        if (!answered(resp)) {
          return;
        }
        return resp.text().then(addEntries);
      })
      .catch(function () {
        // The health poll tells when the console cannot be reached.
      })
      .then(function () {
        liveBusy = false;
        scheduleLive(LIVE_MS);
      });
  }

  // scheduleLive asks for new entries in delay ms unless a request is
  // pending or running, the page is hidden or paused, or the session
  // ended.
  function scheduleLive(delay) {
    if (feed && !signedOut && !document.hidden && !paused && !liveTimer && !liveBusy) {
      liveTimer = window.setTimeout(pollLive, delay);
    }
  }

  function stopLive() {
    window.clearTimeout(liveTimer);
    liveTimer = 0;
  }

  function setPaused(p) {
    paused = p;
    feed.toggleAttribute('data-paused', p);
    feedToggle.textContent = p ? 'Resume live updates' : 'Pause live updates';
    if (p) {
      stopLive();
      feedStatus.textContent = 'Paused: new entries are not shown until you resume.';
    } else {
      feedStatus.textContent = 'Live: new entries appear at the top as they happen.';
      scheduleLive(0); // everything since the pause, at once
    }
  }

  function startLive() {
    if (!feed || !feedRows || !feedStatus || !feedToggle) {
      feed = null;
      return;
    }
    after = new URL(feed.getAttribute('data-live-feed'), location.href).searchParams.get('after') || '0';
    feedToggle.addEventListener('click', function () {
      setPaused(!paused);
    });
    feed.hidden = false;
    scheduleLive(LIVE_MS);
  }

  document.addEventListener('visibilitychange', function () {
    if (document.hidden) {
      window.clearTimeout(timer);
      timer = 0;
      stopLive();
    } else {
      schedule();
      scheduleLive(0);
    }
  });
  showLiveHints(true);
  addCopyButtons(document);
  addShare();
  schedule();
  startLive();
})();
