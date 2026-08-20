# Datastar UI (dsui) — Audit vs Classic UI and Implementation Plan

Scope: `internal/dsui` compared against `internal/ui` (classic). Audit performed on branch
`feat/datastar-ui` at commit `ea025fa4`, verified against a live server (SQLite, 2 feeds,
35 entries) plus `go build` / `go vet` / `go test ./internal/dsui/...` (all green).

## A. Confirmed defects (live-reproduced or code-verified)

| # | Defect | Evidence | Severity |
|---|--------|----------|----------|
| D1 | **Stale view signal on nav clicks after search.** Clicking a feed/category link (`@get('/ds/sse/entries?feedId=2')`) after a search leaves `$view='search'` and `$searchQuery` in the signals; the handler merges URL params over signals, so the user gets stale search results instead of the feed list. Reproduced: feed click after searching "Go" returned 12 search hits instead of 105 feed entries. | `sseEntries` merge order (`handler.go` ~L495); `feed_node.html` `data-on:click="@get('{{.SSEURL}}')"` doesn't reset signals | High |
| D2 | **Empty `<title>`** on all app pages (`/ds/unread`, `/ds/starred`, ...). Settings page sets one. | `curl` shows `<title></title>`; `appViewModel.Title` never set; `ListTitle` exists but only rendered in nav | Low |
| D3 | **Media proxy never applied** to entry content. Classic always runs `proxyFilter` (`mediaproxy.RewriteDocumentWithRelativeProxyURL`) which self-gates on `MEDIA_PROXY_MODE`; dsui renders raw `entry.Content` so images leak client IPs / hotlink origin. | `sseEntry` (`handler.go` ~L602) vs classic `entry.html:285` | High |
| D4 | **Entry sorting prefs ignored.** `queryEntries` hardcodes `WithSorting("published_at", "desc")` + `("id","desc")`; classic uses `user.EntryOrder`/`user.EntryDirection` in every list handler. dsui settings page offers the two selects, so the UI lies. | `queryEntries` (~L1140) vs `unread_entries.go:26-28` | High |
| D5 | **Offset overflow shows empty list.** `offset >= total` yields 0 rows; classic resets offset to 0 and re-queries. Reproduced with `offset=10000`. | `sseEntries` lacks the `offset >= count && count > 0` reset from `unread_entries.go:38-53` | Medium |
| D6 | **User theme ignored** — settings select exists, but layout always uses system `color-scheme: dark light`; classic ships light/dark/serif/sans-serif/system stylesheets. | `layout.html:3`, settings form saves but nothing consumes | Medium |
| D7 | ~~Timezone ignored~~ **Verified non-defect:** the storage `EntryQueryBuilder` joins `users.timezone` and converts `entry.Date`/`CreatedAt` to the user's timezone with location info (live-verified: 00:00 UTC rendered 09:00 for Asia/Tokyo), and `time.Since` is location-independent. Only the *strings* in `elapsedTime` are hardcoded English → tracked under D8. | `entry_query_builder.go:611-614` | — |
| D8 | **No i18n.** All UI strings hardcoded English ("Search...", "Mark page", "Select an entry to read.", elapsedTime units). Classic: 23 locales via `locale.Printer`; `layout.html` already passes `.Language`. | `app.html`, `entry_content.html`, `elapsedTime` | Medium |

Not defects (verified non-gaps): `dir="ltr"` hardcode matches classic (classic has no `dir` attribute at all); `MarkReadOnView` is respected; login/CSRF/star/status/share/refresh/fetch-content flows all work; feed icons are rendered in the tree.

## B. Parity gaps (missing features)

| # | Gap | Classic reference |
|---|-----|-------------------|
| P1 | Reading time not shown (list meta + entry header); `user.ShowReadingTime` pref saved but unused | `entry.ReadingTime`, `item_meta.html` |
| P2 | Entry tags never queried (`WithTags`) nor rendered | `entry.html:167+` (with 5-tag limit + overflow) |
| P3 | `CommentsURL` not exposed/rendered | `entry.html:128-130` |
| P4 | Enclosures: audio-only rendering; no video/image handling, no download filename, no media controls (seek ±10/±30s, speed ±0.25x with persistence) | `entry.html:25-45,246-280`, `app.js` enclosure handlers |
| P5 | No prev/next entry navigation in reader (`NewEntryPaginationBuilder`, keyboard `h`/`l` + toolbar buttons) | `entry_feed.go:56`, `entry_category.go:56`, `entry_unread.go` |
| P6 | Keyboard shortcuts: dsui has j/k/Enter/o/v/s/m/A/r only. Classic adds `g u|b|h|f|c|s`, `g g`/`G`, `/`, `p`/`n`, `h`/`l`, `c`/`C`, `d`, `f`, `F`, `R`, `+`, `#`, `?` help dialog, `z t`, `a` (toggle enclosures) | `app.js:1180-1226` |
| P7 | No keyboard-shortcuts help overlay | `keyboard_shortcuts` dialog |
| P8 | Search: no unread-only toggle | `search.go` `unread` param |
| P9 | No pagination keyboard nav (ArrowLeft/Right); no `z t` scroll-to-item | `app.js:1191-1196,1199` |
| P10 | Entry list rows don't show reading time / feed source in meta line (only date) | `item_meta.html` |

## C. Staged implementation plan

Each stage lands as separate Conventional Commits, one logical change each, with tests and
(fuzzing where harness exists — `helpers_fuzz_test.go` pattern).

### Stage 1 — Correctness fixes (D1–D5) ✅ DONE (commits 782bf8b5, 67ee0b45, b5538009, a143d28e, a0c28e01; all live-verified)
1. **D1 stale view:** nav `data-on:click` expressions set `$view`/`$feedId`/`$categoryId`/`$searchQuery=''` before `@get`. Belt-and-braces: `sseEntries` treats explicit `feedId`/`categoryId` URL params as view selectors (`feed`/`category`) when `view` param absent.
2. **D2 title:** set `appViewModel.Title = ListTitle + " — Miniflux"`.
3. **D3 media proxy:** apply `mediaproxy.RewriteDocumentWithRelativeProxyURL(entry.Content)` in `sseEntry` (and fetch-content path), matching classic `proxyFilter`.
4. **D4 sorting:** `queryEntries` takes `*model.User`, uses `user.EntryOrder`/`user.EntryDirection` with `WithSorting("id", user.EntryDirection)` tiebreaker.
5. **D5 offset reset:** replicate classic's `offset >= total && total > 0 → offset = 0` re-query in `sseEntries` (and page handler).

### Stage 2 — Preferences honored (D6–D8)
6. **Theme:** ship theme CSS (light/dark/serif/sans-serif/system) mapped from `user.Theme`; `data-theme` attribute + stylesheet variants. Keep Tailwind tokens as base.
7. **Timezone:** convert dates with `timezone.Convert(user.Timezone, t)` in view models; `elapsedTime` takes tz.
8. **i18n groundwork:** wire `locale.NewPrinter(user.Language)` into dsui template funcs (`t`, `plural`, localized `elapsed`); migrate hardcoded strings (real translations only — all 23 locale files exist upstream; never English placeholders).

### Stage 3 — Feature parity (P1–P10, priority order)
9. Reading time (P1, P10): `WithTags`-style fetch; show in row meta + entry header behind `ShowReadingTime`.
10. Tags + CommentsURL (P2, P3).
11. Enclosure upgrades (P4): video/image branches, download name, media controls with speed persistence.
12. Prev/next entry (P5, P9): server route using `NewEntryPaginationBuilder`, toolbar buttons, `h`/`l` + ArrowLeft/Right keys.
13. Keyboard parity (P6, P7): `g`-sequence handler, `/` focus search, `f` star, `d` fetch, `c`/`C` comments, `R` refresh, `?` overlay, `z t`, `a`.
14. Search unread-only toggle (P8).

### Stage 4 — Reader typography polish
15. Port classic `.entry-content` rules (common.css ~1100-1230): measure/line-height scale, `img/video` max-width, figure/figcaption, blockquote, code/pre wrapping, table scroll wrapper, RTL-safe margins, `hr`, links. Verify against Go Blog + Lobsters content on live server, both themes.

## Verification protocol per stage
- `go build && go vet && go test ./internal/dsui/...` (+ new tests per change)
- Fuzz changed pure helpers where fuzz harness pattern applies
- Live server regression: login → unread → entry open (mark-on-view, media proxy URLs, title) → star → status toggle → search → feed click (D1 check: correct list) → offset overflow (D5 check) → sorting pref flip (D4 check)
- Commit after each verified logical change
