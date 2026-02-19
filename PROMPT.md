# Prompt Técnico — OFF Barcode Lookup Server (Go)

> **Contexto:** Você é um desenvolvedor sênior especializado em Go, DuckDB, e infraestrutura Docker. Você vai desenvolver um **servidor de lookup de código de barras** para o app **Estoque Doméstico** (iOS/Swift). O objetivo é eliminar a dependência da API pública do Open Food Facts e ter resposta instantânea (<50ms) nos scans de barcode.

---

## 1. Visão Geral da Arquitetura

### Problema atual
- A API pública do Open Food Facts tem rate limit de 100 req/min e latência de 200-800ms
- Para um app de inventário doméstico, o barcode scan precisa ser instantâneo
- Imagens de produtos não estão disponíveis offline

### Solução
Um servidor self-hosted em **Go** que:
1. Usa o **dataset Parquet do Open Food Facts** (Hugging Face, ~4.5GB) como fonte de dados
2. Usa **DuckDB** (via `go-duckdb`) para queries instantâneas por barcode (~5-10ms)
3. Implementa um **proxy/cache de imagens** que baixa sob demanda do AWS S3 do OFF
4. Expõe uma **REST API** simples para o app iOS consumir
5. Roda em **Docker** no servidor TATOOINE (Intel, Ubuntu)
6. Produz um **binário único** (~15MB) com zero dependências externas em runtime

### Stack
- **Linguagem:** Go 1.23+
- **Query Engine:** DuckDB via `github.com/marcboeker/go-duckdb`
- **HTTP Router:** `net/http` stdlib (Go 1.22+ com enhanced routing)
- **Logging:** `log/slog` (stdlib)
- **Container:** Docker multi-stage (golang → scratch/alpine)

### Fluxo do Scan no App
```
[Usuário escaneia barcode]
        │
        ▼
[Cache local no device (SQLite/SwiftData)]
        │ miss
        ▼
[Sua API → DuckDB + Parquet] ← ~10-20ms (inclui rede local/VPN)
        │ miss
        ▼
[Fallback: Bluesoft Cosmos API] ← ~200-500ms (produtos BR não catalogados no OFF)
        │ miss
        ▼
[Último recurso: GPT-5 Nano] ← ~1-2s (gera dados mínimos a partir do barcode)
```

---

## 2. Fontes de Dados do Open Food Facts

### 2.1 Dataset Parquet (Dados estruturados)
- **URL:** `https://huggingface.co/datasets/openfoodfacts/product-database/resolve/main/food.parquet?download=true`
- **Tamanho:** ~4.5GB (download)
- **Conteúdo:** Versão simplificada do JSONL dump, sem campos de debug, colunas organizadas (columnar format)
- **Atualização:** Diária pelo Open Food Facts
- **Licença:** Open Database License (ODbL)

### 2.2 Imagens (AWS S3)
- **Bucket:** `openfoodfacts-images` (região `eu-west-3`)
- **Acesso:** Público, sem autenticação
- **Total:** 3M+ produtos, 7M+ imagens
- **Resoluções disponíveis:** 100px, 200px, 400px, full
- **URL pattern:** `https://openfoodfacts-images.s3.eu-west-3.amazonaws.com/data/{barcode_path}/{image_file}`
- **Lista de arquivos:** `https://openfoodfacts-images.s3.eu-west-3.amazonaws.com/data/data_keys.gz`

### 2.3 Estrutura de URLs de Imagens
O barcode determina o path no S3. A regra é:
- Barcodes com **mais de 8 dígitos:** dividir em grupos de 3/3/3/restante
  - Ex: `3435660768163` → `343/566/076/8163`
  - URL: `.../data/343/566/076/8163/{image}.jpg`
- Barcodes com **8 ou menos dígitos:** usar diretamente
  - Ex: `12345678` → `.../data/12345678/{image}.jpg`

#### Tipos de imagem por produto
Cada produto pode ter:
- **Imagens raw** (numéricas): `1.jpg`, `2.jpg`, `3.jpg` — fotos enviadas por contribuidores
- **Imagens selecionadas** (named): `front_pt.{rev}.{resolution}.jpg`, `ingredients_pt.{rev}.400.jpg`, `nutrition_pt.{rev}.400.jpg`

#### Campos no dataset que definem as imagens
No Parquet/JSONL, o campo `images` é um JSON com esta estrutura:
```json
{
  "1": {
    "sizes": { "full": {"w": 850, "h": 1200}, "100": {...}, "400": {...} },
    "uploader": "kiliweb",
    "uploaded_t": "1527184614"
  },
  "front_fr": {
    "imgid": "1",
    "rev": "4",
    "sizes": { "200": {...}, "full": {...}, "400": {...}, "100": {...} }
  }
}
```

#### Construção da URL da imagem (Go)
```go
func buildBarcodePath(barcode string) string {
    if len(barcode) > 8 {
        // 3435660768163 → 343/566/076/8163
        return fmt.Sprintf("%s/%s/%s/%s",
            barcode[:3], barcode[3:6], barcode[6:9], barcode[9:])
    }
    return barcode
}

func buildS3ImageURL(barcode, imageType string, resolution int) string {
    base := "https://openfoodfacts-images.s3.eu-west-3.amazonaws.com/data"
    folder := buildBarcodePath(barcode)
    // For raw images (numeric): 1.400.jpg
    // For selected: front_pt.{rev}.400.jpg
    filename := fmt.Sprintf("1.%d.jpg", resolution) // fallback to raw image 1
    return fmt.Sprintf("%s/%s/%s", base, folder, filename)
}
```

### 2.4 Campos Relevantes do Parquet para o Estoque Doméstico

| Campo | Tipo | Descrição | Uso no app |
|-------|------|-----------|------------|
| `code` | string | Barcode (EAN/UPC) | **Chave primária de lookup** |
| `product_name` | string | Nome do produto | Exibição principal |
| `brands` | string | Marca(s) | Exibição |
| `categories_tags` | list[string] | Categorias (tags) | Categorização automática |
| `countries_tags` | list[string] | Países onde é vendido | Filtro BR |
| `image_front_url` | string | URL da imagem frontal | Cache de imagem |
| `image_front_small_url` | string | URL da thumbnail | Lista rápida |
| `nutriments` | struct | Dados nutricionais | Info nutricional |
| `nutriscore_grade` | string | Nutri-Score (a-e) | Badge visual |
| `nova_group` | int | Grau de processamento (1-4) | Badge visual |
| `quantity` | string | Peso/volume | Info do produto |
| `serving_size` | string | Porção | Info nutricional |
| `allergens_tags` | list[string] | Alérgenos | Alertas |
| `labels_tags` | list[string] | Selos (orgânico, etc) | Badges |
| `stores_tags` | list[string] | Lojas onde é vendido | Contexto BR |
| `images` | string/json | Metadados de todas as imagens | Construção de URLs |
| `last_modified_t` | int | Timestamp última modificação | Sincronização delta |

---

## 3. Especificação da API REST

### 3.1 GET /api/v1/product/{barcode}
Lookup principal por código de barras.

**Response 200:**
```json
{
  "code": "7891000100103",
  "product_name": "Leite Integral",
  "brands": "Ninho",
  "quantity": "1L",
  "nutriscore_grade": "b",
  "nova_group": 1,
  "categories": ["Laticínios", "Leites"],
  "allergens": ["en:milk"],
  "labels": ["en:organic"],
  "nutriments": {
    "energy_kcal_100g": 64,
    "fat_100g": 3.5,
    "carbohydrates_100g": 4.9,
    "proteins_100g": 3.1
  },
  "image_url": "/api/v1/image/7891000100103/front/400",
  "image_thumb_url": "/api/v1/image/7891000100103/front/200",
  "source": "openfoodfacts",
  "last_modified": "2025-01-15T10:30:00Z"
}
```

**Response 404:**
```json
{
  "code": "0000000000000",
  "found": false,
  "message": "Product not found in local database"
}
```

### 3.2 GET /api/v1/image/{barcode}/{type}/{resolution}
Proxy de imagens com cache local.

- `type`: `front`, `ingredients`, `nutrition`, ou ID numérico raw
- `resolution`: `100`, `200`, `400`

### 3.3 GET /api/v1/search?q={query}&limit={n}
Busca por nome/marca.

### 3.4 GET /api/v1/stats
Status do servidor e dataset (sem auth).

### 3.5 POST /api/v1/dataset/refresh
Trigger manual para baixar dataset atualizado do Hugging Face.

---

## 4. Implementação Técnica

### 4.1 DuckDB em Go

```go
import "github.com/marcboeker/go-duckdb"

// Abrir conexão
db, err := sql.Open("duckdb", "/data/off.duckdb")

// Importar Parquet (primeira vez)
db.Exec(`
    CREATE TABLE IF NOT EXISTS products AS
    SELECT
        CAST(code AS VARCHAR) AS code,
        product_name, brands, categories_tags, countries_tags,
        image_front_url, image_front_small_url,
        nutriscore_grade, nova_group, quantity, serving_size,
        allergens_tags, labels_tags, stores_tags,
        nutriments, images, last_modified_t
    FROM read_parquet('/data/food.parquet')
    WHERE code IS NOT NULL AND code != '' AND length(code) >= 4
`)

// Índice
db.Exec("CREATE INDEX IF NOT EXISTS idx_code ON products(code)")

// Lookup (< 5ms com índice)
row := db.QueryRow("SELECT * FROM products WHERE code = ?", barcode)
```

### 4.2 Estrutura do Projeto Go
```
off-barcode-server/
├── cmd/
│   └── server/
│       └── main.go           # Entry point
├── internal/
│   ├── config/
│   │   └── config.go         # Env vars / configuração
│   ├── database/
│   │   └── duckdb.go         # DuckDB connection + queries
│   ├── handler/
│   │   ├── product.go        # GET /product/{barcode}
│   │   ├── search.go         # GET /search
│   │   ├── image.go          # GET /image/{barcode}/{type}/{res}
│   │   └── stats.go          # GET /stats
│   ├── imagecache/
│   │   └── cache.go          # Download + cache de imagens S3
│   ├── middleware/
│   │   └── apikey.go         # API key auth
│   └── scheduler/
│       └── refresh.go        # Background dataset refresh
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
└── scripts/
    └── entrypoint.sh
```

### 4.3 Proxy/Cache de Imagens

```go
// Fluxo:
// 1. Check /data/images/{barcode_path}/front_400.jpg
// 2. Se não existe → download do S3
// 3. Salva no disco + serve ao cliente
// 4. Cache-Control: public, max-age=2592000

func (c *ImageCache) GetImage(barcode, imgType string, res int) ([]byte, error) {
    localPath := c.localPath(barcode, imgType, res)
    
    // Cache hit
    if data, err := os.ReadFile(localPath); err == nil {
        return data, nil
    }
    
    // Cache miss → download
    s3URL := buildS3ImageURL(barcode, imgType, res)
    resp, err := http.Get(s3URL)
    if err != nil || resp.StatusCode != 200 {
        // Tentar fallback: image_front_url do dataset
        return c.tryFallback(barcode, imgType, res)
    }
    defer resp.Body.Close()
    
    data, _ := io.ReadAll(resp.Body)
    
    // Salvar no cache
    os.MkdirAll(filepath.Dir(localPath), 0755)
    os.WriteFile(localPath, data, 0644)
    
    return data, nil
}
```

---

## 5. Configuração Docker

### 5.1 Variáveis de Ambiente
```env
PARQUET_URL=https://huggingface.co/datasets/openfoodfacts/product-database/resolve/main/food.parquet?download=true
DUCKDB_PATH=/data/off.duckdb
DUCKDB_MEMORY_LIMIT=2GB
DUCKDB_THREADS=4
IMAGE_CACHE_PATH=/data/images
MAX_IMAGE_CACHE_GB=20
IMAGE_CACHE_TTL_DAYS=90
S3_BASE_URL=https://openfoodfacts-images.s3.eu-west-3.amazonaws.com/data
PRECACHE_BRAZILIAN=false
PORT=8080
API_KEY=sua-chave-secreta-aqui
```

### 5.2 Volumes Persistentes
```
/data/
├── food.parquet          # Dataset (~4.5GB)
├── off.duckdb            # Banco DuckDB indexado (~2-3GB)
└── images/               # Cache de imagens (cresce com uso)
    └── 789/100/010/0103/
        ├── front_400.jpg
        └── front_200.jpg
```

### 5.3 Requisitos de Hardware (TATOOINE)
- **Disco:** ~30GB mínimo livre
- **RAM:** 2-4GB alocados para DuckDB
- **CPU:** Qualquer Intel x86_64

---

## 6. Estimativas de Performance

| Operação | Latência | Notas |
|----------|----------|-------|
| Lookup barcode (DuckDB indexado) | < 5ms | Após warm-up |
| Busca por nome (ILIKE) | 50-200ms | Full scan |
| Imagem do cache | < 5ms | Filesystem |
| Imagem cold (S3) | 500-2000ms | Primeira vez |
| Startup do servidor | ~30s | Import Parquet na 1ª vez: ~3-5min |
| Docker image size | ~30-50MB | Multi-stage com alpine |

---

## 7. Referências
- Open Food Facts Data: https://world.openfoodfacts.org/data
- Parquet (Hugging Face): https://huggingface.co/datasets/openfoodfacts/product-database
- API Docs: https://openfoodfacts.github.io/documentation/docs/Product-Opener/api/
- AWS Images: https://openfoodfacts.github.io/openfoodfacts-server/api/aws-images-dataset/
- Image URLs: https://openfoodfacts.github.io/openfoodfacts-server/api/how-to-download-images/
- go-duckdb: https://github.com/marcboeker/go-duckdb
- DuckDB + OFF Blog: https://blog.openfoodfacts.org/en/news/food-transparency-in-the-palm-of-your-hand-explore-the-largest-open-food-database-using-duckdb
