#!/bin/bash
set -e

echo "=== OFF Barcode Lookup Server (Go) ==="

if [ -f /data/food.parquet ]; then
    echo "Dataset: /data/food.parquet ($(du -h /data/food.parquet | cut -f1))"
else
    echo "WARN: /data/food.parquet not found — starting with empty OFF dataset"
    echo "      Custom products will work. Run downloader to import OFF data."
fi

mkdir -p /data/images

echo "Config:"
echo "  DuckDB:     ${DUCKDB_PATH:-/data/off.duckdb}"
echo "  SQLite:     ${SQLITE_PATH:-/data/app.db}"
echo "  Memory:     ${DUCKDB_MEMORY_LIMIT:-2GB}"
echo "  Threads:    ${DUCKDB_THREADS:-4}"
echo "  Cache Max:  ${MAX_IMAGE_CACHE_GB:-20}GB"
echo "  Port:       ${PORT:-8080}"
echo ""
echo "Starting..."

exec "$@"
