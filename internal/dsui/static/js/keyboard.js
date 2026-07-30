// Datastar UI keyboard handler — Google Reader-style shortcuts.
// This file is loaded as a regular script (not a module) to keep it simple.
// It listens for keydown events and manipulates the DOM / triggers Datastar actions.

(function () {
  "use strict";

  // Keys that should only fire when no input is focused.
  const NAV_KEYS = ["j", "k", "o", "v", "s", "m", "A"];

  let selectedIdx = -1;

  function inputFocused() {
    const tag = document.activeElement?.tagName;
    return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || document.activeElement?.isContentEditable;
  }

  function getVisibleEntryRows() {
    return Array.from(document.querySelectorAll("#entry-list .entry-row"));
  }

  function scrollToRow(row) {
    row.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }

  function clearSelection() {
    document.querySelectorAll("#entry-list .entry-row.selected").forEach(function (el) {
      el.classList.remove("selected");
    });
  }

  function selectRow(row) {
    clearSelection();
    row.classList.add("selected");
    scrollToRow(row);
  }

  function selectIndex(idx) {
    var rows = getVisibleEntryRows();
    if (rows.length === 0) return;
    idx = Math.max(0, Math.min(idx, rows.length - 1));
    selectedIdx = idx;
    selectRow(rows[idx]);
  }

  // Simulate a click on an element to trigger Datastar data-on:click.
  function clickElement(el) {
    if (!el) return;
    el.click();
  }

  function getSelectedRow() {
    var rows = getVisibleEntryRows();
    if (selectedIdx >= 0 && selectedIdx < rows.length) {
      return rows[selectedIdx];
    }
    // Fallback: try .selected class.
    var sel = document.querySelector("#entry-list .entry-row.selected");
    if (sel) {
      selectedIdx = rows.indexOf(sel);
    }
    return sel || (rows.length > 0 ? rows[0] : null);
  }

  function getEntryId(row) {
    return row ? parseInt(row.dataset.id, 10) : 0;
  }

  // Trigger Datastar action by dispatching a click on an element
  // that has the correct data-on:click attribute, or by POSTing directly.
  function triggerDatastarAction(url, method) {
    method = method || "GET";
    // Datastar exposes the @get/@post functions as part of its plugin system.
    // We call the global ds object if available, or fall back to fetch.
    if (typeof window.ds !== "undefined" && window.ds.actions) {
      // Use Datastar's action dispatch if available.
      var evt = new CustomEvent("datastar-action", {
        bubbles: true,
        detail: { method: method, url: url },
      });
      document.dispatchEvent(evt);
    } else {
      // Fallback: use fetch with Datastar-Request header.
      fetch(url, {
        method: method,
        headers: {
          Accept: "text/event-stream",
          "Datastar-Request": "true",
        },
      })
        .then(function (resp) {
          if (!resp.ok) console.error("Datastar action failed:", url, resp.status);
        })
        .catch(function (err) {
          console.error("Datastar action error:", url, err);
        });
    }
  }

  // Post JSON body to Datastar endpoint.
  function postDatastarJSON(url, body) {
    fetch(url, {
      method: "POST",
      headers: {
        Accept: "text/event-stream",
        "Content-Type": "application/json",
        "Datastar-Request": "true",
      },
      body: JSON.stringify(body),
    }).catch(function (err) {
      console.error("Datastar POST error:", url, err);
    });
  }

  // Open a URL in a new tab.
  function openOriginalLink(row) {
    if (!row) return;
    // The entry row links to the entry detail; the original link is in the toolbar or entry content.
    // We need to find the open-original button.
    var linkBtn = document.querySelector(".article-toolbar a[href]");
    if (linkBtn) {
      window.open(linkBtn.href, "_blank", "noopener");
      return;
    }
    // Fallback: the entry row itself has data-on:click to load the entry.
    // After loading, we could open. For now, try to find the URL from the row.
    var entryId = getEntryId(row);
    // We'll load the entry first if not loaded, then try again.
  }

  document.addEventListener("keydown", function (e) {
    // Ignore if user is typing in an input.
    if (inputFocused()) return;

    // Ignore if modifier keys are held (except Shift for capital A).
    if (e.ctrlKey || e.metaKey || e.altKey) return;

    var key = e.key;
    var row, entryId;

    switch (key) {
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
        row = getSelectedRow();
        if (row) {
          // Click the row first to load the entry, then open original.
          // For now, try to find an existing original link.
          var origLink = document.querySelector(".article-toolbar a[rel='noopener']");
          if (origLink) {
            window.open(origLink.href, "_blank", "noopener");
          } else {
            // Load entry first by clicking the row.
            clickElement(row);
            // The entry will load via SSE. We can't easily chain this.
            // As a workaround, if the row's data has a feed URL pattern.
          }
        }
        break;

      case "s":
        e.preventDefault();
        row = getSelectedRow();
        if (row) {
          // Find the star button in this row.
          var starBtn = row.querySelector(".star-btn");
          if (starBtn) {
            clickElement(starBtn);
          }
        }
        break;

      case "m":
        e.preventDefault();
        row = getSelectedRow();
        if (row) {
          entryId = getEntryId(row);
          if (entryId) {
            postDatastarJSON("/ds/sse/entry/status", { entryIds: [entryId] });
          }
        }
        break;

      case "A":
        e.preventDefault();
        // Mark all as read.
        triggerDatastarAction("/ds/sse/mark-all-read", "POST");
        break;

      case "r":
        e.preventDefault();
        // Refresh: reload the page.
        window.location.reload();
        break;

      default:
        break;
    }
  });

  // Initialize: select first entry if on a list view.
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

    // On mobile, switch to content panel when an entry is loaded.
    // Observe #entry-content for changes (SSE patches).
    var entryContent = document.getElementById("entry-content");
    if (entryContent) {
      var observer = new MutationObserver(function () {
        if (window.innerWidth <= 768) {
          var container = document.querySelector(".app-container");
          if (container) {
            container.setAttribute("data-active-panel", "content");
          }
          if (nav) {
            nav.querySelectorAll("button").forEach(function (b) {
              b.classList.remove("active");
            });
            var contentBtn = nav.querySelector('button[data-panel="content"]');
            if (contentBtn) contentBtn.classList.add("active");
          }
        }
      });
      observer.observe(entryContent, { childList: true, subtree: true });
    }
  });
})();
