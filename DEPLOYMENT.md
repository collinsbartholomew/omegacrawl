# Deployment Documentation

## Overview

This document covers deployment options for Web Cloner across different environments.

## Deployment Options

### 1. Docker (Recommended)

#### Single Container

```bash
# Build
docker build -t web-cloner .

# Run
docker run --rm \
  -v ./output:/data/output \
  web-cloner -d 1 https://example.com
```

#### Docker Compose (Full Stack)

```bash
# Start full stack with Chrome
docker-compose up -d

# View logs
docker-compose logs -f cloner

# Stop
docker-compose down
```

#### docker-compose.yml

```yaml
version: '3.8'

services:
  chrome:
    image: zenika/alpine-chrome:latest
    ports:
      - "9222:9222"
    command:
      - "--no-sandbox"
      - "--disable-gpu"
      - "--disable-dev-shm-usage"
      - "--remote-debugging-address=0.0.0.0"
      - "--remote-debugging-port=9222"
      - "--disable-setuid-sandbox"
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:9222/json/version"]
      interval: 10s
      timeout: 5s
      retries: 5
    deploy:
      resources:
        limits:
          memory: 2G

  cloner:
    build: .
    depends_on:
      chrome:
        condition: service_healthy
    environment:
      - SEEDS=https://example.com
      - REMOTE_CHROME_URL=ws://chrome:9222/devtools/browser/0
      - BROWSER_POOL_SIZE=2
    volumes:
      - ./output:/data/output
    deploy:
      resources:
        limits:
          memory: 4G
```

### 2. Kubernetes

#### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-cloner
  labels:
    app: web-cloner
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web-cloner
  template:
    metadata:
      labels:
        app: web-cloner
    spec:
      containers:
      - name: cloner
        image: web-cloner:latest
        ports:
        - containerPort: 8080
        env:
        - name: SEEDS
          value: "https://example.com"
        - name: REMOTE_CHROME_URL
          value: "ws://chrome-service:9222/devtools/browser/0"
        - name: BROWSER_POOL_SIZE
          value: "2"
        volumeMounts:
        - name: output
          mountPath: /data/output
        resources:
          requests:
            memory: "2Gi"
            cpu: "1000m"
          limits:
            memory: "4Gi"
            cpu: "2000m"
        securityContext:
          runAsNonRoot: true
          runAsUser: 1000
          readOnlyRootFilesystem: true
          allowPrivilegeEscalation: false
          capabilities:
            drop: ["ALL"]
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
      volumes:
      - name: output
        persistentVolumeClaim:
          claimName: web-cloner-output
```

#### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web-cloner-api
spec:
  selector:
    app: web-cloner
  ports:
  - name: api
    port: 8080
    targetPort: 8080
  type: ClusterIP
```

#### Chrome Sidecar

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chrome
spec:
  replicas: 1
  selector:
    matchLabels:
      app: chrome
  template:
    metadata:
      labels:
        app: chrome
    spec:
      containers:
      - name: chrome
        image: zenika/alpine-chrome:latest
        ports:
        - containerPort: 9222
        command:
        - "--no-sandbox"
        - "--disable-gpu"
        - "--disable-dev-shm-usage"
        - "--remote-debugging-address=0.0.0.0"
        - "--remote-debugging-port=9222"
        - "--disable-setuid-sandbox"
        resources:
          requests:
            memory: "1Gi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
```

#### Chrome Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: chrome-service
spec:
  selector:
    app: chrome
  ports:
  - name: cdp
    port: 9222
    targetPort: 9222
  type: ClusterIP
```

#### PVC for Output

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: web-cloner-output
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Gi
```

### 3. Helm Chart

#### Chart Structure

```
web-cloner/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── pvc.yaml
│   ├── chrome-deployment.yaml
│   ├── chrome-service.yaml
│   ├── networkpolicy.yaml
│   └── _helpers.tpl
```

#### Chart.yaml

```yaml
apiVersion: v2
name: web-cloner
description: Web Cloner - Production-grade web archiver
type: application
version: 1.0.0
appVersion: "1.0.0"
keywords:
  - web
  - crawler
  - archiver
maintainers:
  - name: Web Cloner Team
    email: team@example.com
```

#### values.yaml

```yaml
replicaCount: 3

image:
  repository: web-cloner
  pullPolicy: IfNotPresent
  tag: "1.0.0"

seeds: "https://example.com"

browserPoolSize: 2

resources:
  limits:
    cpu: 2000m
    memory: 4Gi
  requests:
    cpu: 1000m
    memory: 2Gi

persistence:
  enabled: true
  size: 100Gi
  storageClass: ""

chrome:
  enabled: true
  replicaCount: 1
  resources:
    limits:
      cpu: 1000m
      memory: 2Gi
    requests:
      cpu: 500m
      memory: 1Gi

networkPolicy:
  enabled: true

securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
```

### 4. Bare Metal / VM

#### System Requirements

- Go 1.25+
- Chrome/Chromium
- 4GB+ RAM (8GB recommended)
- 2+ CPU cores
- SSD storage

#### Installation

```bash
# Install Go
wget https://go.dev/dl/go1.25.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Install Chrome
sudo apt-get update && sudo apt-get install -y chromium

# Build
git clone https://github.com/user/web-cloner.git
cd web-cloner
go build -o web-cloner ./cmd/clone

# Install as service
sudo cp web-cloner /usr/local/bin/
```

#### Systemd Service

```ini
# /etc/systemd/system/web-cloner.service
[Unit]
Description=Web Cloner
After=network.target

[Service]
Type=simple
User=cloner
Group=cloner
WorkingDirectory=/opt/web-cloner
ExecStart=/usr/local/bin/web-cloner -d 1 -o /var/lib/web-cloner/output https://example.com
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

# Security
User=cloner
Group=cloner
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/web-cloner/output
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable web-cloner
sudo systemctl start web-cloner
```

### 5. Cloud Providers

#### AWS ECS

```json
{
  "family": "web-cloner",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "2048",
  "memory": "4096",
  "containerDefinitions": [
    {
      "name": "cloner",
      "image": "123456789.dkr.ecr.us-east-1.amazonaws.com/web-cloner:latest",
      "portMappings": [{"containerPort": 8080, "protocol": "tcp"}],
      "environment": [
        {"name": "SEEDS", "value": "https://example.com"},
        {"name": "REMOTE_CHROME_URL", "value": "ws://chrome-sidecar:9222/devtools/browser/0"}
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/web-cloner",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "ecs"
        }
      }
    },
    {
      "name": "chrome",
      "image": "zenika/alpine-chrome:latest",
      "portMappings": [{"containerPort": 9222, "protocol": "tcp"}],
      "command": [
        "--no-sandbox",
        "--disable-gpu",
        "--disable-dev-shm-usage",
        "--remote-debugging-address=0.0.0.0",
        "--remote-debugging-port=9222"
      ]
    }
  ]
}
```

#### Google Cloud Run

```yaml
# cloudrun.yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: web-cloner
spec:
  template:
    spec:
      containers:
      - image: gcr.io/PROJECT/web-cloner:latest
        ports:
        - containerPort: 8080
        env:
        - name: SEEDS
          value: "https://example.com"
        resources:
          limits:
            memory: "4Gi"
            cpu: "2000m"
        volumeMounts:
        - name: output
          mountPath: /data/output
      volumes:
      - name: output
        emptyDir: {}
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SEEDS` | Comma-separated seed URLs | - |
| `REMOTE_CHROME_URL` | WebSocket URL for remote Chrome | - |
| `BROWSER_POOL_SIZE` | Number of Chrome processes | 1 |
| `MAX_CONCURRENT_PAGES` | Max concurrent page crawls | 5 |
| `ASSET_CONCURRENCY` | Max concurrent asset downloads | 16 |
| `OUTPUT_DIR` | Output directory | ./output |
| `LOG_LEVEL` | Log level (debug/info/warn/error) | info |
| `API_PORT` | REST API port (0 = disabled) | 0 |
| `DASHBOARD_PORT` | Web UI port (0 = disabled) | 0 |

### Config File

```json
{
  "seeds": ["https://example.com"],
  "max_depth": 10,
  "max_concurrent_pages": 5,
  "asset_concurrency": 16,
  "page_timeout": "120s",
  "crawl_delay": "1s",
  "respect_robots": true,
  "enable_stealth": true,
  "blocked_url_patterns": ["*doubleclick*", "*google-analytics*"],
  "api_port": 8080,
  "dashboard_port": 8081
}
```

## Monitoring

### Health Checks

```bash
# Liveness
curl http://localhost:8080/healthz

# Readiness
curl http://localhost:8080/readyz

# Metrics
curl http://localhost:8080/metrics
```

### Prometheus Alerts

```yaml
groups:
- name: web-cloner
  rules:
  - alert: CrawlDown
    expr: up{job="web-cloner"} == 0
    for: 1m
    labels:
      severity: critical
    annotations:
      summary: "Web Cloner is down"

  - alert: HighErrorRate
    expr: rate(crawler_errors_total[5m]) > 0.1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High error rate detected"

  - alert: CircuitBreakerOpen
    expr: crawler_circuit_breakers_open > 0
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: "Circuit breaker open for {{ $labels.host }}"
```

## Backup & Recovery

### Backup Output

```bash
# Backup crawl output
tar -czf web-cloner-backup-$(date +%Y%m%d).tar.gz /data/output

# Or use rsync for incremental
rsync -av /data/output/ backup-server:/backups/web-cloner/
```

### Restore

```bash
# Restore from backup
tar -xzf web-cloner-backup-20240101.tar.gz -C /
```

### Checkpoint Recovery

```bash
# Resume from checkpoint
./clone -c config.json --resume
```

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| Chrome crashes | OOM | Reduce browser_pool_size, increase memory |
| Slow crawling | Network latency | Increase asset_concurrency, check network |
| Memory leak | Unclosed interceptors | Ensure pool.Release() called |
| Queue stuck | Deadlock | Check circuit breaker, restart |

### Debug Commands

```bash
# Check Chrome connection
curl http://chrome:9222/json/version

# View logs
docker-compose logs -f cloner

# Check metrics
curl http://localhost:8080/metrics

# Check queue
curl http://localhost:8080/api/status
```