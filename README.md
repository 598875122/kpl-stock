# stock-kpl

Local Hermes HTTP adapter for the Kaipanla Quant API.

## Run

Set the upstream API key and start the local adapter:

```powershell
$env:KPL_API_KEY = "sk_pro_xxxxxxxx"
go run ./cmd/kpl-adapter
```

Defaults:

- `KPL_BASE_URL`: `http://124.222.49.67:3000`
- `KPL_LISTEN_ADDR`: `127.0.0.1:8787`
- `KPL_TIMEOUT_SECONDS`: `10`
- `KPL_CACHE_PATH`: `./data/kpl-cache.sqlite`

## Docker Compose

Create `.env` from the example and set your API key:

```powershell
Copy-Item .env.example .env
notepad .env
```

Start the adapter:

```powershell
docker compose up -d --build
```

The service is available on `http://127.0.0.1:8787`. SQLite is stored in the named Docker volume `stock-kpl_kpl-cache` at `/data/kpl-cache.sqlite` inside the container.

Useful commands:

```powershell
docker compose logs -f
docker compose down
docker compose down -v
```

## Hermes Calls

List tools:

```powershell
Invoke-RestMethod http://127.0.0.1:8787/v1/tools
```

Call a tool:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8787/v1/tools/market.sentiment `
  -ContentType application/json `
  -Body '{"arguments":{"date":"20260430"}}'
```
