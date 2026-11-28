#!/usr/bin/env bash
set -euo pipefail

image=$1
container=$(docker create --platform linux/amd64 --network none "$image")
trap 'docker rm -f "$container" >/dev/null' EXIT
docker start "$container" >/dev/null
docker exec -i "$container" sh -ec 'curl --version; crontab -' <<'CRON'
* * * * * touch /tmp/cron-ran
CRON
# BusyBox crond picks up the crontab and runs it at the next minute boundary.
for ((i=0; i<45; i++)); do
    if docker exec "$container" test -e /tmp/cron-ran; then exit 0; fi
    sleep 2
done
docker logs "$container" >&2
exit 1
