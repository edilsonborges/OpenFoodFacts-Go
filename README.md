# OFF Barcode Lookup Server

Servidor self-hosted de lookup de código de barras usando o dataset do [Open Food Facts](https://world.openfoodfacts.org/data).  
Feito em **Go** com **DuckDB**.

## Quick Start (Intel Mac / Linux)

```bash
# 1. Configure
cp .env.example .env
# Edite API_KEY

# 2. Suba tudo (baixa dataset ~4.5GB + inicia servidor)
docker compose up -d

# 3. Acompanhe o progresso
docker compose logs -f

# 4. Teste
curl http://localhost:8090/api/v1/stats

curl -H "X-API-Key: changeme" \
     http://localhost:8090/api/v1/product/7896051166566
```

## Endpoints

| Método | Path | Auth | Descrição |
|--------|------|------|-----------|
| GET | `/api/v1/stats` | Não | Health check e estatísticas |
| GET | `/api/v1/product/{barcode}` | Sim | Lookup por barcode |
| GET | `/api/v1/search?q={query}&limit={n}` | Sim | Busca por nome/marca |
| GET | `/api/v1/image/{barcode}/{type}/{res}` | Sim | Proxy de imagem com cache |
| POST | `/api/v1/dataset/refresh` | Sim | Atualizar dataset |

Auth via header: `X-API-Key: your-key`

## Estrutura

```
off-barcode-server/
├── cmd/server/main.go              # Entry point
├── internal/
│   ├── config/config.go            # Environment config
│   ├── database/duckdb.go          # DuckDB queries
│   ├── handler/handlers.go         # HTTP handlers
│   ├── imagecache/cache.go         # Image proxy/cache
│   ├── middleware/middleware.go     # Auth, logging, CORS
│   └── scheduler/refresh.go        # Background refresh
├── Dockerfile                      # Multi-stage Go build
├── docker-compose.yml              # Downloader + App
├── scripts/entrypoint.sh
└── PROMPT.md                       # Prompt técnico completo
```

## Performance

| Operação | Latência |
|----------|----------|
| Barcode lookup (indexado) | < 5ms |
| Busca por nome | 50-200ms |
| Imagem (cache hit) | < 5ms |
| Imagem (S3 first hit) | 500-2000ms |
| Docker image | ~50MB |

## Produção (TATOOINE)

Ver comentários no `docker-compose.yml` para bind mount e nginx-proxy.

## Licença dos Dados

- Database: [ODbL](https://opendatacommons.org/licenses/odbl/1.0/)
- Images: [CC BY-SA](https://creativecommons.org/licenses/by-sa/3.0/)
