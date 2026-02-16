# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

SAM (Simple AppStakes Manager) is a Go web application for managing Pocket Network application stakes. It combines a Go REST API backend with a React SPA frontend served from a single HTML file.

**Module:** `github.com/pokt-network/sam`

## Common Commands

```bash
make build              # Build binary → ./sam (VERSION=dev by default)
make build VERSION=1.0  # Build with version injected via ldflags
make run                # Build and run (checks for pocketd)
make dev                # Hot reload via air (auto-installs if missing)
make test               # go test -v ./...
make clean              # Remove sam binary and tmp/
make setup              # Install deps + initialize config.yaml
make check-pocketd      # Verify pocketd CLI is in PATH
docker compose up       # Run via Docker (builds image, mounts config)
helm lint charts/sam    # Lint Helm chart
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `9999` | HTTP server port |
| `CONFIG_FILE` | `config.yaml` | Path to config file |
| `DATA_DIR` | `.` | Directory for `autotopup.json` |

Run with custom port: `PORT=8080 ./sam`

## Architecture

**Backend** organized into internal packages:
- `cmd/web/main.go` — Entry point, wiring, CORS, graceful shutdown
- `internal/handler/` — HTTP handlers and route registration
- `internal/pocket/` — Pocket Network API client and pocketd CLI executor
- `internal/config/` — YAML config loading, validation, and persistence
- `internal/autotopup/` — Auto top-up config store (JSON) and background worker
- `internal/cache/` — Generic in-memory cache with TTL
- `internal/validate/` — Input validation
- `internal/models/` — Shared data types

**Frontend** (`web/index.html`) — React 18 SPA using Babel standalone for in-browser JSX transpilation, styled with TailwindCSS via CDN.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/applications?network=` | List apps (cached) |
| GET | `/api/applications/{address}?network=` | Single app details |
| POST | `/api/applications/stake?network=` | Stake a new application |
| POST | `/api/applications/{address}/upstake` | Increase app stake |
| POST | `/api/applications/{address}/fund` | Transfer POKT to app |
| PUT | `/api/applications/{address}/autotopup?network=` | Set auto top-up config |
| DELETE | `/api/applications/{address}/autotopup?network=` | Remove auto top-up config |
| GET | `/api/autotopup?network=` | List auto top-up configs |
| GET | `/api/autotopup/events` | Recent auto top-up events |
| GET | `/api/bank?network=` | Bank account balance (cached) |
| GET | `/api/services?network=` | Available services on network |
| GET | `/api/networks` | Configured networks |
| GET | `/api/config` | Thresholds and network config |
| GET | `/health` | Health check |

Query param `?refresh=true` bypasses cache on read endpoints.

## Configuration

`config.yaml` defines networks with RPC/API endpoints, gateway addresses, bank address, and application addresses. Thresholds (`warning_threshold`, `danger_threshold`) are in uPOKT (1 POKT = 1,000,000 uPOKT).

## External Dependencies

- **Runtime:** `pocketd` binary in PATH (required only for write operations: stake/upstake/fund)
- **Go deps:** gorilla/mux, rs/cors, gopkg.in/yaml.v3
- **Frontend:** React 18, TailwindCSS, Babel standalone (all via CDN)

## Changelog

When adding features, fixing bugs, or making notable changes, update `CHANGELOG.md` under the `[Unreleased]` section. Use subsections: `Added`, `Fixed`, `Changed`, `Removed`.

## Key Patterns

- Read operations query Pocket Network API endpoints directly over HTTP; write operations shell out to `pocketd` CLI
- Application data is fetched in parallel using goroutines
- Cache is per-network; invalidated after 1 minute or on `?refresh=true`
- All amounts in the backend use uPOKT (int64); frontend converts to POKT for display
- No database — blockchain state via API/CLI, auto top-up configs in `autotopup.json`
- New staked apps are persisted to `config.yaml` via targeted line insertion (preserves comments/formatting)
- Auto top-up worker runs every 5 minutes; uses fund-then-upstake sequence from bank account
- Smart funding: worker skips the fund step if app's liquid balance already covers the needed amount
