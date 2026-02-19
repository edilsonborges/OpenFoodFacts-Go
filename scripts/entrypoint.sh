#!/bin/bash
set -e

echo "=== OFF Barcode Lookup Server (Go) ==="

if [ ! -f /data/food.parquet ]; then
    echo "ERROR: /data/food.parquet not found!"
    echo "Run: docker compose run --rm downloader"
    exit 1
fi

echo "Dataset: /data/food.parquet ($(du -h /data/food.parquet | cut -f1))"
mkdir -p /data/images

echo "Config:"
echo "  DuckDB:     ${DUCKDB_PATH:-/data/off.duckdb}"
echo "  Memory:     ${DUCKDB_MEMORY_LIMIT:-2GB}"
echo "  Threads:    ${DUCKDB_THREADS:-4}"
echo "  Cache Max:  ${MAX_IMAGE_CACHE_GB:-20}GB"
echo "  Port:       ${PORT:-8080}"
echo ""
echo "Starting..."

exec "$@"
