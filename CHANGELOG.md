# Changelog

All notable changes to SAM (Simple AppStakes Manager) are documented in this file.

## [Unreleased]

### Added

- **Stake new application** — Stake a new app for any on-chain service directly from the UI (`POST /api/applications/stake`)
- **Services endpoint** — Query available services on a network (`GET /api/services?network=`)
- **Auto top-up** — Background worker automatically funds and upstakes applications when their stake drops below a configurable threshold
  - Per-app config with trigger threshold and target amount (`PUT/DELETE /api/applications/{address}/autotopup`)
  - Smart funding: skips the fund step if the app's liquid balance already covers the needed amount
  - Worker runs every 5 minutes; configs persisted in `autotopup.json`
  - Recent events viewable via `GET /api/autotopup/events`
- **Config persistence** — New staked applications are automatically added to `config.yaml` via targeted line insertion (preserves comments and formatting)
- **Frontend modals** — StakeNewAppModal with service dropdown, AutoTopUpModal with threshold/target inputs and enable/disable toggle
- **Auto top-up indicators** — "AUTO" badge on apps with auto top-up enabled
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
