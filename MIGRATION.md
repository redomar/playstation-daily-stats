# Migration Guide

This document outlines the changes made to restructure the PSN project into a proper monorepo.

## Changes Summary

### 1. Version Updates

#### Go Backend
- **Go**: 1.21 → 1.23
- **Dockerfile**: golang:1.20-alpine → golang:1.23-alpine

#### Frontend
- **Node.js**: 16 → 22 (Docker)
- **Vite**: 5.4.1 → 7.1.9
- **TypeScript**: 5.5.3 → 5.9.3
- **ESLint**: 9.9.0 → 9.37.0
- **Radix UI components**: Updated to latest versions
- **Lucide React**: 0.439.0 → 0.545.0

### 2. Scheduling Changes

**Before:**
- Fetched data every 24 hours from start time
- No specific time scheduling

**After:**
- Fetches data daily at **6:00 AM** local time
- Calculates next 6 AM and waits until that time
- More predictable and consistent scheduling

**Modified file:** `packages/backend/api.go:37-59`

### 3. Project Structure

**Before:**
```
psn/
├── vite-frontend/
├── *.go files (root)
├── go.mod
├── Dockerfile
└── docker-compose.yml
```

**After:**
```
psn/
├── packages/
│   ├── backend/        # All Go files moved here
│   └── frontend/       # vite-frontend renamed and moved
├── output/             # Data output directory
├── package.json        # Root workspace configuration
├── docker-compose.yml  # Updated paths
├── .env.example
├── .gitignore          # Enhanced
└── README.md           # Comprehensive docs
```

### 4. Docker Compose Updates

**Services renamed:**
- `psn-service` → `psn-backend`
- Build context paths updated to `packages/backend` and `packages/frontend`
- Volume mapping: `../output` → `./output`

### 5. New Files Created

- `/package.json` - Root workspace configuration with npm scripts
- `/packages/backend/README.md` - Backend documentation
- `/packages/frontend/README.md` - Frontend documentation
- `/.env.example` - Environment variables template
- `/.gitignore` - Enhanced for monorepo structure
- `/MIGRATION.md` - This file

### 6. NPM Scripts (Root)

New convenience scripts added to root `package.json`:

```bash
npm run dev:frontend       # Start frontend dev server
npm run dev:backend        # Start backend
npm run build:frontend     # Build frontend
npm run build:backend      # Build backend
npm run docker:up          # Start Docker containers
npm run docker:down        # Stop Docker containers
```

## Migration Steps for Developers

If you have an existing clone of this repository, follow these steps:

### 1. Pull Latest Changes
```bash
git pull origin master
```

### 2. Clean Old Dependencies
```bash
# Remove old node_modules
rm -rf vite-frontend/node_modules
rm -rf node_modules

# Remove old Go build artifacts
rm -f main
cd packages/backend && go clean
```

### 3. Reinstall Dependencies
```bash
# Install root dependencies
npm install

# Frontend dependencies (if working locally)
cd packages/frontend
npm install

# Backend dependencies
cd packages/backend
go mod tidy
```

### 4. Update Environment Variables
```bash
# Copy example env file
cp .env.example .env

# Edit with your NPSSO token
nano .env
```

### 5. Test the Setup
```bash
# Option 1: Docker (recommended)
npm run docker:up

# Option 2: Local development
# Terminal 1: Backend
cd packages/backend && go run .

# Terminal 2: Frontend
cd packages/frontend && npm run dev
```

## Breaking Changes

### File Paths
- All Go source files moved from root to `packages/backend/`
- Frontend moved from `vite-frontend/` to `packages/frontend/`
- Update any scripts or references to these paths

### Docker Service Names
- `psn-service` is now `psn-backend`
- Update any docker commands or networking references

### Schedule Timing
- Data fetching now occurs at 6:00 AM daily instead of every 24 hours from start
- First run will fetch immediately, then wait until next 6 AM

## Rollback Instructions

If you need to rollback to the old structure:

```bash
# Checkout previous commit
git log --oneline  # Find the commit before migration
git checkout <commit-hash>

# Or revert the migration commit
git revert <migration-commit-hash>
```

## Support

If you encounter issues after migration:
1. Check the updated README.md for new setup instructions
2. Verify your `.env` file matches `.env.example` format
3. Ensure you're using the correct Node.js (22+) and Go (1.23+) versions
4. Open an issue on GitHub with details of the problem

## Future Considerations

This monorepo structure allows for:
- Shared tooling across packages
- Easier dependency management
- Better CI/CD integration
- Potential for additional packages (e.g., shared types, utilities)
- Consistent versioning and releases
