# US ZIP reference data

The checkout address validator reads from the `us_zip_codes` table. After running migrations, seed it once:

```bash
# Option A: SimpleMaps free tier (zip, city, state_id, state_name columns)
# https://simplemaps.com/data/us-zips — save as data/uszips.csv

# Option B: Open census-derived dataset (zipcode, city, state_abbr, state columns)
curl -L -o data/uszips.csv \
  https://raw.githubusercontent.com/scpike/us-state-county-zip/master/geo-data.csv

make seed-zips
```

Override the CSV path with `USZIPS_CSV=/path/to/file.csv make seed-zips`.

Verify a known ZIP:

```sql
SELECT * FROM us_zip_codes WHERE zip_code = '94104';
```
