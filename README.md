# Stanza Framework

[![CI](https://github.com/stanza-go/framework/actions/workflows/ci.yml/badge.svg)](https://github.com/stanza-go/framework/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/stanza-go/framework.svg)](https://pkg.go.dev/github.com/stanza-go/framework)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

The engine behind [Stanza](https://github.com/stanza-go) — an AI-native, batteries-included Go framework for single-binary applications.

## Packages

All packages live under `pkg/` with zero external dependencies — only Go's standard library and a vendored SQLite amalgamation.

| Package | Description |
|---------|-------------|
| `sqlite` | Vendored SQLite3 via CGo. Connection pooling, query builder, migrations. |
| `http` | Router, middleware, WebSocket, request/response helpers, Prometheus export. |
| `auth` | JWT access tokens, refresh tokens, API keys, middleware. |
| `queue` | SQLite-backed job queue with in-process workers. |
| `cron` | In-process cron scheduler. |
| `cache` | In-memory LRU cache with TTL. |
| `lifecycle` | DI container, service registry, startup/shutdown orchestration. |
| `config` | Environment variables, files, defaults, validation. |
| `cmd` | Command parser, flags, help generation. |
| `log` | Structured JSON logging with rotation. |
| `email` | Email client (Resend API). |
| `webhook` | Outbound webhook delivery with retries. |
| `validate` | Struct validation. |

## Usage

Stanza Framework is designed to be used through [stanza-go/standalone](https://github.com/stanza-go/standalone) — a fully built application you fork and customize. See the [documentation](https://github.com/stanza-go/docs) for guides and recipes.

```go
import "github.com/stanza-go/framework/pkg/http"
import "github.com/stanza-go/framework/pkg/sqlite"
```

## Development

Requires Go 1.26.1+ and CGo (for SQLite).

```bash
make check   # vet + lint + test
make test    # go test -race ./pkg/...
make bench   # benchmarks with -benchmem
make lint    # golangci-lint
make vet     # go vet
```

## License

MIT
