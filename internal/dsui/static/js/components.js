// Web Components for the Datastar UI.
// Uses light DOM (no shadow DOM) for Datastar SSE compatibility.
// These enhance server-rendered HTML with client-side behavior.

class EntryRowElement extends HTMLElement {
  constructor() {
    super();
  }

  connectedCallback() {
    // The server renders the full HTML as children; we just wire up events.
    this._wireStarButton();
    this._wireClick();
  }

  _wireStarButton() {
    const starBtn = this.querySelector(".star-btn");
    if (!starBtn) return;
    // Remove old listener by cloning.
    const clone = starBtn.cloneNode(true);
    starBtn.parentNode.replaceChild(clone, starBtn);
    clone.addEventListener("click", (e) => {
      e.stopPropagation();
      const id = this.dataset.id;
      const starred = this.dataset.starred === "true";
      // POST to toggle star endpoint. Datastar will patch the button HTML.
      fetch(`/ds/sse/entry/star/${id}`, {
        method: "POST",
        headers: {
          Accept: "text/event-stream",
          "X-Csrf-Token": document.body.dataset.csrfToken || "",
        },
      }).catch((err) => console.error("Star toggle failed:", err));
    });
  }

  _wireClick() {
    this.addEventListener("click", () => {
      const id = this.dataset.id;
      if (!id) return;
      // Mark row as selected.
      document.querySelectorAll("entry-row.selected").forEach((el) => {
        el.classList.remove("selected");
      });
      this.classList.add("selected");
      // Load entry content via Datastar action.
      // Use fetch with Datastar-Request header; the browser will process SSE.
      fetch(`/ds/sse/entry/${id}`, {
        method: "GET",
        headers: { Accept: "text/event-stream" },
      }).catch((err) => console.error("Entry load failed:", err));
    });
  }
}

// Define custom elements if not already registered.
if (!customElements.get("entry-row")) {
  customElements.define("entry-row", EntryRowElement);
}
