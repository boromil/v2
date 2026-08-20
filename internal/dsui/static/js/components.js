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
