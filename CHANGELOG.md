# Changelog

All notable changes to SAM (Simple AppStakes Manager) are documented in this file.

## [Unreleased]

### Added

- **Return-liquid sweep endpoint + UI** — new `POST /api/applications/{address}/return-liquid` sends all but 1 POKT of an app's liquid balance back to the configured bank address (signed by the app itself). The reserve (1 POKT) is kept on the app so it can still pay tx fees. UI: cyan arrow-down-to-bank button on each app row/card alongside Fund, opens a confirm modal showing current liquid / reserve / fee / amount-to-send. Button is disabled when the app's liquid balance is at or below the reserve threshold (nothing to sweep).
- **Bank-balance pre-flight + low-balance alerts** — auto top-up worker now queries the bank balance once per network at the start of each cycle and tracks a running deduction as it processes apps. If the running total would exceed the bank balance, remaining fund txs are skipped (no more spamming chain-rejected txs) and a per-network demand snapshot is exposed at `GET /api/autotopup/bank-status`. The dashboard's Bank Balance card now shows a red `LOW` badge with the pending-top-up deficit when the bank can't cover demand. Optional Discord webhook (configured under `notifications.discord` in `config.yaml`) fires a templated message on the same condition, with a configurable per-network cooldown (default 60 min) so the alert isn't repeated every 3-minute cycle.
- **Unstake feature (backend + UI)** — `POST /api/applications/{address}/unstake` begins the unbonding process. After the unbonding period (~1–2h on mainnet), the staked POKT is automatically returned to the application's liquid balance and the on-chain entry is removed; the tracking row in `config.yaml` is **kept** so the app can be re-staked from the same UI row. UI adds a red unstake button on each app row/card, a destructive-action confirm modal (requires typing `UNSTAKE`) that explains the unbonding timeline, and disables Upstake/Delegate/Unstake while an app is `UNBONDING` or `NOT_FOUND`.
- **Application status field** — The `Application` model now exposes `status` (`STAKED` / `UNBONDING` / `NOT_FOUND`) and `unstake_session_end_height`. List endpoint returns a stub row for apps that 404 (never staked, or fully unbonded and removed from chain state) so tracking persists across stake/unstake/restake cycles. Auto-top-up worker now skips apps that are not in `STAKED` status.
- **Configurable bind address** — `BIND_ADDR` env var sets which interface the HTTP server listens on; defaults to `127.0.0.1` (loopback-only) for secure-by-default bare-binary use. Docker and Helm set `BIND_ADDR=0.0.0.0` so the container/pod is reachable via port mapping/Service.
- **Configurable CORS origins** — `ALLOWED_ORIGINS` env var accepts comma-separated origins for production domains; falls back to localhost when unset
- **API authentication** — Optional bearer token auth for write endpoints (stake, upstake, fund, delegate, auto-top-up); configured via `auth.token` in config.yaml or `AUTH_TOKEN` env var
- **Frontend auth flow** — Token input modal triggered on 401 response; token stored in sessionStorage; lock/unlock indicator in header
- **Content-Security-Policy header** — CSP added to SecurityHeaders middleware allowing required CDN sources
- **Tailwind SRI** — Subresource integrity hash added for Tailwind CDN script
- **API request timeouts** — Frontend fetch calls use AbortController with 15s timeout for reads, 60s for writes

### Changed

- **Tx hash logged on manual write endpoints** — `/applications/stake`, `/upstake`, `/fund`, `/delegate`, `/unstake` now log the on-chain `tx_hash` on success alongside the address/network. Previously only the auto top-up worker recorded the hash, which made "did my manual tx land?" answerable only by re-querying chain state.
- **Auto top-up cadence** — worker tick interval reduced from 5 minutes to 3 minutes. Current mainnet block time is ~70s, so 3 min covers ~2.5 blocks and stays comfortably within a single session, letting top-ups react sooner to stake drawdown.

### Fixed

- **Unstaked apps no longer counted as "Low Stake"** — apps with no on-chain stake (`NOT_FOUND`) reported `danger` status (stake 0 ≤ danger threshold), inflating the Low Stake Apps dashboard count. Unstaked and unbonding apps are now excluded from that count. Their status badge label is also shortened from "NOT STAKED" to "UNSTAKED" so it no longer wraps to two lines.
- **Account-sequence race on writes** — any pocketd write tx could fail with `account sequence mismatch, expected N, got M` when two writes from the same signer overlapped (worker cycles overlapping with each other or with manual API calls, or an admin running pocketd from a shell while SAM is mid-cycle). pocketd's default broadcast-mode is `sync`, so the tx returns once the mempool accepts it — before block inclusion — and a second tx that re-queries the chain sees the old sequence number. Fix is in two layers, both via parse-and-retry around the pocketd error: (1) every tx method (`StakeNewApplication`, `UpstakeApplication`, `FundApplication`, `DelegateToGateway`, `UnstakeApplication`) now runs through a new `RunTxWithSeqRetry` that parses `expected N` from the error, looks up the signer's `account_number` via `/cosmos/auth/v1beta1/accounts/{addr}`, and retries once with explicit `--account-number / --sequence` flags; (2) the auto top-up worker additionally pre-fetches the bank's `account_number + sequence` once per cycle and threads a locally-incremented sequence through each fund tx via `FundApplicationWithSequence`, so the in-cycle case never needs a retry and many simultaneous funds don't generate retry storms.
- **Silent background load failures** — Bank account, auto-top-up config, and auto-top-up event load errors now show user-visible notifications instead of logging to console only
- **Auto top-up upstake failing** — Fund calculation did not reserve 1 uPOKT for the upstake transaction fee, causing the on-chain stake tx to fail with insufficient funds while the fund tx succeeded (money left bank but stake was not increased)
- **Silent on-chain tx failures** — Transaction responses now check the Cosmos SDK `code` field; previously a failed on-chain tx (code != 0) was treated as successful if it returned a txhash

### Added

- **Minimum liquid balance reserve** — Auto top-up now supports a `min_liquid_balance` setting per app to keep a minimum liquid POKT balance after upstaking (e.g., 5 POKT for tx fees). Set via the auto top-up API.

- **Delegate to gateway** — Delegate an application to a gateway directly from the UI (`POST /api/applications/{address}/delegate`); gateway dropdown populated from `config.yaml`

- **Docker support** — Multi-stage Dockerfile with pocketd bundled, docker-compose.yml for local dev
- **Helm chart** — Full Kubernetes deployment chart (`charts/sam/`) with ConfigMap, PVC, ingress, health probes
- **GitHub Actions CI** — Runs vet, test, build, Docker build, and Helm lint on push/PR
- **GitHub Actions release** — Builds cross-platform binaries, Docker image, and Helm chart on `v*` tags
- **`CONFIG_FILE` env var** — Override config.yaml path (default: `config.yaml`), enables mounted configs in containers
- **`DATA_DIR` env var** — Override directory for `autotopup.json` (default: `.`), enables persistent volumes
- **Version injection** — Binary version set via `-ldflags` at build time, logged at startup
- **`VERSION` file** — Single source of truth for project version; CI validates consistency with Chart.yaml and git tags

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

- **Incomplete services list** — Services query now paginates through all API pages instead of returning only the first page
- **Docker build failing** — Corrected pocketd download URL and binary name in Dockerfile (asset was renamed from `poktroll_*` to `pocket_*` and binary from `poktrolld` to `pocketd`)
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
