# PSN Data Management

## Volume Strategy

The PlayStation stats data is stored in a Docker named volume called `psn-output-data`. This ensures:
- **Data persistence** across container restarts and rebuilds
- **Data safety** - volume is managed by Docker and not deleted on container removal
- **Consistency** - same approach works locally and in production

## Production Data Location

### Permanent Storage
On the production server (`psn.rx1.uk` / koronto), data is stored at:
- **Primary location**: `/opt/psn-data/` (permanent, survives redeployments)
- **Docker volume**: Managed by Dokploy, linked to deployment ID

The `/opt/psn-data/` directory is independent of Dokploy's deployment system and will persist even if the service is redeployed with a new ID.

### Current Stats
- **Files**: ~378 snapshots
- **Size**: ~264MB
- **History**: 400+ days of gaming data
- **Update frequency**: Daily (at 06:00 UTC)

## Data Recovery Process

If the deployment is recreated and data is missing:

1. SSH to the server:
```bash
ssh koronto
```

2. Check if permanent data exists:
```bash
ls -lh /opt/psn-data/*.json | wc -l
# Should show 378+ files
```

3. Find the new Docker volume name:
```bash
docker volume ls | grep psn-output-data
# Will show something like: playstation-stats-service-XXXXX_psn-output-data
```

4. Copy data to the new volume:
```bash
docker run --rm \
  -v /opt/psn-data:/source \
  -v playstation-stats-service-XXXXX_psn-output-data:/dest \
  alpine sh -c 'cp /source/*.json /dest/'
```

5. Restart the backend:
```bash
docker restart playstation-stats-service-XXXXX-psn-backend-1
```

## Local Development

### Using the Named Volume
The docker-compose.yml uses a named volume `psn-output-data` which Docker creates automatically.

### Restoring from Backup
If you need to restore data from the backup:

```bash
# Stop containers
docker-compose down

# Find the volume name
docker volume ls | grep psn

# Copy backup data to the volume  
docker run --rm \
  -v $(pwd)/output_backup:/backup \
  -v psn_psn-output-data:/data \
  alpine sh -c "cp /backup/*.json /data/"

# Start containers
docker-compose up
```

### Creating a Backup
```bash
# Backup the volume to local directory
docker run --rm \
  -v psn_psn-output-data:/data \
  -v $(pwd)/output_backup:/backup \
  alpine sh -c "cp /data/*.json /backup/"

# Or backup from production server
rsync -avz koronto:/opt/psn-data/ ./output_backup/
```

## Important Notes

- ⚠️ The `output/` directory is in `.gitignore` - data files are **NOT** committed to Git
- ✅ Data persists in Docker volumes which survive container restarts
- 📦 Backups are stored locally in `output_backup/` (also gitignored)
- 🔄 The backend creates new snapshot files daily (output_TIMESTAMP.json)
- 💾 Production data is in `/opt/psn-data/` - this location is permanent
- 🆔 Dokploy deployment IDs may change, but data can always be copied from `/opt/psn-data/`

## Deployment Checklist

When redeploying or the service gets a new deployment ID:

- [ ] Verify `/opt/psn-data/` still exists and has all files
- [ ] Note the new Docker volume name
- [ ] Copy data from `/opt/psn-data/` to the new volume
- [ ] Restart backend container
- [ ] Verify API returns 378+ snapshots: `curl http://psn.rx1.uk:8080/api/analytics | jq '.totalStats'`
- [ ] Verify frontend loads without errors: `curl http://psn.rx1.uk/`

