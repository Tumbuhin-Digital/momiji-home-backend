#!/bin/sh
set -e

echo "Running migrations..."
/usr/local/bin/migrate \
  -path=/app/migrations \
  -database "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}" \
  up

if [ "${SEED_ZIPS:-}" = "1" ] || [ "${SEED_ZIPS:-}" = "true" ]; then
  ZIPS_CSV_PATH="${USZIPS_CSV:-/app/data/uszips.csv}"
  ZIPS_CSV_URL="${USZIPS_CSV_URL:-https://raw.githubusercontent.com/scpike/us-state-county-zip/master/geo-data.csv}"

  echo "Seeding ZIP codes..."
  mkdir -p "$(dirname "$ZIPS_CSV_PATH")"

  if [ ! -f "$ZIPS_CSV_PATH" ]; then
    echo "Downloading ZIP CSV to $ZIPS_CSV_PATH..."
    curl -L -o "$ZIPS_CSV_PATH" "$ZIPS_CSV_URL"
  fi

  ./seed_zips -csv "$ZIPS_CSV_PATH"
fi

echo "Starting server..."
exec ./main