// Datastar UI keyboard handler — Google Reader-style shortcuts.
// This file is loaded as a regular script (not a module) to keep it simple.
// It listens for keydown events and manipulates the DOM / triggers Datastar actions.

(function () {
  "use strict";

  var selectedIdx = -1;
  var selectedEntryId = null; // data-id of the selected row; survives row re-renders
  var pendingGo = false; // 'g' pressed, awaiting second key
  var pendingZ = false; // 'z' pressed, awaiting 't' (classic "z t")

  function inputFocused() {
    var tag = document.activeElement?.tagName;
    return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || document.activeElement?.isContentEditable;
  }

  function getVisibleEntryRows() {
    return Array.from(document.querySelectorAll("#entry-list entry-row"));
  }

  function scrollToRow(row) {
    row.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }

  function clearSelection() {
    selectedEntryId = null;
    document.querySelectorAll("#entry-list entry-row.selected").forEach(function (el) {
      el.classList.remove("selected");
    });
  }

  function selectRow(row) {
    clearSelection();
    row.classList.add("selected");
    selectedEntryId = row.getAttribute("data-id");
    scrollToRow(row);
  }

  function selectIndex(idx) {
    var rows = getVisibleEntryRows();
    if (rows.length === 0) return;
    idx = Math.max(0, Math.min(idx, rows.length - 1));
    selectedIdx = idx;
    selectRow(rows[idx]);
  }

  function clickElement(el) {
    if (!el) return;
    el.click();
  }

  // Loads the entry for the given row (triggers the row's SSE fetch).
  function loadEntry(row) {
    if (row) clickElement(row);
  }

  // Classic-UI style prev/next entry navigation within the reading pane:
  // moves the list selection and loads the entry.
  function navigateEntry(delta) {
    var rows = getVisibleEntryRows();
    if (rows.length === 0) return;
    if (selectedIdx === -1) {
      selectedIdx = 0;
    } else {
      selectedIdx = Math.max(0, Math.min(selectedIdx + delta, rows.length - 1));
    }
    selectRow(rows[selectedIdx]);
    loadEntry(rows[selectedIdx]);
  }

  function getSelectedRow() {
    var rows = getVisibleEntryRows();
    if (selectedIdx >= 0 && selectedIdx < rows.length) {
      return rows[selectedIdx];
    }
    var sel = document.querySelector("#entry-list entry-row.selected");
    if (sel) {
      selectedIdx = rows.indexOf(sel);
    }
    return sel || (rows.length > 0 ? rows[0] : null);
  }

  // Google Reader-style g-sequences: g then u/b/h/s navigates views.
  function handleGoSequence(key) {
    switch (key) {
      case "u":
        window.location.href = "/ds/unread";
        break;
      case "b":
        window.location.href = "/ds/starred";
        break;
      case "h":
        window.location.href = "/ds/history";
        break;
      case "s":
        window.location.href = "/ds/settings";
        break;
      case "f":
        goToFeedOfSelected();
        break;
    }
  }

  // Classic "g f": open the feed page of the selected/current entry.
  function goToFeedOfSelected() {
    var row = getSelectedRow();
    if (!row) return;
    var feedID = row.getAttribute("data-feed-id");
    if (feedID) {
      window.location.href = "/ds/feed/" + feedID;
    }
  }

  function scrollListTo(edge) {
    var list = document.getElementById("entry-list");
    if (!list) return;
    var rows = getVisibleEntryRows();
    if (edge === "top") {
      selectedIdx = 0;
      if (rows.length > 0) selectRow(rows[0]);
      list.scrollTop = 0;
    } else {
      selectedIdx = rows.length - 1;
      if (rows.length > 0) selectRow(rows[rows.length - 1]);
      list.scrollTop = list.scrollHeight;
    }
  }

  function toggleShortcutsOverlay() {
    var overlay = document.getElementById("shortcuts-overlay");
    if (overlay) overlay.hidden = !overlay.hidden;
  }

  document.addEventListener("keydown", function (e) {
    // Escape always works, even inside inputs: it blurs the search box so
    // shortcuts become available again (classic Esc closes overlays).
    if (e.key === "Escape" && !e.ctrlKey && !e.metaKey && !e.altKey) {
      var overlay = document.getElementById("shortcuts-overlay");
      if (overlay && !overlay.hidden) {
        overlay.hidden = true;
      }
      var search = document.querySelector('.top-nav input[type="search"]');
      if (search && document.activeElement === search) {
        e.preventDefault();
        search.blur();
      }
      return;
    }

    if (inputFocused()) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;

    var key = e.key;
    var row;

    // z-sequence: first 'z' arms, 't' completes, anything else cancels.
    if (pendingZ) {
      pendingZ = false;
      e.preventDefault();
      if (key === "t") {
        var selRow = getSelectedRow();
        if (selRow) scrollToRow(selRow);
      }
      return;
    }

    // g-sequence: first 'g' arms, next key completes or cancels.
    if (pendingGo) {
      pendingGo = false;
      e.preventDefault();
      if (key === "g") {
        scrollListTo("top");
      } else {
        handleGoSequence(key);
      }
      return;
    }

    switch (key) {
      case "g":
        e.preventDefault();
        pendingGo = true;
        break;

      case "G":
        e.preventDefault();
        scrollListTo("bottom");
        break;

      case "j":
        e.preventDefault();
        selectIndex(selectedIdx + 1);
        break;

      case "k":
        e.preventDefault();
        if (selectedIdx === -1) {
          selectIndex(0);
        } else {
          selectIndex(selectedIdx - 1);
        }
        break;

      case "n":
        e.preventDefault();
        navigateEntry(1);
        break;

      case "ArrowLeft":
        e.preventDefault();
        clickElement(document.querySelector('.pagination a[data-action="prev-page"]'));
        break;

      case "ArrowRight":
        e.preventDefault();
        clickElement(document.querySelector('.pagination a[data-action="next-page"]'));
        break;

      case "p":
        e.preventDefault();
        navigateEntry(-1);
        break;

      case "Enter":
      case "o":
        e.preventDefault();
        row = getSelectedRow();
        if (row) {
          clickElement(row);
        }
        break;

      case "v":
        e.preventDefault();
        var origLink = document.querySelector("#article-toolbar a[rel='noopener']");
        if (origLink) {
          window.open(origLink.href, "_blank", "noopener");
        } else {
          // Load the selected entry first, then open original link.
          row = getSelectedRow();
          if (row) {
            clickElement(row);
            // Wait briefly for SSE patch to render the toolbar, then open.
            setTimeout(function () {
              var link = document.querySelector("#article-toolbar a[rel='noopener']");
              if (link) {
                window.open(link.href, "_blank", "noopener");
              }
            }, 300);
          }
        }
        break;

      case "s":
        e.preventDefault();
        row = getSelectedRow();
        if (row) {
          var starBtn = row.querySelector(".star-btn");
          if (starBtn) {
            clickElement(starBtn);
          }
        }
        break;

      case "f":
        // Classic parity: f toggles star on the article toolbar (current entry).
        e.preventDefault();
        clickElement(document.querySelector('#article-toolbar button[data-action="toggle-star"]'));
        break;

      case "h":
        // Classic parity: h goes to previous entry.
        e.preventDefault();
        navigateEntry(-1);
        break;

      case "l":
        // Classic parity: l goes to next entry.
        e.preventDefault();
        navigateEntry(1);
        break;

      case "c":
        // Classic parity: c opens the comments link in a new tab.
        e.preventDefault();
        var commentsLink = document.querySelector('#article-toolbar a[data-action="comments-link"]');
        if (commentsLink) {
          window.open(commentsLink.href, "_blank", "noopener");
        }
        break;

      case "C":
        // Classic parity: C opens the comments link in the current tab.
        e.preventDefault();
        var commentsLink2 = document.querySelector('#article-toolbar a[data-action="comments-link"]');
        if (commentsLink2) {
          window.location.href = commentsLink2.href;
        }
        break;

      case "d":
        // Classic parity: d fetches the original content of the current entry.
        e.preventDefault();
        clickElement(document.querySelector('#article-toolbar button[data-action="fetch-content"]'));
        break;

      case "a":
        // Classic parity: a toggles the enclosures section.
        e.preventDefault();
        var enclosures = document.querySelector("#entry-content details.entry-enclosures");
        if (enclosures) {
          enclosures.open = !enclosures.open;
        }
        break;

      case "R":
        // Classic parity: R refreshes all feeds.
        e.preventDefault();
        clickElement(document.querySelector('button[data-action="refresh-all"]'));
        break;

      case "z":
        // Classic parity: z t scrolls the selected item into view. Arm like 'g'.
        e.preventDefault();
        pendingZ = true;
        break;

      case "m":
        e.preventDefault();
        // Click the status-toggle (Read/Unread) button in the article toolbar.
        // It is identified by the stable data-action marker rather than by its
        // label, which switches between "Read" and "Unread".
        var statusBtn = document.querySelector('#article-toolbar button[data-action="toggle-status"]');
        if (statusBtn) {
          clickElement(statusBtn);
        }
        break;

      case "A":
        e.preventDefault();
        var markAllBtn = document.querySelector(".mark-read-btn");
        if (markAllBtn) {
          clickElement(markAllBtn);
        }
        break;

      case "r":
        e.preventDefault();
        window.location.reload();
        break;

      case "/":
        e.preventDefault();
        var search = document.querySelector('.top-nav input[type="search"]');
        if (search) search.focus();
        break;

      case "?":
        e.preventDefault();
        toggleShortcutsOverlay();
        break;

    }
  });

  document.addEventListener("DOMContentLoaded", function () {
    var rows = getVisibleEntryRows();
    if (rows.length > 0) {
      selectIndex(0);
    }

    // Mobile nav panel toggling.
    var nav = document.getElementById("mobile-nav");
    if (nav) {
      nav.querySelectorAll("button").forEach(function (btn) {
        btn.addEventListener("click", function () {
          var panel = this.dataset.panel;
          var container = document.querySelector(".app-container");
          if (container) {
            container.setAttribute("data-active-panel", panel);
          }
          nav.querySelectorAll("button").forEach(function (b) {
            b.classList.remove("active");
          });
          this.classList.add("active");
        });
      });
    }

    // On mobile, switch to content panel when an entry loads via SSE.
    var entryContent = document.getElementById("entry-content");
    if (entryContent) {
      // Only auto-switch when the content panel is actually displaying an
      // entry (i.e. an article toolbar is rendered). This avoids yanking the
      // mobile user to the content panel on unrelated changes, and when
      // they've navigated to a different panel (e.g. Feeds).
      // Datastar patches MORPH the DOM in place: entry changes usually show
      // up as characterData/attribute mutations, not childList, so subscribe
      // to all three (the callback re-checks current state, so extra fires
      // are harmless).
      // Remember the title the pane shows so we only switch panels when a
      // NEW entry loads (the server pre-renders the first entry into the
      // pane; its toolbar/styling mutations must not yank the user away
      // from the list panel on load).
      var lastShownTitle = (entryContent.querySelector(".article-title") || {}).textContent || null;
      var observer = new MutationObserver(function () {
        if (window.innerWidth > 768) return;
        var container = document.querySelector(".app-container");
        if (!container || container.getAttribute("data-active-panel") === "content") return;
        var titleEl = entryContent.querySelector(".article-title");
        if (!titleEl) return;
        var title = titleEl.textContent;
        if (title === lastShownTitle) return;
        lastShownTitle = title;
        container.setAttribute("data-active-panel", "content");
        if (nav) {
          nav.querySelectorAll("button").forEach(function (b) {
            b.classList.remove("active");
          });
          var contentBtn = nav.querySelector('button[data-panel="content"]');
          if (contentBtn) contentBtn.classList.add("active");
        }
      });
      observer.observe(entryContent, { childList: true, subtree: true, characterData: true, attributes: true });

      // The observer is only needed on mobile; disconnect it on desktop so it
      // stops firing on every content/panel change, and re-connect when back
      // to a mobile viewport.
      var syncObserver = function () {
        if (window.innerWidth > 768) {
          observer.disconnect();
        } else if (document.getElementById(entryContent.id)) {
          observer.observe(entryContent, { childList: true, subtree: true, characterData: true, attributes: true });
        }
      };
      window.addEventListener("resize", syncObserver);
    }

    // Reset keyboard selection whenever the entry list is re-rendered via SSE.
    // A re-render replaces the entry-row nodes (dropping any .selected class),
    // so a stale index would otherwise point at the wrong row.
    var entryList = document.getElementById("entry-list");
    if (entryList) {
      var listObserver = new MutationObserver(function () {
        // Row nodes are replaced by SSE patches (e.g. sseEntry re-renders the
        // opened row). The replacement drops the .selected class, so restore
        // it from the tracked entry id to keep keyboard selection stable.
        var current = entryList.querySelector("entry-row.selected");
        if (current && current.getAttribute("data-id") === selectedEntryId) {
          selectedIdx = getVisibleEntryRows().indexOf(current);
          return;
        }
        if (selectedEntryId !== null) {
          var byId = entryList.querySelector('entry-row[data-id="' + selectedEntryId + '"]');
          if (byId) {
            clearSelection();
            byId.classList.add("selected");
            selectedIdx = getVisibleEntryRows().indexOf(byId);
            return;
          }
        }
        selectedIdx = -1;
        var sel = entryList.querySelector("entry-row.selected");
        if (sel) {
          var rows = getVisibleEntryRows();
          selectedIdx = rows.indexOf(sel);
        } else {
          // List fully re-rendered (e.g. page change): select the first row
          // so j/k continue from a sane position, like classic.
          var firstRows = getVisibleEntryRows();
          if (firstRows.length > 0) selectIndex(0);
        }
      });
      // Datastar patches MORPH the DOM in place: full-list refreshes
      // (pagination, mark-page-read) often mutate row text/attributes rather
      // than replacing nodes, so a childList-only observer never fires. With
      // no .selected row surviving a morph, selection is silently lost —
      // fall back to the first row (classic resets to the top item).
      listObserver.observe(entryList, { childList: true, subtree: true, characterData: true, attributes: true });
    }

    // Settings page scroll spy for sidebar nav
    var settingsNav = document.querySelector('.settings-nav');
    if (settingsNav) {
      var links = settingsNav.querySelectorAll('a');
      var sections = document.querySelectorAll('.settings-section');
      var content = document.getElementById('settings-content');
      if (content) {
        links.forEach(function(link) {
          link.addEventListener('click', function(e) {
            e.preventDefault();
            var target = document.querySelector(this.getAttribute('href'));
            if (target) target.scrollIntoView({behavior:'smooth', block:'start'});
            links.forEach(function(l) { l.classList.remove('active'); });
            this.classList.add('active');
          });
        });
        content.addEventListener('scroll', function() {
          var scrollPos = content.scrollTop + 60;
          sections.forEach(function(section, i) {
            if (section.offsetTop <= scrollPos) {
              links.forEach(function(l) { l.classList.remove('active'); });
              if (links[i]) links[i].classList.add('active');
            }
          });
        });
      }
    }
  });
})();

// Panel resize — drag handle between entry list and content panels
(function(){
    var h = document.getElementById('panel-resize-handle');
    if (!h) return;
    var c = document.querySelector('.app-container');
    var sx, sw;
    h.onmousedown = function(e) {
        e.preventDefault();
        sx = e.clientX;
        sw = document.getElementById('entry-list-panel').offsetWidth;
        h.classList.add('active');
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
    };
    window.addEventListener('mousemove', function(e) {
        if (!sx) return;
        var w = Math.max(260, Math.min(800, sw + e.clientX - sx));
        c.style.gridTemplateColumns = c.style.gridTemplateColumns.replace(/1fr 4px 1fr/, w + 'px 4px 1fr');
        try { localStorage.setItem('dsui-entryListWidth', w); } catch(_) {}
    });
    window.addEventListener('mouseup', function() {
        sx = null; h.classList.remove('active');
        document.body.style.cursor = ''; document.body.style.userSelect = '';
    });
    try {
        var w = parseInt(localStorage.getItem('dsui-entryListWidth'));
        if (w && w >= 260 && w <= 800)
            c.style.gridTemplateColumns = c.style.gridTemplateColumns.replace(/1fr 4px 1fr/, w + 'px 4px 1fr');
    } catch(_) {}
})();
