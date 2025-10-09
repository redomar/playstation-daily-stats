# PSN Backend

Go-based backend service that fetches PlayStation Network game data and serves it via REST API.

## Features

- Authenticates with PlayStation Network using NPSSO token
- Fetches game library data (up to 600 titles)
- Scheduled data collection at 6:00 AM daily
- REST API endpoint to serve latest data
- Token caching and refresh

## Environment Variables

**Note**: Environment variables are configured at the **root level** (not in this package). See root `.env.example` for template.

- `NPSSO`: Your PlayStation Network NPSSO authentication token
- `ALLOWED_ORIGINS`: Comma-separated list of allowed CORS origins

When running locally, the backend reads from root `.env` file via `godotenv.Load()`.
When running in Docker, variables are passed via `docker-compose.yml`.

## API Endpoints

### GET /api/latest-output
Returns the most recent snapshot of your game library.

## Running Locally

```bash
# Install Go 1.23+
# Set environment variables in .env file
go run .
```

## Docker

```bash
docker build -t psn-backend .
docker run -p 8080:8080 --env-file .env psn-backend
```
