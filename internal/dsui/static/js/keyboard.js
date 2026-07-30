// Datastar UI keyboard handler — Google Reader-style shortcuts.
// This file is loaded as a regular script (not a module) to keep it simple.
// It listens for keydown events and manipulates the DOM / triggers Datastar actions.

(function () {
  "use strict";

  var selectedIdx = -1;

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
    document.querySelectorAll("#entry-list entry-row.selected").forEach(function (el) {
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

  function clickElement(el) {
    if (!el) return;
    el.click();
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

  document.addEventListener("keydown", function (e) {
    if (inputFocused()) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;

    var key = e.key;
    var row;

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

      case "m":
        e.preventDefault();
        // Click the toggle read button in the article toolbar.
        var toolbarBtns = document.querySelectorAll("#article-toolbar .toolbar-btn");
        for (var bi = 0; bi < toolbarBtns.length; bi++) {
          if (toolbarBtns[bi].textContent.indexOf("Mark") !== -1) {
            clickElement(toolbarBtns[bi]);
            break;
          }
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
