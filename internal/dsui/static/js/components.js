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
