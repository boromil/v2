// Web Components for the Datastar UI.
// Uses light DOM (no shadow DOM) for Datastar SSE compatibility.
// These enhance server-rendered HTML with client-side behavior.

class EntryRowElement extends HTMLElement {
  connectedCallback() {
    this.addEventListener("click", (e) => {
      // Don't select if clicking the star button (handled by Datastar).
      if (e.target.closest(".star-btn")) return;

      document.querySelectorAll("entry-row.selected").forEach((el) => {
        el.classList.remove("selected");
      });
      this.classList.add("selected");
    });
  }
}

if (!customElements.get("entry-row")) {
  customElements.define("entry-row", EntryRowElement);
}

// Enclosure media controls: seek buttons, playback speed, and progression
// persistence via the classic save-progression route. Works for both audio
// and video elements rendered by the entry_content template.
(function () {
  "use strict";

  function csrfToken() {
    var el = document.querySelector("[data-csrf-token]");
    return el ? el.getAttribute("data-csrf-token") : "";
  }

  function mediaElementForControls(controls) {
    var enclosure = controls.closest(".enclosure-media");
    return enclosure ? enclosure.querySelector("audio, video") : null;
  }

  function saveProgression(el) {
    var url = el.getAttribute("data-save-url");
    if (!url) return;
    fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Csrf-Token": csrfToken(),
      },
      body: JSON.stringify({ progression: Math.floor(el.currentTime) }),
    }).catch(function () {});
  }

  function initControls(controls) {
    if (controls.dataset.initialized) return;
    controls.dataset.initialized = "1";

    var media = mediaElementForControls(controls);
    if (!media) return;

    var indicator = controls.querySelector("[data-speed-indicator-for]");

    controls.querySelectorAll("[data-seek]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        media.currentTime = Math.max(
          0,
          Math.min(media.duration || Infinity, media.currentTime + parseFloat(btn.dataset.seek))
        );
      });
    });

    controls.querySelectorAll("[data-speed]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        media.playbackRate = Math.max(
          0.25,
          Math.min(4, media.playbackRate + parseFloat(btn.dataset.speed))
        );
        if (indicator) {
          indicator.textContent = media.playbackRate.toFixed(2) + "x";
        }
      });
    });

    // Restore last position once metadata is available.
    var last = parseInt(media.getAttribute("data-last-position"), 10) || 0;
    if (last > 0) {
      media.addEventListener("loadedmetadata", function () {
        if (last < (media.duration || Infinity) - 5) {
          media.currentTime = last;
        }
      });
    }

    // Persist progression on pause and periodically while playing.
    var lastSave = 0;
    media.addEventListener("pause", function () { saveProgression(media); });
    media.addEventListener("timeupdate", function () {
      var now = Date.now();
      if (now - lastSave > 10000) {
        lastSave = now;
        saveProgression(media);
      }
    });
  }

  function initAll(root) {
    (root || document).querySelectorAll(".media-controls").forEach(initControls);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { initAll(); });
  } else {
    initAll();
  }

  // Re-init when SSE patches swap in new entry content.
  var content = document.getElementById("entry-content");
  if (content) {
    new MutationObserver(function () { initAll(content); }).observe(content, {
      childList: true,
      subtree: true,
    });
  }
})();

// Toasts dismiss themselves after a few seconds. Registered first because the
// toast markup exists on every page, while the reading pane does not.
(function () {
  "use strict";

  ["toast-error", "toast-success", "toast-info"].forEach(function (id) {
    var toast = document.getElementById(id);
    if (!toast) return;
    var timer = null;
    new MutationObserver(function () {
      clearTimeout(timer);
      if (toast.style.display === "none") return;
      timer = setTimeout(function () { toast.click(); }, 4000);
    }).observe(toast, { attributes: true, attributeFilter: ["style"] });
  });
})();

// Reading-pane behavior: loading feedback while an entry is fetched and
// scrolling the new article back to the top.
(function () {
  "use strict";

  var pane = document.getElementById("entry-content");
  var panel = document.getElementById("entry-content-panel");
  if (!pane) return;

  var lastTitle = (pane.querySelector(".article-title") || {}).textContent || "";
  var loadTimer = null;

  function startLoading() {
    if (!panel) return;
    panel.classList.add("is-loading");
    clearTimeout(loadTimer);
    // Safety net: never leave the bar running if the request fails.
    loadTimer = setTimeout(stopLoading, 8000);
  }

  function stopLoading() {
    clearTimeout(loadTimer);
    if (panel) panel.classList.remove("is-loading");
  }

  document.addEventListener("click", function (event) {
    var target = event.target;
    if (!target || !target.closest) return;
    var row = target.closest("entry-row");
    if (!row) return;
    // Starring does not replace the reading pane.
    if (target.closest(".star-btn")) return;
    startLoading();
  });

  new MutationObserver(function () {
    var titleEl = pane.querySelector(".article-title");
    var title = titleEl ? titleEl.textContent : "";
    if (title === lastTitle) return;
    lastTitle = title;
    stopLoading();
    pane.scrollTop = 0;
  }).observe(pane, { childList: true, subtree: true, characterData: true });
})();

// Share link: clicking copies the public URL and confirms with a toast.
(function () {
  "use strict";

  // A copy confirmation must not land on top of a refresh or import toast, so
  // it steps up when one of those is already visible.
  function otherToastVisible() {
    return ["toast-error", "toast-success", "toast-info"].some(function (id) {
      var el = document.getElementById(id);
      return el && getComputedStyle(el).display !== "none";
    });
  }

  function showToast(message) {
    if (!message) return;
    document.querySelectorAll(".toast.share-toast").forEach(function (toast) {
      toast.remove();
    });
    var toast = document.createElement("div");
    toast.className = "toast toast-info share-toast";
    if (otherToastVisible()) toast.classList.add("is-stacked");
    toast.setAttribute("role", "status");
    toast.textContent = message;
    document.body.appendChild(toast);
    setTimeout(function () { toast.remove(); }, 2500);
  }

  // execCommand fallback: works on insecure origins and when the async
  // Clipboard API is blocked, as long as the click is a user gesture.
  function legacyCopy(text) {
    var field = document.createElement("textarea");
    field.value = text;
    field.setAttribute("readonly", "");
    field.style.position = "fixed";
    field.style.top = "-1000px";
    field.style.opacity = "0";
    document.body.appendChild(field);
    field.select();
    var copied = false;
    try {
      copied = document.execCommand("copy");
    } catch (error) {
      copied = false;
    }
    field.remove();
    return copied;
  }

  // Resolves to true when the URL reached the clipboard. The async API rejects
  // on insecure origins and in some embedded browsers, hence the fallback.
  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text).then(
        function () { return true; },
        function () { return legacyCopy(text); }
      );
    }
    return Promise.resolve(legacyCopy(text));
  }

  document.addEventListener("click", function (event) {
    var link = event.target && event.target.closest && event.target.closest(".share-link");
    if (!link) return;

    var url = link.getAttribute("data-share-url") || link.href;
    var label = link.getAttribute("data-copied-label");

    event.preventDefault();

    copyText(url).then(function (copied) {
      if (copied) {
        showToast(label);
        return;
      }
      // Last resort: open the public page, where the URL sits in the address
      // bar. window.open would be blocked here because the user activation is
      // spent by the time the clipboard promise rejects.
      window.location.assign(url);
    });
  });
})();

// Settings > Feeds: filter the feed list in the browser. The haystack comes
// from the server (title, host, feed URL, category) so no row text has to be
// scraped and the "Remove" label never matches.
//
// Everything is looked up on demand and the listeners live on document: a
// language change patches .settings-page inner HTML, which replaces these
// nodes, so any reference captured at load would go stale.
(function () {
  "use strict";

  function scopeOf(node) {
    return node.closest(".settings-page") || document;
  }

  function applyFilter(input) {
    var scope = scopeOf(input);
    var errorsOnly = scope.querySelector("[data-feed-filter-errors]");
    var countEl = scope.querySelector("[data-feed-filter-count]");
    var emptyEl = scope.querySelector("[data-feed-empty]");
    var list = scope.querySelector("[data-feed-list]");
    var rows = scope.querySelectorAll("[data-feed-row]");
    var terms = input.value.trim().toLowerCase().split(/\s+/).filter(Boolean);
    var onlyErrors = !!(errorsOnly && errorsOnly.checked);
    var visible = 0;

    Array.prototype.forEach.call(rows, function (row) {
      var haystack = row.getAttribute("data-feed-search") || "";
      var matches = terms.every(function (term) {
        return haystack.indexOf(term) !== -1;
      });
      if (matches && onlyErrors) matches = row.hasAttribute("data-feed-error");
      row.hidden = !matches;
      if (matches) visible++;
    });

    var filtering = terms.length > 0 || onlyErrors;
    if (countEl) {
      countEl.hidden = !filtering;
      countEl.textContent = visible + " / " + rows.length;
    }
    // An empty bordered list would leave a 2px bar behind the empty state.
    if (list) list.hidden = visible === 0;
    if (emptyEl) emptyEl.hidden = !filtering || visible > 0;
  }

  document.addEventListener("input", function (event) {
    var input = event.target;
    if (input && input.matches && input.matches("[data-feed-filter]")) {
      applyFilter(input);
    }
  });

  document.addEventListener("change", function (event) {
    var toggle = event.target;
    if (!toggle || !toggle.matches || !toggle.matches("[data-feed-filter-errors]")) return;
    var input = scopeOf(toggle).querySelector("[data-feed-filter]");
    if (input) applyFilter(input);
  });

  document.addEventListener("keydown", function (event) {
    var input = event.target;
    if (event.key === "Escape" && input && input.matches && input.matches("[data-feed-filter]") && input.value !== "") {
      input.value = "";
      applyFilter(input);
      event.stopPropagation();
    }
  });

  document.querySelectorAll("[data-feed-filter]").forEach(applyFilter);
})();

// Destructive settings actions ask once, in place, instead of submitting on the
// first click. Without JS the form still submits, so nothing is lost.
(function () {
  "use strict";

  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form || !form.hasAttribute || !form.hasAttribute("data-confirm")) return;
    if (form.getAttribute("data-confirmed") === "1") return;

    event.preventDefault();

    var submitButton = form.querySelector("button[type='submit']");
    var row = form.closest(".feed-row");

    var bar = document.createElement("div");
    bar.className = "confirm-bar";
    bar.setAttribute("role", "alertdialog");

    var question = document.createElement("span");
    question.className = "confirm-bar-text";
    question.textContent = form.getAttribute("data-confirm") || "";
    bar.setAttribute("aria-label", question.textContent);

    var confirmButton = document.createElement("button");
    confirmButton.type = "button";
    confirmButton.className = "toolbar-btn confirm-remove";
    confirmButton.textContent = submitButton ? submitButton.textContent.trim() : "OK";

    var cancelButton = document.createElement("button");
    cancelButton.type = "button";
    cancelButton.className = "toolbar-btn";
    cancelButton.textContent = form.getAttribute("data-cancel") || "Cancel";

    function dismiss() {
      bar.remove();
      if (submitButton) {
        submitButton.style.display = "";
        submitButton.focus();
      }
      if (row) row.classList.remove("is-confirming");
    }

    confirmButton.addEventListener("click", function () {
      form.setAttribute("data-confirmed", "1");
      form.submit();
    });
    cancelButton.addEventListener("click", dismiss);

    bar.appendChild(question);
    bar.appendChild(confirmButton);
    bar.appendChild(cancelButton);
    form.appendChild(bar);
    if (submitButton) submitButton.style.display = "none";
    if (row) row.classList.add("is-confirming");
    confirmButton.focus();
  });

  document.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    var bar = document.querySelector(".confirm-bar");
    if (!bar) return;
    var cancelButton = bar.querySelector("button:last-child");
    if (cancelButton) cancelButton.click();
    event.stopPropagation();
  });
})();

// Settings > Categories: "Update" only lights up once the name actually
// changed, so the row reads as a label with an action rather than a form.
// Delegated for the same reason as the feed filter: the settings fragment is
// replaced on a language change.
(function () {
  "use strict";

  function sync(form) {
    var input = form.querySelector("input[name='title']");
    var button = form.querySelector("button[type='submit']");
    if (!input || !button) return;
    var initial = input.getAttribute("data-initial");
    if (initial === null) initial = input.value;
    button.disabled = input.value.trim() === initial.trim();
  }

  // A fragment patched in after a language change starts out unsynced, so the
  // state is refreshed on first focus as well as on typing.
  document.addEventListener("focusin", function (event) {
    var input = event.target;
    if (!input || !input.matches || !input.matches("form.category-rename input[name='title']")) return;
    sync(input.form);
  });

  document.addEventListener("input", function (event) {
    var input = event.target;
    if (!input || !input.matches || !input.matches("form.category-rename input[name='title']")) return;
    sync(input.form);
  });

  document.querySelectorAll("form.category-rename").forEach(sync);
})();
