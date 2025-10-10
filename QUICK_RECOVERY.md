# Quick Recovery After Redeployment

## What Happens When You Push Code?

When Dokploy pulls new code from Git and redeploys:
1. ✅ Code updates are applied
2. ⚠️ Docker volume may be recreated/emptied
3. ❌ Backend will only see 1 snapshot (the new one it creates)
4. ❌ Frontend will show errors because analytics data is incomplete

## Quick Fix (Takes ~2 minutes)

### Step 1: SSH to server
```bash
ssh koronto
```

### Step 2: Find the volume name
```bash
docker volume ls | grep psn
# Look for: psn-output-data or playstation-stats-service-*_psn-output-data
```

### Step 3: Copy data from permanent storage
```bash
# Replace VOLUME_NAME with the name from step 2
docker run --rm \
  -v /opt/psn-data:/source \
  -v VOLUME_NAME:/dest \
  alpine sh -c 'cp /source/*.json /dest/ && echo "Copied $(ls /dest/*.json | wc -l) files"'
```

### Step 4: Restart backend
```bash
# Find backend container name
docker ps | grep backend

# Restart it (replace with actual name)
docker restart playstation-stats-service-XXXXX-psn-backend-1
```

### Step 5: Verify
```bash
# Should see "Loaded 377 snapshots"
docker logs playstation-stats-service-XXXXX-psn-backend-1 | grep "Loaded.*snapshots"

# Test API (from your local machine)
curl -s http://psn.rx1.uk:8080/api/analytics | jq '.totalStats'
# Should show: totalGames: 150, daysTracked: 400
```

## Why This Happens

- Docker volumes are tied to container lifecycles
- Dokploy may create new volumes on redeployment
- `/opt/psn-data` is our permanent backup (independent of deployments)
- We copy from permanent storage → active volume after each deployment

## Permanent Solution (TODO)

To avoid this manual step, we could:
1. Use Dokploy's persistent volume configuration
2. Mount `/opt/psn-data` directly in docker-compose (requires sudo access)
3. Create a Docker volume with fixed name that survives redeployments
4. Add a post-deployment script in Dokploy to automatically copy data

For now, the manual 2-minute recovery process is documented and reliable.
