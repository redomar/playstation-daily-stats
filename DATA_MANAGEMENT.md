# PSN Data Management

## Volume Strategy

The PlayStation stats data is stored in a Docker named volume called `psn-output-data`. This ensures:
- **Data persistence** across container restarts and rebuilds
- **Data safety** - volume is managed by Docker and not deleted on container removal
- **Consistency** - same approach works locally and in production

## Local Development

### Using the Named Volume
The docker-compose.yml uses a named volume `psn-output-data` which Docker creates automatically.

### Restoring from Backup
If you need to restore data from the backup:

```bash
# Stop containers
docker-compose down

# Find the volume location
docker volume inspect psn-output-data

# Copy backup data to the volume
docker run --rm -v psn-output-data:/data -v $(pwd)/output_backup:/backup alpine sh -c "cp /backup/* /data/"

# Start containers
docker-compose up
```

### Creating a Backup
```bash
# Backup the volume to local directory
docker run --rm -v psn-output-data:/data -v $(pwd)/output_backup:/backup alpine sh -c "cp /data/* /backup/"
```

## Production Deployment (Dokploy)

On the server, data is stored at:
- `/etc/dokploy/compose/playstation-stats-service-t232r7/output/`

The Docker volume is automatically managed by Dokploy. To access the data:

```bash
# SSH to server
ssh koronto

# View volume location
docker volume inspect playstation-stats-service-t232r7_psn-output-data

# Backup from server
rsync -avz koronto:/etc/dokploy/compose/playstation-stats-service-t232r7/output/ ./output_backup/
```

## Important Notes

- ⚠️ The `output/` directory is in `.gitignore` - data files are **NOT** committed to Git
- ✅ Data persists in Docker volumes which survive container restarts
- 📦 Backups are stored locally in `output_backup/` (also gitignored)
- 🔄 The backend creates new snapshot files daily (output_TIMESTAMP.json)
- 💾 Current dataset: ~393 files, ~264MB (as of Oct 2024)

## Migration from Old Setup

If migrating from the old bind mount (`./output:/app/output`):

1. Stop containers: `docker-compose down`
2. Copy existing data to the new volume:
   ```bash
   docker volume create psn-output-data
   docker run --rm -v $(pwd)/output:/source -v psn-output-data:/dest alpine sh -c "cp /source/* /dest/"
   ```
3. Start with new config: `docker-compose up`
