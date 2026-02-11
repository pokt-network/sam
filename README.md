<div align="center">
  <a href="https://www.pokt.network">
    <img src="https://user-images.githubusercontent.com/16605170/74199287-94f17680-4c18-11ea-9de2-b094fab91431.png" alt="Pocket Network logo" width="340"/>
  </a>
</div>

# SAM — Simple AppStakes Manager

A lightweight web application for monitoring and managing [Pocket Network](https://www.pokt.network/) application stakes. SAM pairs a Go REST API with a React single-page frontend served from a single binary.

<div>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white"/></a>
  <a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-blue.svg"/></a>
  <a href="https://github.com/pokt-network/sam/pulls"><img src="https://img.shields.io/github/issues-pr/pokt-network/sam.svg"/></a>
  <a href="https://github.com/pokt-network/sam/issues"><img src="https://img.shields.io/github/issues/pokt-network/sam.svg"/></a>
</div>

## Features

- **Dashboard** — View all application stakes, balances, and status at a glance
- **Multi-network** — Manage applications across multiple Pocket Network chains
- **Upstake & Fund** — Increase application stakes or send POKT directly from the UI
- **Auto-refresh** — Optional 60-second polling with manual refresh and keyboard shortcuts
- **Status indicators** — Configurable warning/danger thresholds for stake levels
- **Single binary** — No separate frontend build step; React SPA served from the Go server

## Quick Start

### Prerequisites

- **Go 1.25+**
- **pocketd** CLI in your `PATH` (required only for write operations)

Install pocketd:

```bash
curl -sSL https://raw.githubusercontent.com/pokt-network/poktroll/main/tools/scripts/pocketd-install.sh | bash
```

### Setup

```bash
# Clone and install dependencies
git clone https://github.com/pokt-network/sam.git
cd sam
make install

# Create your configuration
cp config.yaml.example config.yaml
# Edit config.yaml with your network endpoints and addresses

# Build and run
make run
```

Open [http://localhost:9999](http://localhost:9999) in your browser.

## Configuration

Copy `config.yaml.example` and fill in your values:

```yaml
config:
  keyring-backend: test
  thresholds:
    warning_threshold: 2000000000  # 2000 POKT (in uPOKT)
    danger_threshold: 1000000000   # 1000 POKT (in uPOKT)

  networks:
    pocket:
      rpc_endpoint: https://sauron-rpc.infra.pocket.network/
      api_endpoint: https://sauron-api.infra.pocket.network/
      gateways:
        - pokt1your_gateway_address_here
      bank: pokt1your_bank_address_here
      applications:
        - pokt1your_app_address_1
        - pokt1your_app_address_2
```

| Field | Description |
|-------|-------------|
| `keyring-backend` | Cosmos keyring backend (`test`, `file`, `os`, `kwallet`, `pass`) |
| `pocketd-home` | Optional custom pocketd home directory |
| `thresholds` | Stake levels (uPOKT) that trigger warning/danger status in the UI |
| `rpc_endpoint` | Pocket Network RPC endpoint (used for write transactions). Public Sauron mainnet endpoints are provided by default — replace with your own if you have dedicated infrastructure |
| `api_endpoint` | Pocket Network REST API endpoint (used for read queries) |
| `bank` | Address that funds applications (must have keys in keyring) |
| `applications` | List of application addresses to monitor |
| `gateways` | Gateway addresses associated with your applications |

All amounts are in **uPOKT** (1 POKT = 1,000,000 uPOKT).

## Usage

### Make Commands

```bash
make build          # Build binary → ./sam
make run            # Build and run (checks for pocketd)
make dev            # Hot reload via air (auto-installs if missing)
make test           # Run tests
make clean          # Remove build artifacts
make setup          # Install deps + initialize config.yaml
make check-pocketd  # Verify pocketd CLI is in PATH
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `9999` | HTTP server port |

```bash
PORT=8080 ./sam
```

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `r` | Refresh data |
| `n` | Switch network |

## API

All endpoints are prefixed with `/api` unless noted.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/applications?network=` | List all monitored applications |
| `GET` | `/api/applications/{address}?network=` | Single application details |
| `POST` | `/api/applications/{address}/upstake?network=` | Increase application stake |
| `POST` | `/api/applications/{address}/fund?network=` | Send POKT to application |
| `GET` | `/api/bank?network=` | Bank account balance |
| `GET` | `/api/networks` | Configured network names |
| `GET` | `/api/config` | Threshold configuration |
| `GET` | `/health` | Health check |

Add `?refresh=true` to any GET endpoint to bypass the 1-minute cache.

#### POST body (upstake / fund)

```json
{ "amount": 100.5 }
```

Amount is in POKT (not uPOKT).

## Architecture

```
cmd/web/main.go              → Entry point, server setup, CORS, graceful shutdown
internal/
├── config/config.go          → YAML config loading and validation
├── handler/
│   ├── handler.go            → HTTP handlers (REST endpoints)
│   ├── routes.go             → Route registration
│   └── middleware.go         → Request logging, security headers
├── pocket/
│   ├── client.go             → Read-only HTTP queries to Pocket Network API
│   ├── pocketd.go            → pocketd CLI executor for write transactions
│   └── transactions.go       → Upstake and fund transaction logic
├── validate/validate.go      → Input validation (addresses, amounts, service IDs)
├── cache/cache.go            → Generic in-memory cache with TTL
└── models/models.go          → Shared data types
web/index.html                → React 18 SPA (Babel + TailwindCSS via CDN)
```

**Key design decisions:**
- Read operations query the Pocket Network REST API directly over HTTP
- Write operations shell out to the `pocketd` CLI binary
- Application data is fetched in parallel using goroutines
- In-memory cache per network with 1-minute TTL
- No database — all state comes from the blockchain

## Running the Tests

```bash
make test
```

## Security

SAM is designed to run on a local machine or behind an authenticated reverse proxy. It does **not** implement application-level authentication or rate limiting.

Hardening measures included:

- **Input validation** — Addresses, amounts, and service IDs validated against strict patterns
- **YAML injection prevention** — Service IDs from API responses are validated before YAML interpolation
- **Integer overflow protection** — Stake calculations checked for int64 overflow
- **Error sanitization** — Internal errors logged server-side; generic messages returned to clients
- **Environment isolation** — Subprocess commands receive a minimal environment (HOME, PATH only)
- **Security headers** — X-Content-Type-Options, X-Frame-Options, Referrer-Policy, Permissions-Policy
- **CORS restriction** — Limited to localhost on the configured port
- **Body size limits** — POST request bodies capped at 1 KB
- **SRI hashes** — CDN scripts pinned to exact versions with subresource integrity
- **Config validation** — Addresses, endpoints, and keyring backend validated at startup
- **Response size limits** — API response reads capped at 1 MB
- **Redirect restriction** — HTTP client only follows same-host redirects

## Contributing

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on contributions and the process of submitting pull requests.

## Support & Contact

<div>
  <a href="https://twitter.com/poktnetwork"><img src="https://img.shields.io/twitter/url/http/shields.io.svg?style=social"/></a>
  <a href="https://t.me/POKTnetwork"><img src="https://img.shields.io/badge/Telegram-blue.svg"/></a>
  <a href="https://discord.gg/AKp8eMt"><img src="https://img.shields.io/badge/Discord-purple.svg"/></a>
</div>

## License

This project is licensed under the MIT License; see the [LICENSE.md](LICENSE.md) file for details.
