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
| P6 | Keyboard shortcuts: **DONE** full classic set (f401b373, e4b86093, 98d64d10, 674d93ae, 40c2ea2d): j/k, Enter/o, v, s, m, A, r, n/p, ArrowLeft/Right, '/', '?'+Esc overlay, g u|b|h|s|f, g g/G, plus parity keys h/l, f, c/C, d, a, z t, R. Intentionally omitted: g c (no categories page in dsui; feed tree groups by category), F/g f-to-feeds-list (same: no feeds page, g f goes to the selected entry's feed), '+' and '#' (dsui has no dedicated feeds page; add/remove subscription live in the settings feeds section since round 7). | `keyboard.js` |
| P7 | ~~No keyboard-shortcuts help overlay~~ **DONE** (98d64d10): '?' toggle + Escape close, localized | `keyboard_shortcuts` dialog |
| P8 | ~~Search: no unread-only toggle~~ **DONE** (3c90e0ec, 6a797fa1): checkbox beside search box, `searchUnreadOnly` signal → status-filtered query | `search.go` `unread` param |
| P9 | ~~No pagination keyboard nav~~ **DONE** (e4b86093, 40c2ea2d): ArrowLeft/Right click the marked pagination links; `z t` scrolls the selected item into view | | `app.js:1191-1196,1199` |
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

## Post-completion validation sweep, round 9 (2026-08-21)
User-reported issues: GUI prefs should apply on save (not reload), and the panel resize handle showed a cursor but could not drag.
- **Resize handle fixed (dead code path)**: the drag handler rewrote `style.gridTemplateColumns` via `.replace(/1fr 4px 1fr/, ...)`, but the inline style is empty until first set (the real grid lives in the stylesheet: `240px 1fr 4px 1fr`), so the replace never matched and dragging did nothing. Now the handler writes the explicit template `240px {w}px 4px 1fr`, guarded to desktop widths, plus touch support and the existing localStorage persistence. Verified live: drag +150px widens list 578→728, width survives reload.
- **GUI prefs apply on save, no reload — pure Datastar**: layout now seeds `uiTheme/uiFont/uiLang/uiDir` signals on `<html>` and binds `data-attr="{'data-theme': $uiTheme, 'data-font': $uiFont, 'lang': $uiLang, 'dir': $uiDir}"`. On save the SSE response (a) re-renders the settings fragment in the new language and patches it (`selector .settings-page`, mode inner), and (b) patches the chrome signals — data-attr keeps the html attributes live, so the CSS theme flips instantly.
- Research note: tried ExecuteScript first; it injects an inline `<script>` which the app CSP (`script-src 'self'`) blocks, and the SDK v1.2.2 protocol has no attribute-patch event — `data-attr` is the documented mechanism ("sets the value of any HTML attribute to an expression, and keeps it in sync"). Verified empirically that data-attr works on the `<html>` element and reacts to SSE signal patches.
- Live-verified: theme dark+serif applies instantly (body bg flips to rgb(26,26,46)), lang becomes fr-FR, labels re-render in French ("Paramètres du lecteur"), toast localized ("Préférences sauvegardées !"), NO reload (beforeunload never fired), prefs persist across reload, restore to light/en works. Regression tests: TestLayoutBindsChromeSignals, TestSSESaveSettingsPatchesChromeSignals, TestResizeHandleAppliesGridWidth.

## Post-completion validation sweep, round 8 (2026-08-21)
Management parity completion: categories.
- **Round-7 regression fixed**: settings.html had TWO sections with id="feeds" (reading prefs + the new management section) — invalid HTML that broke the #feeds nav anchor. Management section renamed to id="subscriptions".
- **Gap closed**: dsui had no category CRUD (classic: create/rename/remove under Categories). Added to the subscriptions section: create form, per-category rename form, remove button — all regular form POSTs with CSRF hidden fields (/ds/create-category, /ds/rename-category/{id}, /ds/remove-category/{id}).
- **Deliberate divergence from classic**: classic's removeCategory calls store.RemoveCategory unguarded; on SQLite the FK cascades, which would silently delete every feed in the category (and their entries). dsui refuses removal while feeds remain assigned (flash error + disabled button). Classic's opaque FK failure on Postgres is an upstream UX issue, not copied.
- Live-verified: create → row with feed count; duplicate → localized "This category already exists." flash; rename round-trips; empty category removes; feed-bearing category refuses (flash + survives); missing id redirects; CSRF 400 without token; sidebar unaffected.
- Regression tests: TestSettingsListsCategoriesWithCounts, TestCreateCategory, TestRenameCategory, TestRemoveCategoryGuardsAssignedFeeds.

## Post-completion validation sweep, round 7 (2026-08-21)
Integration boundary: feed lifecycle management.
- **Gap found and fixed**: dsui had NO way to add or remove a feed (only OPML import/export), while this audit's P6 note claimed both existed in settings — an overstated claim, now corrected. Classic exposes this via /subscribe and the feed edit page.
- Implemented: settings "Feeds" section — add-feed form (POST /ds/add-feed → reader feedHandler.CreateFeed, default FirstCategory, localized errors via the round-5 flash banner) and per-feed remove (POST /ds/remove-feed/{id} → store.RemoveFeed, entries cascade). Feed rows show title, site link, category, parsing errors.
- Handler correctness fix during testing: store.FeedByID returns (nil, nil) for not-found, so the initial removeFeed existence check could never fire; now nil-aware (also makes the cross-user path a clean redirect).
- Live-verified: add https://miniflux.app/feed.xml → appears in settings + sidebar, feed view lists 50 entries; unfetchable URL → localized flash error shown exactly once; remove → row gone, DB has 0 orphaned entries; missing feed id → redirect, no error; CSRF 400 without token.
- Regression tests: TestSettingsListsFeedsWithRemoveForms, TestRemoveFeedDeletesFeed, TestRemoveFeedMissingFeedRedirects, TestAddFeedRequiresURL, TestRemoveFeedIsUserScoped.

## Post-completion validation sweep, round 6 (2026-08-21)
Untested-surface pass: escaping, auth/CSRF boundary, live prefs, mobile, media progression:
- **a1473030** (found via mobile testing): Datastar SSE patches MORPH the DOM in place — after the initial render, updates arrive as characterData/attribute mutations, not childList. Both keyboard.js observers (mobile panel auto-switch, list selection reset) subscribed to childList only, so (a) tapping a row on mobile NEVER switched to the content panel, and (b) pagination/mark-page-read silently killed keyboard selection. Observers now subscribe to all mutation types; panel switches only when the article title changes (pre-rendered first entry must not yank the user on load); list morphs fall back to selecting row 0 (classic parity).
- Escaping verified with hostile titles: entry titles escaped in row and detail (html/template auto-escape), feed titles escaped in the tree; entry content renders raw via template.HTML exactly like classic's safeHTML (same trust model: sanitizer at ingest).
- Auth/CSRF boundary: all 11 POST endpoints reject missing/wrong CSRF with 400; unauthenticated GETs redirect to login with return URL.
- Live prefs: language fr_FR and theme dark switch via settings and take effect on next render; restored.
- Media progression: pause handler saves position via the upstream save-progression endpoint (77s round trip → data-last-position on re-render); seek/speed controls re-verified. Note: live-feed refresh had legitimately dropped the fixture enclosure (feed content changed), not a bug.
- Mobile suites all green after the observer fix; desktop suites unchanged.

## Post-completion validation sweep, round 5 (2026-08-21)
Integration-boundary pass over remaining flows; three fixes, all live-verified:
- **eb237b8f**: OPML import/fetch failures were silent (log + redirect, no user-visible error; classic re-renders with the error). Failures now set a short-lived flash cookie and redirect to /ds/settings, which renders an error banner exactly once (consuming response expires the cookie). Fetch errors use the localized fetcher message.
- **8f3f49ee**: after '/' focused the search box, ALL shortcuts were dead until clicking elsewhere — Escape was swallowed by the input guard. Escape now closes the overlay and blurs the search box even while an input is focused; typing is unaffected.
- Verified-correct (no change): 'A' marks exactly the current page's entries (page-2 test with epp=5: only those 5 ids flipped, countUnread patch correct — reading the stale initial data-signals attribute had produced a false alarm); share flow end-to-end including the public /share/{code} page (upstream route, 200); 'v' fallback (loads entry then opens original); 'm' flips both row class and toolbar label; '/' focus works; direct mark-page-read POST ignores extraneous entryIds JSON by design (page derived from signals).
- Static i18n audit re-run with the new overlay keys: 115 dsui keys present in all 23 locales.
- All three Playwright suites against the final build: core 10/11 (known login-redirect premise), r3 8/8, r4 16/16 (c skipped: no comments URL on the selected fixture entry).

## Post-completion validation sweep, round 4 (2026-08-21)
Closed the P6 parity-shortcut residual list and, in the process, found two real SSE bugs:
- **40c2ea2d**: implemented h/l, f, c/C, d, a, z t, g f, R (markers: `data-action=toggle-star|fetch-content|comments-link|refresh-all`, `data-feed-id` on rows, enclosures now inside `<details class=entry-enclosures open>` so 'a' can toggle). Selection restore: sseEntry replaces the opened row node, which silently dropped the keyboard `.selected` class — keyboard.js now tracks the selected entry id and re-applies it after list mutations (this is what made h/l lose selection mid-navigation).
- **49f859e7**: two latent toolbar bugs exposed by the new 'f' key and a corrected share harness — sseToggleStar patched `#toolbar-star-icon-{id}` (never existed; label stuck on "Star"), and sseToggleShare patched only a `shared` signal no template binds (label never flipped, no public share link shown). Both now patch real ids; the shared state shows the `/share/{code}` link like classic.
- **Correction to round 3**: its "share → unshare" PASS was a false positive — Playwright `:has-text` is case-insensitive, so a "Share" matcher also matched "Unshare" labels. The r3 harness now uses exact regex matching and drives share state to a known point first.
- Playwright r4 harness (18 checks: h/l, f, d, c, a, z t, g f, R, 8 overlay rows) passed 3× consecutively; r3 (fixed) 8/8; core 10/11 with the known login-redirect premise note. g c / F / '+' / '#' intentionally omitted (no dsui equivalents; documented in P6).

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
