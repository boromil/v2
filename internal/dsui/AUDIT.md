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
| D6 | ~~User theme ignored~~ **FIXED** (dd218c93): `data-theme`/`data-font` attributes from `user.Theme`; dark-mode media queries scoped under `html:not([data-theme="light"])` + `html[data-theme="dark"]` duplicates; serif stack via `data-font="serif"`. | `layout.html:3` | — |
| D7 | ~~Timezone ignored~~ **Verified non-defect:** the storage `EntryQueryBuilder` joins `users.timezone` and converts `entry.Date`/`CreatedAt` to the user's timezone with location info (live-verified: 00:00 UTC rendered 09:00 for Asia/Tokyo), and `time.Since` is location-independent. Only the *strings* in `elapsedTime` are hardcoded English → tracked under D8. | `entry_query_builder.go:611-614` | — |
| D8 | ~~No i18n~~ **FIXED** (d9c6be83, 60690fcc, f401b373): per-language template clones (`tplFor`) bind `t`/`plural`/`elapsed` via `locale.Printer`; menu labels, list titles, empty states, toolbar, settings page, pagination, feed error counter, relative timestamps ("il y a 5 heures") all localized; parse-time stubs keep the shared template valid. | `app.html`, `entry_content.html`, `elapsedTime` | — |

Not defects (verified non-gaps): `dir="ltr"` hardcode matches classic (classic has no `dir` attribute at all); `MarkReadOnView` is respected; login/CSRF/star/status/share/refresh/fetch-content flows all work; feed icons are rendered in the tree.

## B. Parity gaps (missing features)

| # | Gap | Classic reference |
|---|-----|-------------------|
| P1 | ~~Reading time not shown~~ **DONE** (e008b3c9): entry header behind `ShowReadingTime` (list meta + entry header); `user.ShowReadingTime` pref saved but unused | `entry.ReadingTime`, `item_meta.html` |
| P2 | ~~Entry tags never queried~~ **DONE** (e008b3c9): chip list in header (`WithTags`) nor rendered | `entry.html:167+` (with 5-tag limit + overflow) |
| P3 | ~~`CommentsURL` not exposed/rendered~~ **DONE** (e008b3c9): toolbar link when present | `entry.html:128-130` |
| P4 | ~~Enclosures: audio-only~~ **DONE** (e008b3c9, f80e1e8a): typed audio/video/image rendering with Html5MimeType, download links, seek/speed media controls, and progression persistence via the classic save-progression endpoint. | `entry.html:25-45,246-280`, `app.js` enclosure handlers |
| P5 | ~~No prev/next entry navigation~~ **DONE** (f401b373): `n`/`p` move selection + load entry (`NewEntryPaginationBuilder`, keyboard `h`/`l` + toolbar buttons) | `entry_feed.go:56`, `entry_category.go:56`, `entry_unread.go` |
| P6 | Keyboard shortcuts: **DONE** core set (f401b373, e4b86093, 98d64d10, 674d93ae): j/k, Enter/o, v, s, m, A, r, n/p, ArrowLeft/Right, '/', '?'+Esc overlay, g u|b|h|s, g g/G. Still open (low value in 3-pane layout): g f|c, h/l, c/C, d, f, F, R, '+', '#', z t, a. | `app.js:1180-1226` |
| P7 | ~~No keyboard-shortcuts help overlay~~ **DONE** (98d64d10): '?' toggle + Escape close, localized | `keyboard_shortcuts` dialog |
| P8 | ~~Search: no unread-only toggle~~ **DONE** (3c90e0ec, 6a797fa1): checkbox beside search box, `searchUnreadOnly` signal → status-filtered query | `search.go` `unread` param |
| P9 | ~~No pagination keyboard nav~~ **DONE** (e4b86093): ArrowLeft/Right click the marked pagination links; `z t` remains low-priority | | `app.js:1191-1196,1199` |
| P10 | ~~Rows don't show reading time / feed source~~ **DONE** (0f9e4164): reading time added behind `ShowReadingTime`; feed name was already present | `item_meta.html` |

## C. Staged implementation plan

Each stage lands as separate Conventional Commits, one logical change each, with tests and
(fuzzing where harness exists — `helpers_fuzz_test.go` pattern).

### Stage 1 — Correctness fixes (D1–D5) ✅ DONE (commits 782bf8b5, 67ee0b45, b5538009, a143d28e, a0c28e01; all live-verified)
1. **D1 stale view:** nav `data-on:click` expressions set `$view`/`$feedId`/`$categoryId`/`$searchQuery=''` before `@get`. Belt-and-braces: `sseEntries` treats explicit `feedId`/`categoryId` URL params as view selectors (`feed`/`category`) when `view` param absent.
2. **D2 title:** set `appViewModel.Title = ListTitle + " — Miniflux"`.
3. **D3 media proxy:** apply `mediaproxy.RewriteDocumentWithRelativeProxyURL(entry.Content)` in `sseEntry` (and fetch-content path), matching classic `proxyFilter`.
4. **D4 sorting:** `queryEntries` takes `*model.User`, uses `user.EntryOrder`/`user.EntryDirection` with `WithSorting("id", user.EntryDirection)` tiebreaker.
5. **D5 offset reset:** replicate classic's `offset >= total && total > 0 → offset = 0` re-query in `sseEntries` (and page handler).

### Stage 2 — Preferences honored (D6–D8) ✅ DONE (dd218c93, d9c6be83; D7 verified non-defect)
6. **Theme:** ship theme CSS (light/dark/serif/sans-serif/system) mapped from `user.Theme`; `data-theme` attribute + stylesheet variants. Keep Tailwind tokens as base.
7. **Timezone:** convert dates with `timezone.Convert(user.Timezone, t)` in view models; `elapsedTime` takes tz.
8. **i18n groundwork:** wire `locale.NewPrinter(user.Language)` into dsui template funcs (`t`, `plural`, localized `elapsed`); migrate hardcoded strings (real translations only — all 23 locale files exist upstream; never English placeholders).

### Stage 3 — Feature parity (P1–P10, priority order) — P1–P5, P8 ✅ DONE (e008b3c9, f401b373, 3c90e0ec, 60690fcc); P7 (98d64d10), P9 (e4b86093), P10 (0f9e4164), P4 media controls (f80e1e8a), P6 core shortcuts + g-sequences (674d93ae) done; P6 residual low-value keys documented as open
9. Reading time (P1, P10): `WithTags`-style fetch; show in row meta + entry header behind `ShowReadingTime`.
10. Tags + CommentsURL (P2, P3).
11. Enclosure upgrades (P4): video/image branches, download name, media controls with speed persistence.
12. Prev/next entry (P5, P9): server route using `NewEntryPaginationBuilder`, toolbar buttons, `h`/`l` + ArrowLeft/Right keys.
13. Keyboard parity (P6, P7): `g`-sequence handler, `/` focus search, `f` star, `d` fetch, `c`/`C` comments, `R` refresh, `?` overlay, `z t`, `a`.
14. Search unread-only toggle (P8).

### Stage 4 — Reader typography polish ✅ DONE (0b6c139a): 42rem measure, heading/list/blockquote/code/table/figure styles, dark variants
15. Port classic `.entry-content` rules (common.css ~1100-1230): measure/line-height scale, `img/video` max-width, figure/figcaption, blockquote, code/pre wrapping, table scroll wrapper, RTL-safe margins, `hr`, links. Verify against Go Blog + Lobsters content on live server, both themes.

## Verification protocol per stage
- `go build && go vet && go test ./internal/dsui/...` (+ new tests per change)
- Fuzz changed pure helpers where fuzz harness pattern applies
- Live server regression: login → unread → entry open (mark-on-view, media proxy URLs, title) → star → status toggle → search → feed click (D1 check: correct list) → offset overflow (D5 check) → sorting pref flip (D4 check)
- Commit after each verified logical change

## Post-completion validation sweep, round 3 (2026-08-21)
Extended Playwright suite to flows not covered before; all pass against the live server:
- `g u` view jump, `G` bottom / `gg` top selection jumps (index evidence), `s` star toggle round-trip, share→unshare→share toolbar cycle, fetch-content re-render keeps tags + `<time>` header (live e6e67fb9 confirmation), search pagination page 2 renders different rows with correct `N / M` indicator, settings save round-trip via the browser form.
- Media controls exercised with a stubbed audio element: seek −10s adjusts `currentTime` 100→90, speed +0.25 adjusts `playbackRate` 1→1.25 and the indicator label.
- Investigation note: SSE/HTML "row counts" gathered via `grep -c entry-row` are 3× the true row count (each row emits 3 matching lines); browser locator counts are authoritative. A suspected search-pagination total discrepancy ("2 / 2") was disproven: FTS `go` matches 12 entries, so 2 pages at `entries_per_page=10` is correct. No bug.

## Post-completion validation sweep, round 2 (2026-08-21)
Deep pass over secondary render paths plus a real-browser (Chromium/Playwright) interaction suite:
- Found+fixed **e6e67fb9**: fetch-content re-render rebuilt the detail by hand and dropped tags/comments/reading time/share code/enclosures; now re-fetches with enclosures and reuses `entryToDetailView` (regression: `TestSSEFetchContentKeepsParityFields`).
- Found+fixed **5caae601**: `sseEntry` and toggle-status single-row re-renders dropped `ReadingTime`/`ShowReadingTime`, so the reading-time chip vanished after opening an entry or toggling status (regression: `TestRowRerenderKeepsReadingTime`).
- Found+fixed **6a37339e**: stale `?q=` on non-search app pages leaked a dead `searchQuery` into pagination URLs; only `/ds/search` consumes `q` now (regression: `TestNonSearchViewIgnoresQParam`).
- Classic keybinding audit: dsui `A` (mark page read), `m`, `s`, `v`, `?`/Esc, `/`, `g u|b|h|s`, `gg`/`G`, j/k all match classic semantics; dsui's extra `r`=reload is harmless. `R` (refresh feeds) remains in the documented-open P6 residual list.
- Real-browser checks (Playwright Chromium against the live server): login→rows, search("go")=12 → feed click=10 rows (D1 holds in-browser), entry `<time>` relative header, j/j/k selection advance+back with title evidence, `n` loads next entry into the pane, `m` flips toolbar status label, `?`+Esc overlay, unread-only checkbox filters 4→3 live, pagination link markers. 10 of 11 checks passed; the one nominal failure was a harness premise error (it expected the dsui app after login, but login redirects to the classic default home page by upstream design — not an app defect).
- Empty-state catalog values verified per view in fr_FR (`alert.no_*` keys all resolve).
- Fresh `go vet ./...`, `go build ./...`, `go test ./...` (55 packages, no cache): green.
- Static i18n audit: 109 `t`/`plural`/printer keys used across dsui templates and Go code exist in all 23 locale files.
- Found and fixed (57e47caa): entry header date used a hardcoded English `"January 2, 2006"` layout; now renders localized relative time via `elapsed` (classic parity), with iso `datetime` + absolute `title` tooltip.
- Enclosure proxying re-derived against classic `entry.html`: dsui's `ShouldProxifyURLWithMimeType` is exactly equivalent to classic's `mustBeProxyfied(type)` + `proxyURL` combination for all mode/scheme/type combinations (default `http-only` + types=image leaves https audio raw in both).
- Live checks: all six theme enum values map to correct `data-theme`/`data-font`; timezone conversion exact (2025-10-29 00:00 UTC → Oct 28 19:00 America/Chicago); French menu/titles/empty states/reading time; tags, comments link, typed enclosures, gated reading time on entry detail; unread-only search excludes read entries (raw line-match counts 12 → 9; distinct rows 4 → 3, read entry absent); pagination markers on multi-page lists; shortcuts overlay + g-sequences in served JS; media progression round-trip (77s saved → `data-last-position="77"`).
- Note: neither dsui nor classic validates theme enum membership server-side beyond non-empty; both UIs only offer valid values in the select. Test-DB residue (theme="light") was sweep curl fallout, not a code path reachable from the UI.
