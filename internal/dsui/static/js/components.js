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
