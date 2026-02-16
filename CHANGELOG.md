# Changelog

All notable changes to SAM (Simple AppStakes Manager) are documented in this file.

## [Unreleased]

### Changed

- **Typography** — Replaced Inter with Sora (headings) and DM Sans (body) for a more distinctive fintech aesthetic
- **Error notifications** — Error toasts now persist until manually dismissed (success toasts still auto-dismiss after 5s); errors use a red theme instead of orange
- **API error messages** — Server-side error details are now surfaced to the user instead of generic "Failed to..." messages
- **Auto top-up removal** — Delete button now requires a second click to confirm, preventing accidental removal
- **Low-stake card** — Stats panel "Low Stake Apps" card now uses a red-tinted alert style when count > 0

### Added

- **Mobile responsive layout** — Applications table collapses to a card-based layout on screens < 768px
- **Loading skeletons** — Shimmer placeholder shown during initial data load instead of a blank page
- **Modal focus traps** — Tab key is now trapped within open modals (WCAG 2.1 compliance)
- **Accessibility improvements** — `aria-label` on icon-only buttons, `role="switch"` + `aria-checked` on toggle, `role="dialog"` + `aria-modal` on modals, `role="alert"` on notifications, semantic `<header>`, `<main>`, `<nav>` landmarks

### Fixed

- **`max-w-8xl` layout bug** — Replaced non-existent Tailwind class with `max-w-screen-2xl` to properly constrain content width
- **Deprecated `keypress` event** — Keyboard shortcuts now use `keydown`, which fires consistently across all browsers
- **Keyboard shortcuts in form fields** — Shortcuts are now suppressed when typing in inputs, selects, or textareas

### Added (prior)

- **Stake new application** — Stake a new app for any on-chain service directly from the UI (`POST /api/applications/stake`)
- **Services endpoint** — Query available services on a network (`GET /api/services?network=`)
- **Auto top-up** — Background worker automatically funds and upstakes applications when their stake drops below a configurable threshold
  - Per-app config with trigger threshold and target amount (`PUT/DELETE /api/applications/{address}/autotopup`)
  - Smart funding: skips the fund step if the app's liquid balance already covers the needed amount
  - Worker runs every 5 minutes; configs persisted in `autotopup.json`
  - Recent events viewable via `GET /api/autotopup/events`
- **Config persistence** — New staked applications are automatically added to `config.yaml` via targeted line insertion (preserves comments and formatting)
- **Frontend modals** — StakeNewAppModal with service dropdown, AutoTopUpModal with threshold/target inputs and enable/disable toggle
- **Auto top-up indicators** — "AUTO" badge on apps with auto top-up enabled; hover tooltip shows trigger/target values
- **Auto top-up events panel** — Collapsible activity panel between stats and search showing recent auto top-up events with status, timestamps, and tx hashes
- **Store validation** — `Store.Set()` rejects configs with non-positive values or target <= trigger

### Fixed

- **Race condition in worker event tracking** — `addEvent()` now uses a dedicated mutex, preventing data races with the `Events()` reader
- **Empty applications list handling** — `SaveApplicationAddress` now correctly inserts the first entry when the `applications:` list is empty
- **In-memory/disk state inconsistency** — Handler rolls back the in-memory config change if `SaveApplicationAddress` fails on disk
- **Missing fsync in atomic write** — Auto top-up store now calls `Sync()` before close to ensure data durability on crash
- **Fund handler cache invalidation** — `handleFund` now invalidates the app cache (not just bank cache) so liquid balance changes are reflected
- **Worker shutdown responsiveness** — `pollBalance` is now context-aware; worker checks for cancellation between apps and networks
- **Incomplete error messages in worker events** — Fund/upstake failure events now capture `result.Message` when the error is nil but success is false

### Changed

- Refactored monolithic `main.go` into internal packages: `handler`, `pocket`, `config`, `autotopup`, `cache`, `validate`, `models`
- CORS configuration includes `PUT` and `DELETE` methods
- Graceful shutdown stops the auto top-up worker before the HTTP server
- Fixed `go.mod` dependency markers (direct deps were incorrectly marked `// indirect`)
