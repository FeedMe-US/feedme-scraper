# FeedMe Menu Scraper

Go-based scraper for UCLA dining hall menus.

## Quick Start

```bash
# Build
go build -o feedme-scraper .

# Run (requires env vars)
export SUPABASE_URL=...
export SUPABASE_SERVICE_KEY=...
./feedme-scraper run --verbose --days 7
```

## Commands

```bash
./feedme-scraper run           # Scrape menus
./feedme-scraper run --dry-run # Preview without writing
./feedme-scraper clear-cache   # Clear HTTP cache
./feedme-scraper version       # Show version
```

## Scheduling

Runs automatically via GitHub Actions at 5 AM and 5 PM PST.

## Architecture Note

⚠️ **Embedding Generation**: After scraping, this workflow calls the feedme-api
`/admin/generate-embeddings` endpoint to create search embeddings for new items.
The embedding logic lives in feedme-api to avoid code duplication.
