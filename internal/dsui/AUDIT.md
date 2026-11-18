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

## Visual audit, round 12 — rendered screenshots (2026-11-18)

Method: the app was rendered in a real browser against a live SQLite server (109 entries,
real feed) and reviewed from screenshots at 1440x900 and 430x900, light and dark, covering
unread, starred (empty), settings, about, sessions, mobile list, mobile reader, and the
save toast. Headless Chromium could not composite in this environment (page screenshots
time out, CDP capture drops the target), so the captures came from the OS-level window.

### Defects found and fixed

| # | Defect | Evidence | Fix |
|---|--------|----------|-----|
| V1 | **"Mark all as read" was clipped** in the list header at 1440px and on mobile; the second button rendered half outside the panel. | screenshot: label cut to "Mark all…" | Actions grouped in `.list-actions` with `margin-left:auto`; `.top-nav` wraps, so the group drops to a second row instead of overflowing |
| V2 | **Reading pane repeated the list's empty message**, so an empty view showed "There are no starred entries." twice, at two different heights. | starred view screenshot | Pane shows a muted centered logo mark; the view-specific message stays in the list only |
| V3 | **Raw translation key rendered**: the category row printed `page.categories.feed_count: 1` because the template used `t` on a plural key. | settings screenshot | `plural` helper; `TestSettingsUsesPluralForFeedCount` fails on a raw key |
| V4 | **Feed name in row meta was green** (bright green in dark mode) while the rest of the meta line is muted gray, so every row had a colored word competing with the title. | unread screenshot, light and dark | Inherits the muted meta color; long feed names ellipsize |
| V5 | **Entry toolbar invisible on first load**: the pane pre-rendered the first entry, but `data-show="$selectedEntryId"` was bound to a signal still at 0, so star/share/fetch/status were hidden until the user clicked a row. | unread screenshot had no toolbar; a11y dump showed the buttons | `selectedEntryId` seeded with the preloaded entry id |
| V6 | **Attachments dumped full URLs** (20+ lines of `https://github.com/...` per release note) and printed "0 B" when the feed declares no size. | unread screenshot of a release note | Link label is the last path segment with the full URL in the tooltip; size shown only when known |
| V7 | **Inconsistent meta separators**: a middot appeared only before the reading time. | article header screenshot | CSS separators between every meta item |
| V8 | **Large gap between the article header and the first heading** (header padding plus the heading's top margin). | article screenshot | `.article-body > :first-child { margin-top: 0 }` |
| V9 | **Settings layout confusion**: "Feeds" appeared twice (prefs section and management section), Save sat between them reading as "save feeds", long select labels were truncated in five columns, and the header had no page title ("← Feeds"). | settings screenshot | Feed prefs merged into Reader Settings, sticky save bar, 300px grid columns, header shows "← Unread  Settings" with Logout, divider between section links and page links |
| V10 | **Saving settings jumped the page** to another section: the response replaced `.settings-page`, resetting the content pane scroll position. | screenshot after save | Page is re-rendered only when the language changes (the only case where server-rendered labels differ); theme/font/lang still apply instantly through the chrome signals |

### Dependency update

- Datastar JS bundle upgraded v1.0.2 → v1.0.5 (current stable; the Go SDK is already at the
  latest published version, datastar-go v1.2.2). Re-verified after the upgrade: click-to-open
  patches the reading pane, star/status toggles patch rows and the sidebar, and the settings
  save returns its toast and chrome signals.

### Verification

- `go build ./...`, `go vet ./...`, `go test ./...`, `make test` (race), `gofmt -l .` clean.
- New regression tests: enclosure filename/size labels, plural feed count, header action
  wrapper, single empty-state message, no re-render on save without a language change.
- Screenshots re-taken after each fix (unread light/dark, starred empty, settings, mobile
  list and reader, save toast).

## Post-completion review pass, round 11 — full read of the package (2026-11-18)

Review scope: every file in `internal/dsui` (handler, pages, templates, JS, CSS) read end to
end against `internal/ui`, then verified against a live SQLite server (real feed,
109 entries, curl + sqlite3 for state checks). Each finding below is a commit.

### Defects found and fixed

| # | Defect | Evidence | Fix |
|---|--------|----------|-----|
| D9 | **Password change reset every boolean preference.** The password form posted to `/ds/sse/settings`, whose partial-form apply writes the checkbox fields unconditionally: reading time, keyboard shortcuts, strip-heading, mark-read-on-view all flipped to false. The form also never asked for the current password, so a hijacked session could take over the account. | `applyToUser` + form with only password/confirmation; live-verified 1→0 after submit | Dedicated `POST /ds/sse/password` that only touches the password, requires the current password when one is set, runs the same validator, signs out other sessions, rotates the current one, and re-renders the section (`ef8b2da2`) |
| D10 | **Mark-read-on-view ignored for the preloaded entry**, and the media-completion variant ignored entirely (`user.MarkReadOnView` was checked directly instead of `Entry.ShouldMarkAsReadOnView`). | `showApp` never touched status; `sseEntry` used the raw flag | Both paths use `ShouldMarkAsReadOnView`; the preloaded entry is marked read before the menu is built (`4111f113`) |
| D11 | **Sidebar unread counters went stale.** Opening an entry, toggling its status, or marking a page read patched only a page-level `countUnread` signal; the per-feed and per-category counters in the tree kept their old values until a reload. | live: tree still showed 105 after reading an entry | `sidebarFragment` re-renders the tree and its counters on every status change (`4111f113`) |
| D12 | **Reading pane always said "no unread entries"** in starred, history, shares, feed, and category views. | `entry_content.html` hardcoded `alert.no_unread_entry` | Per-view empty message (`438f14b0`) |
| D13 | **`<time datetime>` had no timezone offset**, so the markup was not a valid machine timestamp. | `2006-01-02T15:04:05` | RFC 3339 with offset (`438f14b0`) |
| D14 | **`keyboard_shortcuts` preference ignored**: shortcuts stayed active after being disabled in the settings. | no reference to the field in JS | `data-keyboard-shortcuts` on `<body>`, handler bails out (`438f14b0`) |
| D15 | **Refresh had no guards.** Every click pushed a job batch (classic rate-limits through the session force-refresh interval), the per-host polling limit was not applied, and a single-feed refresh accepted any feed id without an ownership check. | `refreshFeeds`/`refreshFeed` | Session interval, `WithLimitPerHost`, ownership check, localized toasts (`ccd45709`) |
| D16 | **Hardcoded English server messages and labels**: refresh toasts, feed/category validation flashes, mobile panel labels, settings page title, category-in-use guard. | grep of Go literals + templates | Localized through existing keys plus three new keys translated in all 24 locales (`ccd45709`); `TestDsuiTranslationKeysExistInEveryLocale` now fails when a key is missing anywhere |

### Parity gaps closed

| # | Gap | Implementation |
|---|-----|----------------|
| P11 | Shared entries page | `/ds/shares` as a first-class reader view (list + pane + pagination + SSE), menu entry (`d45fad08`) |
| P12 | About page | `/ds/about`: version/commit/build date/Go version, plus admin-only database version, database size, and global options (`d45fad08`) |
| P13 | Sessions page | `/ds/sessions`: list with current-session marker and revoke action that never removes the requesting session (`d45fad08`) |
| P14 | Logout | Form in the sidebar and on every standalone page, posting to the shared `/logout` route with the session CSRF token (`d45fad08`) |
| P15 | Admin screens | Integrations, authentication, API keys, and user management stay in the classic UI and are linked from the settings nav. They are set-once screens whose update paths (Fever token hashing, Google Reader password hashing, duplicate checks, notifier construction) would be duplicated wholesale; linking keeps one implementation. |

### Deliberate boundaries (unchanged)

- `dir="ltr"` stays hardcoded: the classic UI has no `dir` attribute at all, and Miniflux
  has no RTL locale.
- Keyboard `g c` (categories page), `F` (feeds page), `+`/`#` remain omitted: dsui has no
  dedicated feeds/categories pages.

### Verification

- `go build ./...`, `go vet ./...`, `go test ./...`, `make test` (race), `gofmt -l .` clean.
- 20 new regression tests across the fixes above (password/preference preservation, current
  password requirement, mark-read-on-view, sidebar counters, per-view empty state, RFC 3339,
  shortcuts preference, refresh rate limit, refresh ownership, shares view, about, sessions,
  session revocation, logout form, locale key coverage).
- Live SQLite server: login → unread/starred/history/shares/settings/about/sessions render
  with correct titles → add feed → SSE open marks read and patches `#subscription-panel
  .feed-tree` with the new counters → status toggle patches row, toolbar, and tree →
  share creates a code and `/ds/shares` lists the entry → settings save returns the
  localized toast and chrome signals → password change rejects a wrong current password,
  preserves preferences, and the new password logs in → session revoke deletes the row →
  logout ends the session.

## Post-completion validation sweep, round 10 — systematic UX review (2026-08-21)
Google Reader-aesthetic pass over layout/typography/hierarchy/affordances/accessibility/i18n (review done against live DOM + CSS/template analysis; the supplied screenshots could not be rendered by this agent's model, which has no vision input).
- **Strengths confirmed (no change)**: three-pane grid with independent scroll and resizable divider; compact 0.8125rem list rows with muted meta line; unread=600/gray-900 vs read=400/gray-600 distinction; 42rem reading measure at 1.65 line-height; star/mark-read/share/fetch/comments affordances.
- **Selected-row contrast (fixed)**: sidebar selected item used `blue-100` but entry-list selected row used `blue-50` (near-invisible). Now both `blue-100`; dark mode selected row `#1a2a4a` → `#1e3a5f` (both the prefers-color-scheme and explicit `data-theme=dark` blocks). Verified live: selected row bg = `oklch(0.932 0.032 255.585)` (blue-100).
- **Keyboard focus (fixed)**: zero `:focus-visible` rules existed. Added a global focus ring + inset variants for `.feed-tree-item`; removed a dead rule for the non-focusable custom `<entry-row>` element.
- **i18n leak (fixed)**: sidebar header hardcoded "Subscriptions" → `{{t "menu.feeds"}}`. Verified live: "Feeds".
- **Search input (fixed)**: hard-fixed 120px → `10rem` with `flex:1 1 10rem` / `min-width:6rem` / `max-width:18rem`. Verified live: 160px wide, grows.
- **Remaining documented gaps (unchanged)**: mobile-nav buttons "Articles"/"Reader" are hardcoded English with no existing i18n key (leaving as-is per the "real native keys only, no invented translations" rule); RTL direction still hardcoded `ltr` (matches classic, which has no `dir` attribute at all); keyboard `g c`/`F`/`+`/`#` remain intentionally omitted (no feeds/categories pages in dsui).

- **Squished list titles (fixed — this was the real "squished" complaint)**: entry titles used `white-space:nowrap` + `text-overflow:ellipsis`, squishing every title to one truncated line (5 of 12 titles cut off mid-word at the list width). Now a 2-line clamp (`-webkit-line-clamp:2`) so titles wrap like Google Reader; short titles stay one line, long ones flow to two. Verified live: previously-truncated titles ("Zero-Knowledge Proofs Aren't Age Verification…", "Open-sourcing OpenPubkey SSH (OPKSSH)…") now render fully.
- **Grid ratio (follow-up)**: default grid refined from `minmax(320px,420px)` to `240px 1fr 4px 2fr` so the reading pane always stays ~2x the list regardless of viewport (the fixed-width minmax could still squeeze content on narrower windows).
- **Import button floated right (fixed)**: `.settings-file-input` had `width:100%` with no `max-width`, so `flex:1` stretched the file input full-width and pushed the "Import" button to the far right edge. Added `max-width:320px` (matches `.settings-input`) and dropped the stray `margin-bottom`; the button now sits adjacent to the file input, consistent with the subscribe/update buttons.

- **Missing Tailwind utilities (fixed — root cause of the "squished" look)**: `app.css` is a pre-compiled Tailwind v4 build, but several utility classes the templates use were absent from it, so they silently no-op'd: `gap-1`, `mb-1`, `mb-2`, `mt-2`, `py-1`, `py-2`, `text-xs`, `font-semibold`, `inline-flex`. Result: the search "Unread" checkbox rendered with 0px gap against the search input, and intended margins/padding (e.g. the fetch-OPML form's `mt-2`) were never applied. Added all nine utilities to `@layer utilities` (Tailwind v4 `calc(var(--spacing) * N)` style) and bumped the search form to `gap-2`. Verified live: checkbox gap 0px → 8px; fetch-OPML form margin-top 0px → 8px.

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
