# stock-kpl

Clean local SDK and CLI for Kaipanla Quant API calls used by Hermes cron jobs.

## Build

```bash
go build -o /usr/local/bin/kpl ./cmd/kpl
```

## Environment

```bash
export KPL_API_KEY=***
export KPL_BASE_URL=${KPL_BASE_URL:-http://124.222.49.67:3000}
export KPL_TIMEOUT_SECONDS=${KPL_TIMEOUT_SECONDS:-10}
export KPL_CACHE_PATH=${KPL_CACHE_PATH:-/root/kpl-stock/data/kpl-cache.sqlite}
```

## CLI

```bash
kpl tools
kpl call --tool stock.bigorder --args '{"code":"600183","date":"20260612"}'
kpl bigorder --codes 600183,600584,601138 --args '{"date":"20260612"}' --workers 8
kpl intraday --codes 600183,600584 --workers 8
kpl path --path /api/auction/limit-bid --query 'limit=500'
```

## Go SDK

Import `stock-kpl/pkg/kpllocal` inside Go jobs. Use `Call` for one request and `CallMany` for multi-stock concurrent requests.
