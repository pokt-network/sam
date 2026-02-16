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
- **Stake new apps** — Stake a new application for any on-chain service directly from the UI
- **Upstake & Fund** — Increase application stakes or send POKT directly from the UI
- **Auto top-up** — Automatically fund and upstake applications when their stake drops below a configurable threshold
- **Auto-refresh** — Optional 60-second polling with manual refresh and keyboard shortcuts
- **Status indicators** — Configurable warning/danger thresholds for stake levels
- **Single binary** — No separate frontend build step; React SPA served from the Go server

## Quick Start

### Option 1: Docker (recommended)

```bash
git clone https://github.com/pokt-network/sam.git
cd sam

# Create your configuration
cp config.yaml.example config.yaml
# Edit config.yaml with your network endpoints and addresses

docker compose up
```

Open [http://localhost:9999](http://localhost:9999). The Docker image bundles `pocketd`, so write operations work out of the box.

### Option 2: From source

**Prerequisites:**
- **Go 1.25+**
- **pocketd** CLI in your `PATH` (required only for write operations):
  ```bash
  curl -sSL https://raw.githubusercontent.com/pokt-network/poktroll/main/tools/scripts/pocketd-install.sh | bash
  ```

```bash
git clone https://github.com/pokt-network/sam.git
cd sam
make install

cp config.yaml.example config.yaml
# Edit config.yaml with your network endpoints and addresses

make run
```

Open [http://localhost:9999](http://localhost:9999).

### Option 3: Pre-built binary

Download the latest release from [GitHub Releases](https://github.com/pokt-network/sam/releases). Each archive includes the `sam` binary and the `web/` directory.

```bash
tar -xzf sam-v*.tar.gz
cd sam-v*
cp config.yaml.example config.yaml
# Edit config.yaml
./sam
```

### Option 4: Helm (Kubernetes)

```bash
helm install sam oci://ghcr.io/pokt-network/charts/sam \
  --set config.networks.pocket.bank=pokt1your_bank_address \
  --set config.networks.pocket.applications[0]=pokt1your_app
```

See [Helm chart values](#helm-chart) for full configuration options.

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
make build              # Build binary → ./sam (version from VERSION file)
make build VERSION=1.0  # Build with explicit version override
make run                # Build and run (checks for pocketd)
make dev                # Hot reload via air (auto-installs if missing)
make test               # Run tests
make clean              # Remove build artifacts
make setup              # Install deps + initialize config.yaml
make check-pocketd      # Verify pocketd CLI is in PATH
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `9999` | HTTP server port |
| `CONFIG_FILE` | `config.yaml` | Path to the configuration file |
| `DATA_DIR` | `.` | Directory for runtime data (`autotopup.json`) |

```bash
PORT=8080 ./sam
# Or with custom paths (useful in containers):
CONFIG_FILE=/etc/sam/config.yaml DATA_DIR=/var/lib/sam ./sam
```

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `r` | Refresh data |
| `n` | Switch network |

### Staking a New Application

1. Click **"Stake New App"** in the header
2. Enter the application address (must already exist in the keyring)
3. Select a service from the dropdown (services are fetched from the network)
4. Enter the stake amount in POKT
5. Confirm — the application will be staked on-chain and automatically added to `config.yaml` for monitoring

### Auto Top-Up

Auto top-up automatically maintains application stakes above a minimum threshold by funding and upstaking from the bank account.

1. Click the cycle icon (↻) next to any application in the table
2. Set the **trigger threshold** — when stake falls below this value, a top-up is triggered
3. Set the **target amount** — stake will be increased to this level
4. Enable the toggle and save

The background worker checks all enabled applications every 5 minutes. When a top-up is triggered:

1. The worker queries the app's current stake and liquid balance
2. If the liquid balance doesn't cover the needed amount, the difference is funded from the bank
3. The app's stake is increased to the target amount via upstake

Auto top-up configs are persisted in `autotopup.json` and survive server restarts. Recent top-up events can be viewed via the `/api/autotopup/events` endpoint.

## API

All endpoints are prefixed with `/api` unless noted.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/applications?network=` | List all monitored applications |
| `GET` | `/api/applications/{address}?network=` | Single application details |
| `POST` | `/api/applications/stake?network=` | Stake a new application |
| `POST` | `/api/applications/{address}/upstake?network=` | Increase application stake |
| `POST` | `/api/applications/{address}/fund?network=` | Send POKT to application |
| `PUT` | `/api/applications/{address}/autotopup?network=` | Configure auto top-up for an app |
| `DELETE` | `/api/applications/{address}/autotopup?network=` | Remove auto top-up config |
| `GET` | `/api/autotopup?network=` | List all auto top-up configs |
| `GET` | `/api/autotopup/events` | Recent auto top-up activity log |
| `GET` | `/api/bank?network=` | Bank account balance |
| `GET` | `/api/services?network=` | Available services on the network |
| `GET` | `/api/networks` | Configured network names |
| `GET` | `/api/config` | Threshold configuration |
| `GET` | `/health` | Health check |

Add `?refresh=true` to any GET endpoint to bypass the 1-minute cache.

#### POST body (upstake / fund)

```json
{ "amount": 100.5 }
```

#### POST body (stake new application)

```json
{ "address": "pokt1abc...", "service_id": "anvil", "amount": 100 }
```

#### PUT body (auto top-up)

```json
{ "enabled": true, "trigger_threshold": 1000, "target_amount": 5000 }
```

`trigger_threshold` and `target_amount` are in POKT. The backend converts to uPOKT. `target_amount` must be greater than `trigger_threshold`.

All amounts in request bodies are in POKT (not uPOKT).

## Docker

### docker compose (local development)

```bash
# Start SAM with persistent data volume
docker compose up

# Rebuild after code changes
docker compose up --build

# Stop and remove containers (data volume persists)
docker compose down
```

The `docker-compose.yml` mounts your local `config.yaml` into the container and uses a named volume (`sam-data`) for `autotopup.json` persistence.

### Custom Docker build

```bash
# Build with specific pocketd version
docker build --build-arg POCKETD_VERSION=v0.1.31 --build-arg VERSION=1.0.0 -t sam .

# Run with custom config location
docker run -p 9999:9999 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v sam-data:/app/data \
  sam
```

## Helm Chart

The Helm chart deploys SAM to Kubernetes with a ConfigMap for configuration, a PersistentVolumeClaim for runtime data, and health probes.

### Install

```bash
# From OCI registry
helm install sam oci://ghcr.io/pokt-network/charts/sam

# From local chart
helm install sam charts/sam -f my-values.yaml
```

### Key values

| Value | Default | Description |
|-------|---------|-------------|
| `image.repository` | `ghcr.io/pokt-network/sam` | Container image |
| `image.tag` | Chart `appVersion` | Image tag |
| `service.type` | `ClusterIP` | Kubernetes service type |
| `service.port` | `9999` | Service port |
| `ingress.enabled` | `false` | Enable ingress resource |
| `persistence.enabled` | `true` | Enable PVC for runtime data |
| `persistence.size` | `100Mi` | PVC storage size |
| `resources.requests.memory` | `64Mi` | Memory request |
| `resources.limits.memory` | `128Mi` | Memory limit |
| `config` | *(see values.yaml)* | SAM configuration (rendered as `config.yaml`) |

The chart uses an init container to copy the ConfigMap-seeded `config.yaml` to the PVC, so SAM can write back to it (e.g., when staking new apps).

### Lint and template

```bash
helm lint charts/sam
helm template sam charts/sam
```

## CI/CD

GitHub Actions workflows are included:

- **CI** (`.github/workflows/ci.yml`) — Runs on push to main/master and PRs: `go vet`, `go test`, `go build`, Docker build (no push), `helm lint`
- **Release** (`.github/workflows/release.yml`) — Triggered by `v*` tags: builds cross-platform binaries (linux/darwin amd64+arm64, windows/amd64), publishes Docker image to GHCR, packages and pushes Helm chart to GHCR OCI registry, creates a GitHub Release with checksums

### Versioning

The `VERSION` file at the repo root is the single source of truth. All build artifacts (Go binary, Docker image, Helm chart) derive their version from it.

When bumping the version:

1. Update `VERSION`
2. Update `charts/sam/Chart.yaml` (`version` and `appVersion`)
3. Commit and push
4. Tag and release:

```bash
git tag v1.0.0
git push origin v1.0.0
```

CI checks that `VERSION` and `Chart.yaml` stay in sync. The release workflow validates the git tag matches the `VERSION` file.

## Architecture

```
cmd/web/main.go              → Entry point, server setup, CORS, graceful shutdown
internal/
├── autotopup/
│   ├── store.go              → Auto top-up config persistence (JSON file)
│   └── worker.go             → Background worker for periodic fund + upstake
├── config/config.go          → YAML config loading, validation, and persistence
├── handler/
│   ├── handler.go            → HTTP handlers (REST endpoints)
│   ├── routes.go             → Route registration
│   └── middleware.go         → Request logging, security headers
├── pocket/
│   ├── client.go             → Read-only HTTP queries to Pocket Network API
│   ├── pocketd.go            → pocketd CLI executor for write transactions
│   └── transactions.go       → Stake, upstake, and fund transaction logic
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
- Auto top-up configs stored in `autotopup.json` (no database required)
- Background worker checks stakes every 5 minutes and performs fund + upstake as needed
- New applications staked from the UI are automatically added to `config.yaml`

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
