# pgwarden

pgwarden is a lightweight Docker tool for PostgreSQL backup and restore. It uses `pg_dump` to create backups, encrypts them with GPG, and stores them in any S3-compatible storage (AWS S3, Cloudflare R2, MinIO, Backblaze, DigitalOcean, GCS, etc.).

**Why pgwarden?**

- **Docker-first**: Single container, runs anywhere Docker runs
- **Mandatory encryption**: All backups encrypted with GPG AES-256, no plaintext option
- **Non-root by default**: Runs as UID 1000, no privilege escalation
- **Lightweight**: Alpine-based image, ~64MB
- **S3-compatible**: Works with any S3-compatible storage, not just AWS
- **Simple**: One binary, no complex configuration, no dependencies
- **HTTP API**: Trigger backup/restore from web apps
- **Health check**: Built-in `/health` endpoint for monitoring

## Features

- **pg_dump backup**: Custom format (-Fc), compressed
- **S3-compatible storage**: AWS S3, R2, MinIO, Backblaze, DigitalOcean, GCS, etc.
- **Restore**: pg_restore from S3 with one command
- **HTTP API**: For integration with web apps
- **CLI**: Manual backup, restore, list, delete
- **Mandatory GPG encryption**: All backups encrypted with AES-256 (no plaintext option)
- **Non-root user**: Runs as `pgwarden` (UID 1000), no privilege escalation
- **Lightweight image**: Alpine-based Docker image, ~64MB
- **Built-in health check**: HTTP `/health` endpoint for Docker HEALTHCHECK
- **OCI labels**: Container metadata for registries

## Quick Start

### Prerequisites

- Docker installed
- PostgreSQL accessible from Docker (host network or Docker network)
- S3-compatible storage (AWS S3, R2, MinIO, etc.)

### Single Docker Command

```bash
docker run --rm --network host \
  -e PGHOST=localhost \
  -e PGPORT=5432 \
  -e PGUSER=postgres \
  -e PGPASSWORD=secret \
  -e PGDATABASE=mydb \
  -e S3_ENDPOINT=s3.amazonaws.com \
  -e S3_BUCKET=my-backups \
  -e S3_PREFIX=pgwarden \
  -e S3_ACCESS_KEY=xx \
  -e S3_SECRET_KEY=xx \
  -e S3_REGION=auto \
  -e GPG_PASSPHRASE=$(openssl rand -base64 32) \
  pgwarden:distroless backup
```

### Docker Compose

Create `docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: mydb
      POSTGRES_PASSWORD: secret
    ports:
      - "5432:5432"

  pgwarden:
    build: .
    container_name: pgwarden
    depends_on:
      postgres:
        condition: service_healthy
    env_file:
      - .env
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD", "pgwarden", "health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

Run with:

```bash
# Set env vars (copy from .env.example)
cp .env.example .env
# Edit .env with your credentials

# Start services
docker compose up -d

# Run backup
docker compose exec pgwarden pgwarden backup
```

### Local Binary (Development)

```bash
# Build
go build -o pgwarden ./cmd/pgwarden

# Set env vars (copy from .env.example)
cp .env.example .env
# Edit .env with your credentials

# Run
./pgwarden backup
```

## CLI Usage

```bash
# Create backup
pgwarden backup

# List all backups
pgwarden list

# Restore from backup
pgwarden restore --id 20260726_071210

# Delete backup
pgwarden delete --id 20260726_071210

# Start HTTP API server
pgwarden serve

# Health check
pgwarden health

# Show help
pgwarden --help
pgwarden backup --help
```

## HTTP API

| Method   | Endpoint        | Description         | Auth |
| -------- | --------------- | ------------------- | ---- |
| `GET`    | `/health`       | Health check        | No   |
| `POST`   | `/backup`       | Create backup       | Yes  |
| `POST`   | `/restore`      | Restore from backup | Yes  |
| `GET`    | `/backups`      | List all backups    | Yes  |
| `DELETE` | `/backups/{id}` | Delete backup       | Yes  |

### Examples

```bash
# Health check
curl http://localhost:8080/health
# {"status":"ok"}

# Create backup
curl -X POST http://localhost:8080/backup
# {"id":"20260726_071210","size":151018,"duration":"25.879631304s","checksum":"...","created_at":"..."}

# List backups
curl http://localhost:8080/backups
# [{"key":"pgwarden/backups/2026/07/20260726_071210.dump.gpg","size":151018,"modified":"..."}]

# Restore from backup
curl -X POST http://localhost:8080/restore -H "Content-Type: application/json" -d '{"id":"20260726_071210"}'
# {"status":"completed","id":"20260726_071210"}

# Delete backup
curl -X DELETE http://localhost:8080/backups/20260726_071210
# {"deleted":true,"id":"20260726_071210"}

# With auth
curl -H "Authorization: Bearer your-api-secret" http://localhost:8080/backups
```

## Environment Variables

### PostgreSQL

| Variable     | Default     | Description         |
| ------------ | ----------- | ------------------- |
| `PGHOST`     | `localhost` | PostgreSQL host     |
| `PGPORT`     | `5432`      | PostgreSQL port     |
| `PGUSER`     | `postgres`  | PostgreSQL user     |
| `PGPASSWORD` | —           | PostgreSQL password |
| `PGDATABASE` | `postgres`  | PostgreSQL database |

### S3 Storage

| Variable        | Default            | Description                      |
| --------------- | ------------------ | -------------------------------- |
| `S3_ENDPOINT`   | `s3.amazonaws.com` | S3 endpoint hostname             |
| `S3_BUCKET`     | —                  | S3 bucket name (required)        |
| `S3_PREFIX`     | `pgwarden`         | S3 key prefix for backups        |
| `S3_REGION`     | —                  | S3 region (auto-detect if empty) |
| `S3_ACCESS_KEY` | —                  | S3 access key                    |
| `S3_SECRET_KEY` | —                  | S3 secret key                    |
| `S3_USE_SSL`    | `true`             | Use SSL for S3 connections       |

### Security

| Variable         | Default | Description                          |
| ---------------- | ------- | ------------------------------------ |
| `GPG_PASSPHRASE` | —       | GPG encryption passphrase (required) |
| `API_SECRET`     | —       | API auth secret (empty = no auth)    |

### API Server

| Variable   | Default | Description          |
| ---------- | ------- | -------------------- |
| `API_PORT` | `8080`  | HTTP API server port |

## Architecture

### Backup Flow

```
┌─────────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│                 │     │             │     │             │     │             │
│   pgwarden      │────▶│  pg_dump    │────▶│  GPG        │────▶│  S3/R2/MinIO│
│   (Docker)      │     │  (pipe)     │     │  encrypt    │     │  upload     │
│                 │     │             │     │  (AES-256)  │     │             │
└─────────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
         ▲                                                           │
         │                                                           │
         └──────────── CLI or HTTP API ─────────────────────────────┘
```

### Restore Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│             │     │             │     │             │     │             │
│  S3/R2/     │────▶│  Download   │────▶│  GPG        │────▶│  pg_restore │
│  MinIO      │     │  .dump.gpg  │     │  decrypt    │     │  (pipe)     │
│             │     │             │     │             │     │             │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

### Backup File Format

All backups are stored as `.dump.gpg` files:

```
backups/
├── 2026/
│   └── 07/
│       ├── 20260726_071210.dump.gpg
│       ├── 20260726_081200.dump.gpg
│       └── 20260726_091200.dump.gpg
└── 2026/
    └── 08/
        └── ...
```

**File naming:** `backups/{YYYY}/{MM}/{YYYYMMDD_HHMMSS}.dump.gpg`

**File format:** pg_dump custom format (`-Fc`), encrypted with GPG AES-256

## Security

### Encryption

**All backups are encrypted** using GPG symmetric encryption with AES-256:

- **Algorithm:** AES-256 (symmetric)
- **Key derivation:** GPG standard (S2K)
- **Format:** GPG binary format
- **No plaintext option** — `GPG_PASSPHRASE` is mandatory

```bash
# Backup is encrypted in transit and at rest
pgwarden backup
# → Creates .dump.gpg file in S3
```

### Non-Root User

pgwarden runs as a non-root user by default:

- **User:** `pgwarden` (UID 1000)
- **Group:** `pgwarden` (GID 1000)
- **No shell:** `/sbin/nologin`
- **Home:** `/home/pgwarden`

### API Authentication

The HTTP API supports optional authentication:

```bash
# Without auth (development only)
docker run pgwarden serve

# With auth
docker run -e API_SECRET=my-secret pgwarden serve

# All requests must include:
curl -H "Authorization: Bearer my-secret" http://localhost:8080/backups
```

### Best Practices

1. **Generate strong passphrases:**

   ```bash
   GPG_PASSPHRASE=$(openssl rand -base64 32)
   ```

2. **Store secrets securely:**
   - Use Docker secrets, Kubernetes secrets, or `.env` files (not in code)
   - Never commit `.env` files to Git

3. **Use HTTPS for S3:**
   - Set `S3_USE_SSL=true` (default)
   - Use cloud provider TLS endpoints

4. **Rotate passphrases periodically:**
   - Old backups will still be decryptable with old passphrase
   - New backups use new passphrase

5. **Backup your passphrase:**
   - Store in password manager
   - Keep offline backup in secure location
   - **If lost, backups are unrecoverable**

## Image Details

### Base

- **Base image:** `alpine:3.20`
- **Final image:** ~64MB (compressed)
- **Runtime:** pg_dump, pg_restore, gpg

### Components

| Component         | Source            | Version |
| ----------------- | ----------------- | ------- |
| Go binary         | Build from source | 1.25+   |
| PostgreSQL client | Alpine packages   | 16.x    |
| GPG               | Alpine packages   | 2.4+    |
| CA certificates   | Alpine            | Latest  |

### OCI Labels

```yaml
org.opencontainers.image.title: pgwarden
org.opencontainers.image.description: PostgreSQL backup guardian
org.opencontainers.image.version: dev
org.opencontainers.image.source: https://github.com/rfqma/pgwarden
org.opencontainers.image.licenses: Proprietary
```

### Health Check

```bash
# Docker automatically checks:
pgwarden health
# Returns: {"status":"ok"}
```

## Development

### Build from Source

```bash
# Clone repository
git clone https://github.com/rfqma/pgwarden.git
cd pgwarden

# Build binary
go build -o pgwarden ./cmd/pgwarden

# Run
./pgwarden backup
```

### Build Docker Image

```bash
# Build local image
docker build -t pgwarden:dev .

# Test
docker run --rm --network host --env-file .env pgwarden:dev backup
```

## License

Proprietary — All rights reserved.
