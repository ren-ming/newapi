#!/bin/bash
set -e

echo '========================================'
echo '  New-API Redeploy Script'
echo '========================================'
echo ''

cd "$(dirname "$0")"

echo '[1/4] Stopping containers...'
docker compose down
echo ''

echo '[2/4] Building images...'
docker compose build
echo ''

echo '[3/4] Starting containers...'
docker compose up -d
echo ''

echo '[4/4] Waiting for service to be ready...'
sleep 3

if docker exec new-api wget -qO- http://localhost:3000/api/status | grep -q '"success":\s*true'; then
    echo ''
    echo '========================================'
    echo '  Redeploy completed successfully!'
    echo '  Access: http://server2:3005'
    echo '========================================'
else
    echo ''
    echo '========================================'
    echo '  WARNING: Service health check failed'
    echo '  Check logs: docker compose logs -f'
    echo '========================================'
    exit 1
fi
