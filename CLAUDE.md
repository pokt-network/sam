# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

SAM (Simple AppStakes Manager) is a Go web application for managing Pocket Network application stakes. It combines a Go REST API backend with a React SPA frontend served from a single HTML file.

**Module:** `github.com/pokt-network/sam`

## Common Commands

```bash
make build          # Build binary → ./sam
make run            # Build and run (checks for pocketd)
make dev            # Hot reload via air (auto-installs if missing)
make test           # go test -v ./...
make clean          # Remove sam binary and tmp/
make setup          # Install deps + initialize config.yaml
make check-pocketd  # Verify pocketd CLI is in PATH
```

Run with custom port: `PORT=8080 ./sam` (default: 9999)

## Architecture

**Monolithic single-file backend** (`cmd/web/main.go`, ~780 lines) organized in sections:
1. **Models** — Config, Application, BankAccount, StakeRequest, TransactionResponse, API response structs
2. **Global State** — In-memory caches (`appCache`, `bankCache`) with 1-minute TTL, keyed by network name
3. **Configuration** — Loads `config.yaml` (networks, thresholds, keyring settings)
4. **pocketd Helpers** — Shells out to `pocketd` CLI for upstake/fund transactions
5. **API Query Functions** — Direct HTTP calls to Pocket Network REST API endpoints (no CLI for reads)
6. **HTTP Handlers** — Gorilla Mux routes under `/api` prefix
7. **Router Setup** — CORS-enabled, serves `web/index.html` at root

**Frontend** (`web/index.html`) — React 18 SPA using Babel standalone for in-browser JSX transpilation, styled with TailwindCSS via CDN.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/applications?network=` | List apps (cached) |
| GET | `/api/applications/{address}?network=` | Single app details |
| POST | `/api/applications/{address}/upstake` | Increase app stake |
| POST | `/api/applications/{address}/fund` | Transfer POKT to app |
| GET | `/api/bank?network=` | Bank account balance (cached) |
| GET | `/api/networks` | Configured networks |
| GET | `/api/config` | Thresholds and network config |
| GET | `/health` | Health check |

Query param `?refresh=true` bypasses cache on read endpoints.

## Configuration

`config.yaml` defines networks with RPC/API endpoints, gateway addresses, bank address, and application addresses. Thresholds (`warning_threshold`, `danger_threshold`) are in uPOKT (1 POKT = 1,000,000 uPOKT).

## External Dependencies

- **Runtime:** `pocketd` binary in PATH (required only for write operations: upstake/fund)
- **Go deps:** gorilla/mux, rs/cors, gopkg.in/yaml.v3
- **Frontend:** React 18, TailwindCSS, Babel standalone (all via CDN)

## Key Patterns

- Read operations query Pocket Network API endpoints directly over HTTP; write operations shell out to `pocketd` CLI
- Application data is fetched in parallel using goroutines
- Cache is per-network; invalidated after 1 minute or on `?refresh=true`
- All amounts in the backend use uPOKT (int64); frontend converts to POKT for display
- No database — all state comes from the blockchain via API/CLI
