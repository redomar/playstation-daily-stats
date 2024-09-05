#!/bin/sh
set -e

echo "Running initial execution of the application..."
/app/main

echo "Initial execution completed. Starting cron daemon..."

# Load the environment variables
env >> /etc/environment

# Start crond in the background
crond -b -l 8

echo "Cron daemon started. Container is now running..."

# Keep the container running
tail -f /dev/null