# API Documentation

## REST API

The Web Cloner provides a REST API for controlling crawl operations programmatically.

### Base URL

```
http://localhost:<api-port>
```

### Authentication

Currently, the API does not require authentication. For production deployments, consider adding API key authentication or OAuth.

### Endpoints

#### GET /api/status

Returns the current crawl status.

**Response:**
```json
{
  "running": true,
  "paused": false,
  "pages_fetched": 150,
  "assets_saved": 3421,
  "errors": 3,
  "bytes_total": 125829123,
  "queue_size": 45,
  "elapsed": "15m32s",
  "current_url": "https://example.com/page5",
  "seed_urls": 1
}
```

#### POST /api/start

Starts a new crawl.

**Request:**
```json
{
  "seeds": ["https://example.com", "https://example.org"]
}
```

**Response:**
```json
{
  "status": "started"
}
```

**Error Responses:**
- `400 Bad Request` - Invalid JSON or missing seeds
- `409 Conflict` - Crawl already running

#### POST /api/stop

Stops the current crawl.

**Response:**
```json
{
  "status": "stopped"
}
```

#### POST /api/pause

Pauses the current crawl (finishes current page, doesn't start new ones).

**Response:**
```json
{
  "status": "paused"
}
```

**Error Responses:**
- `409 Conflict` - No crawl running

#### POST /api/resume

Resumes a paused crawl.

**Response:**
```json
{
  "status": "resumed"
}
```

**Error Responses:**
- `409 Conflict` - No crawl running

#### GET /metrics

Prometheus metrics endpoint (if enabled).

**Response:** Prometheus text format metrics

### WebSocket Endpoints

The Web UI provides a WebSocket endpoint for real-time updates:

```
ws://localhost:<dashboard-port>/ws
```

### Rate Limiting

The API implements rate limiting:
- 100 requests/second sustained
- Burst of 200 requests
- Health check endpoints (`/healthz`, `/readyz`) are exempt

### CORS

CORS is enabled with `Access-Control-Allow-Origin: *` for development. For production, configure specific origins.

### Example Usage

```bash
# Start a crawl
curl -X POST http://localhost:8080/api/start \
  -H "Content-Type: application/json" \
  -d '{"seeds": ["https://example.com"]}'

# Check status
curl http://localhost:8080/api/status

# Pause crawl
curl -X POST http://localhost:8080/api/pause

# Resume crawl
curl -X POST http://localhost:8080/api/resume

# Stop crawl
curl -X POST http://localhost:8080/api/stop
```