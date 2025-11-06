# ToolBridge API Deployment Guide

This guide covers deploying the ToolBridge API in different environments:

1. **Local Development** - Docker Compose for quick testing
2. **Production** - Kubernetes for scalable deployment

## Table of Contents

- [Prerequisites](#prerequisites)
- [Local Development (Docker Compose)](#local-development-docker-compose)
- [Production Deployment (Kubernetes)](#production-deployment-kubernetes)
- [Configuration](#configuration)
- [Database Migrations](#database-migrations)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### For Local Development

- Docker 20.10+ and Docker Compose 2.0+
- Git

### For Production (Kubernetes)

- Kubernetes cluster (1.24+)
- `kubectl` configured for your cluster
- Container registry access (Docker Hub, GCR, ECR, etc.)
- (Optional) `kustomize` for easier deployment

---

## Local Development (Docker Compose)

Docker Compose provides the fastest way to run the entire stack locally for development and testing.

### Quick Start

```bash
# 1. Clone the repository
git clone https://github.com/yourusername/toolbridge-api.git
cd toolbridge-api

# 2. Copy environment template
cp .env.example .env

# 3. (Optional) Edit .env to customize configuration
# Default values work fine for local development

# 4. Start all services
docker-compose up -d

# 5. Check service health
docker-compose ps

# 6. View logs
docker-compose logs -f api

# 7. Test the API
curl http://localhost:8080/healthz
# Expected: "ok"
```

### What Gets Started

The Docker Compose setup includes:

1. **postgres** - PostgreSQL 16 database
2. **migrate** - One-shot container that applies database migrations
3. **api** - The ToolBridge API server (starts after migrations complete)
4. **pgadmin** (optional) - Database admin interface

### Service Details

| Service | Port | Description |
|---------|------|-------------|
| API | 8080 | Main ToolBridge API |
| PostgreSQL | 5432 | Database |
| PgAdmin | 5050 | Database admin UI (profile: admin) |

### Running PgAdmin (Optional)

To start the optional PgAdmin service for database inspection:

```bash
docker-compose --profile admin up -d pgadmin
```

Access PgAdmin at http://localhost:5050:
- Email: `admin@toolbridge.local`
- Password: `admin`

### Common Operations

```bash
# Stop all services
docker-compose down

# Stop and remove volumes (fresh start)
docker-compose down -v

# View logs for specific service
docker-compose logs -f api
docker-compose logs -f postgres
docker-compose logs migrate

# Restart API after code changes
docker-compose restart api

# Rebuild and restart after code changes
docker-compose up -d --build api

# Run database migrations manually
docker-compose run --rm migrate

# Access PostgreSQL directly
docker-compose exec postgres psql -U toolbridge -d toolbridge

# Run smoke tests
./scripts/smoke-test.sh http://localhost:8080
```

### Environment Variables

Key environment variables for Docker Compose (in `.env` file):

```bash
# API Configuration
PORT=8080
API_PORT=8080
ENV=dev

# Database
POSTGRES_PASSWORD=dev-password

# Security (CHANGE IN PRODUCTION!)
JWT_HS256_SECRET=dev-secret-change-in-production
```

---

## Production Deployment (Kubernetes)

For production workloads, deploy to Kubernetes for high availability and scalability.

### Architecture

The Kubernetes deployment includes:

- **Namespace**: `toolbridge` - Isolated namespace for all resources
- **PostgreSQL**: Stateful database with persistent volume
- **Migration Job**: Runs database migrations before API starts
- **API Deployment**: 2+ replicas with health checks
- **Services**: ClusterIP for internal access

### Step 1: Build and Push Docker Image

```bash
# Build the image
docker build -t your-registry/toolbridge-api:v1.0.0 .

# Push to your registry
docker push your-registry/toolbridge-api:v1.0.0

# Update k8s/kustomization.yaml with your image
# Edit the images section to point to your registry
```

### Step 2: Configure Secrets

**IMPORTANT**: Update secrets before deploying!

```bash
# Edit k8s/secret.yaml and update these values:
# - postgres-password: Strong database password
# - jwt-secret: Strong JWT signing secret (32+ characters)

# Example: Generate a secure JWT secret
openssl rand -base64 32
```

The secret file uses `stringData` for easier editing:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: toolbridge-secret
  namespace: toolbridge
type: Opaque
stringData:
  postgres-password: "YOUR-SECURE-PASSWORD-HERE"
  jwt-secret: "YOUR-SECURE-JWT-SECRET-HERE"
```

### Step 3: Review Configuration

Edit `k8s/configmap.yaml` to match your environment:

```yaml
data:
  ENV: "production"          # production, staging, dev
  PORT: "8080"
  DB_HOST: "toolbridge-postgres"
  DB_PORT: "5432"
  DB_NAME: "toolbridge"
  DB_USER: "toolbridge"
  DB_SSLMODE: "require"      # Use "require" in production!
```

### Step 4: Deploy to Kubernetes

#### Option A: Using kubectl (Simple)

```bash
# Deploy all resources in order
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/secret.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/postgres.yaml

# Wait for postgres to be ready
kubectl wait --for=condition=ready pod -l app=postgres -n toolbridge --timeout=300s

# Run migrations
kubectl apply -f k8s/migration-job.yaml

# Wait for migrations to complete
kubectl wait --for=condition=complete job/toolbridge-migrate -n toolbridge --timeout=300s

# Deploy API
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml

# Wait for API to be ready
kubectl wait --for=condition=ready pod -l app=toolbridge-api -n toolbridge --timeout=300s
```

#### Option B: Using Kustomize (Recommended)

```bash
# Update k8s/kustomization.yaml with your image details
# Then deploy everything at once:

kubectl apply -k k8s/

# Or preview changes first:
kubectl diff -k k8s/
```

### Step 5: Verify Deployment

```bash
# Check all resources
kubectl get all -n toolbridge

# Check pod status
kubectl get pods -n toolbridge

# View API logs
kubectl logs -l app=toolbridge-api -n toolbridge -f

# Check migration job logs
kubectl logs job/toolbridge-migrate -n toolbridge

# Test health endpoint
kubectl port-forward -n toolbridge svc/toolbridge-api 8080:80
curl http://localhost:8080/healthz
```

### Step 6: Expose the API

The default service is `ClusterIP` (internal only). Choose an exposure method:

#### Option A: LoadBalancer (Cloud Providers)

Uncomment the LoadBalancer service in `k8s/service.yaml` and apply:

```bash
kubectl apply -f k8s/service.yaml

# Get external IP
kubectl get svc toolbridge-api-external -n toolbridge
```

#### Option B: Ingress (Recommended for Production)

Create an Ingress resource (example):

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: toolbridge-ingress
  namespace: toolbridge
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  tls:
  - hosts:
    - api.toolbridge.example.com
    secretName: toolbridge-tls
  rules:
  - host: api.toolbridge.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: toolbridge-api
            port:
              number: 80
```

#### Option C: NodePort (Development/Testing)

Edit `k8s/service.yaml` to use `type: NodePort`.

### Scaling

```bash
# Scale API replicas
kubectl scale deployment toolbridge-api -n toolbridge --replicas=5

# Or edit deployment
kubectl edit deployment toolbridge-api -n toolbridge
```

### Updates and Rollouts

```bash
# Update image
kubectl set image deployment/toolbridge-api api=your-registry/toolbridge-api:v1.1.0 -n toolbridge

# Check rollout status
kubectl rollout status deployment/toolbridge-api -n toolbridge

# Rollback if needed
kubectl rollout undo deployment/toolbridge-api -n toolbridge

# View rollout history
kubectl rollout history deployment/toolbridge-api -n toolbridge
```

---

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `JWT_HS256_SECRET` | Yes | - | JWT signing secret (32+ chars) |
| `PORT` | No | `8080` | HTTP server port |
| `ENV` | No | `dev` | Environment (dev/staging/production) |

### Database Connection

The application expects a `DATABASE_URL` in this format:

```
postgres://user:password@host:port/database?sslmode=require
```

For Kubernetes, this is constructed from ConfigMap and Secret values.

---

## Database Migrations

Migrations are SQL files in the `migrations/` directory, executed in order by filename.

### Migration File Naming

```
0001_initial_schema.sql
0002_add_users.sql
0003_add_notes.sql
...
```

### Running Migrations

#### Docker Compose

Migrations run automatically when you start the stack:

```bash
docker-compose up -d
```

To run manually:

```bash
docker-compose run --rm migrate
```

#### Kubernetes

Migrations run as a Job before the API starts:

```bash
# Run migrations
kubectl apply -f k8s/migration-job.yaml

# Check status
kubectl get job toolbridge-migrate -n toolbridge

# View logs
kubectl logs job/toolbridge-migrate -n toolbridge

# Re-run migrations (delete and recreate job)
kubectl delete job toolbridge-migrate -n toolbridge
kubectl apply -f k8s/migration-job.yaml
```

#### Local Development

You can run migrations locally using the script:

```bash
# Ensure postgres container is running
docker-compose up -d postgres

# Run migrations
./scripts/migrate.sh

# Or use make target
make migrate
```

### Migration Status

Check which migrations have been applied:

```bash
# Docker Compose
docker-compose exec postgres psql -U toolbridge -d toolbridge \
  -c "SELECT migration, applied_at FROM schema_migrations ORDER BY applied_at"

# Kubernetes
kubectl exec -it deployment/toolbridge-postgres -n toolbridge -- \
  psql -U toolbridge -d toolbridge \
  -c "SELECT migration, applied_at FROM schema_migrations ORDER BY applied_at"
```

---

## Troubleshooting

### Docker Compose Issues

#### API won't start

```bash
# Check logs
docker-compose logs api

# Common issues:
# 1. Migrations failed
docker-compose logs migrate

# 2. Database not ready
docker-compose logs postgres

# 3. Port already in use
docker-compose ps
lsof -ti:8080 | xargs kill -9
```

#### Database connection errors

```bash
# Check postgres is healthy
docker-compose ps postgres

# Verify connection
docker-compose exec postgres pg_isready -U toolbridge

# Check database exists
docker-compose exec postgres psql -U toolbridge -l
```

#### Migrations not running

```bash
# Run migrations manually
docker-compose run --rm migrate

# Check migration logs
docker-compose logs migrate

# Verify migration tracking table
docker-compose exec postgres psql -U toolbridge -d toolbridge \
  -c "SELECT * FROM schema_migrations"
```

### Kubernetes Issues

#### Pods not starting

```bash
# Describe pod to see events
kubectl describe pod -l app=toolbridge-api -n toolbridge

# Common issues:
# 1. Image pull errors - check image name/tag
# 2. Secret/ConfigMap not found - check they exist
# 3. Resource limits - check node capacity

# Check events
kubectl get events -n toolbridge --sort-by='.lastTimestamp'
```

#### Migration job failed

```bash
# Check job status
kubectl get job toolbridge-migrate -n toolbridge -o yaml

# View logs
kubectl logs job/toolbridge-migrate -n toolbridge

# Delete and retry
kubectl delete job toolbridge-migrate -n toolbridge
kubectl apply -f k8s/migration-job.yaml
```

#### API not accessible

```bash
# Check service
kubectl get svc -n toolbridge

# Port forward for testing
kubectl port-forward -n toolbridge svc/toolbridge-api 8080:80

# Check pod logs
kubectl logs -l app=toolbridge-api -n toolbridge --tail=100

# Check health
curl http://localhost:8080/healthz
```

#### Database connection issues

```bash
# Check postgres service
kubectl get svc toolbridge-postgres -n toolbridge

# Check postgres logs
kubectl logs -l app=postgres -n toolbridge

# Test connection from API pod
kubectl exec -it deployment/toolbridge-api -n toolbridge -- \
  wget -qO- toolbridge-postgres:5432
```

### General Debugging

```bash
# Check API health
curl http://localhost:8080/healthz

# Run smoke tests
./scripts/smoke-test.sh http://localhost:8080

# Check database connectivity
psql "$DATABASE_URL" -c "SELECT version()"

# View recent logs
docker-compose logs --tail=100 api          # Docker Compose
kubectl logs -l app=toolbridge-api -n toolbridge --tail=100  # K8s
```

---

## Production Checklist

Before deploying to production:

- [ ] Change `JWT_HS256_SECRET` to a strong random value (32+ characters)
- [ ] Change `POSTGRES_PASSWORD` to a strong password
- [ ] Set `ENV=production` in ConfigMap
- [ ] Enable SSL for database (`DB_SSLMODE=require`)
- [ ] Configure proper resource limits in `k8s/deployment.yaml`
- [ ] Set up Ingress with TLS certificates
- [ ] Configure backup solution for PostgreSQL
- [ ] Set up monitoring and alerting
- [ ] Review security policies and network policies
- [ ] Test disaster recovery procedures
- [ ] Document runbook for operations team

### Known Limitations (Phase 7)

⚠️ **Single Replica Requirement:**

The current implementation (Phase 7) uses in-memory storage for:
- Sync session management
- Rate limiting state

**Impact:**
- Helm chart is configured with `replicaCount: 1` (single replica only)
- Sessions and rate limiters are NOT shared between pods
- Scaling to multiple replicas will cause intermittent sync failures

**Workarounds:**
1. **Single Replica** (current default): Keep `replicaCount: 1`
2. **Vertical Scaling**: Increase CPU/memory limits for the single pod
3. **Redis Implementation** (future): See `Plans/redis-distributed-state.md` for horizontal scaling roadmap

**Future Work:**
- Phase 8+ will implement Redis-backed distributed storage
- This will enable horizontal scaling with `replicaCount: 2+`
- See `Plans/redis-distributed-state.md` for implementation details

---

## Additional Resources

- [PostgreSQL Docker Image](https://hub.docker.com/_/postgres)
- [Kubernetes Documentation](https://kubernetes.io/docs/)
- [Docker Compose Documentation](https://docs.docker.com/compose/)
- [Kustomize Documentation](https://kustomize.io/)

## Support

For issues and questions:
- GitHub Issues: https://github.com/yourusername/toolbridge-api/issues
- Documentation: See README.md
