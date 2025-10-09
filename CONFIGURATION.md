# Configuration Guide

This monorepo uses **centralized configuration** with all environment variables and settings managed from the root directory.

## Configuration Files Overview

### Root Level (Centralized)

| File | Purpose | Used By |
|------|---------|---------|
| `.env` | **All environment variables** | Docker Compose, Backend (local dev) |
| `.env.example` | Template for `.env` | Developers (reference) |
| `package.json` | Workspace & npm scripts | Root, Frontend |
| `docker-compose.yml` | Container orchestration | Docker services |

### Package Level

| Package | File | Purpose | Why Here? |
|---------|------|---------|-----------|
| backend | `go.mod` | Go dependencies | Required by Go modules |
| frontend | `package.json` | Frontend dependencies | Required by npm workspace |
| frontend | `tsconfig.json` | TypeScript config | Frontend-specific |
| frontend | `vite.config.ts` | Vite build config | Frontend-specific |

## Environment Variables (Centralized)

**Location**: `/Users/Mohamed/Workspace/experiments/go/psn/.env` (root only)

```env
# PlayStation Network Authentication
NPSSO=your_npsso_token_here

# Backend CORS Configuration
ALLOWED_ORIGINS=http://localhost:5173,http://localhost:5173

# Frontend API URL
VITE_ALLOWED_ORIGINS=http://localhost:8080
```

### Why Centralized?

✅ **Single source of truth** - One place to manage all environment variables
✅ **Docker Compose integration** - Automatically reads from root `.env`
✅ **No duplication** - Eliminates config drift between packages
✅ **Easier deployment** - One file to configure for all services

### How It Works

**Docker (Production):**
1. `docker-compose.yml` reads `.env` from root
2. Passes variables to containers via `environment:` section
3. Backend receives vars as environment variables
4. Frontend receives vars as build-time variables

**Local Development:**
1. Backend: `godotenv.Load()` reads from `../../.env` (root)
2. Frontend: Vite reads `VITE_*` vars from root `.env`

## NPM Workspace (Why package.json in both places?)

### Root `package.json`
```json
{
  "workspaces": ["packages/*"],
  "scripts": {
    "dev:frontend": "npm run dev --workspace=packages/frontend"
  }
}
```
- **Coordinates** all packages
- **Provides** convenience scripts
- **Manages** shared dependencies (if any)

### Frontend `package.json`
```json
{
  "name": "frontend",
  "dependencies": {
    "react": "^18.3.1"
  }
}
```
- **Defines** frontend-specific dependencies
- **Required** by npm workspaces
- **Not duplication** - it's the workspace member config

## Go Module (Why go.mod in backend?)

### Backend `go.mod`
```go
module psn
go 1.23
```
- **Required** by Go's module system
- **Defines** Go dependencies
- **Cannot** be centralized - Go requires it in the module root

## Docker Configuration

### Root `docker-compose.yml`
```yaml
services:
  psn-backend:
    environment:
      - NPSSO=${NPSSO}              # From root .env
      - ALLOWED_ORIGINS=${ALLOWED_ORIGINS}
```
- **Orchestrates** both services
- **Passes** environment variables from root `.env`
- **Maps** volumes for output directory

### Package Dockerfiles
- `packages/backend/Dockerfile` - Builds Go binary
- `packages/frontend/Dockerfile` - Builds React app
- **Do NOT** copy `.env` files (vars passed by docker-compose)

## Best Practices

### ✅ DO:
- Keep **one `.env`** at root
- Use `.env.example` as template
- Add `.env` to `.gitignore`
- Document all env vars in `.env.example`
- Use root npm scripts for common tasks

### ❌ DON'T:
- Create `.env` files in packages
- Duplicate configuration
- Commit `.env` to git
- Hardcode secrets in code
- Copy `.env` in Dockerfiles

## Adding New Configuration

### New Environment Variable:
1. Add to root `.env`
2. Add to `.env.example` with description
3. Update `docker-compose.yml` if needed
4. Document in this file

### New Frontend Dependency:
```bash
npm install <package> --workspace=packages/frontend
```

### New Backend Dependency:
```bash
cd packages/backend
go get <package>
```

## Troubleshooting

**"Environment variable not found"**
- Check root `.env` file exists
- Verify variable is defined in `.env`
- Restart Docker Compose to reload vars

**"Cannot find module"**
- Run `npm install` at root (installs workspace)
- Check package.json exists in package directory

**"go.mod not found"**
- Each Go package needs its own `go.mod`
- Run `go mod init <name>` in package directory

## Migration from Old Structure

If you had package-level `.env` files:
1. Consolidate all vars into root `.env`
2. Delete package-level `.env` files
3. Update Dockerfiles to remove `.env` COPY commands
4. Restart services

See [MIGRATION.md](MIGRATION.md) for full migration guide.
