#!/bin/bash
set -euo pipefail

cd /var/www/momiji/backend

docker build -t momiji-backend:latest .

ENV_FILE=$(mktemp)
cleanup() { rm -f "$ENV_FILE"; }
trap cleanup EXIT

docker inspect momiji_app_prod --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -v -E '^(PATH|HOSTNAME|HOME)=' > "$ENV_FILE"

RESTART=$(docker inspect momiji_app_prod --format '{{.HostConfig.RestartPolicy.Name}}')
if [ -z "$RESTART" ]; then
  RESTART=unless-stopped
fi
NETWORK=$(docker inspect momiji_app_prod --format '{{.HostConfig.NetworkMode}}')

VOL_ARGS=()
while IFS= read -r bind; do
  if [ -n "$bind" ]; then
    VOL_ARGS+=(-v "$bind")
  fi
done < <(docker inspect momiji_app_prod --format '{{range .HostConfig.Binds}}{{println .}}{{end}}')

docker rm -f momiji_app_prod_bak >/dev/null 2>&1 || true
docker stop momiji_app_prod
docker rename momiji_app_prod momiji_app_prod_bak

rollback() {
  echo "New container failed. Restoring the previous one."
  docker rm -f momiji_app_prod >/dev/null 2>&1 || true
  docker rename momiji_app_prod_bak momiji_app_prod
  docker start momiji_app_prod
  exit 1
}

if ! docker run -d \
  --name momiji_app_prod \
  --restart "$RESTART" \
  --network "$NETWORK" \
  -p 127.0.0.1:7001:7001 \
  "${VOL_ARGS[@]}" \
  --env-file "$ENV_FILE" \
  momiji-backend:latest
then
  rollback
fi

ready=0
for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
  if docker logs momiji_app_prod 2>&1 | grep -q "Server started"; then
    ready=1
    break
  fi
  if ! docker ps --format '{{.Names}}' | grep -qx momiji_app_prod; then
    docker logs --tail 80 momiji_app_prod || true
    rollback
  fi
  sleep 5
done

docker logs --tail 50 momiji_app_prod

if [ "$ready" -ne 1 ]; then
  rollback
fi

docker rm -f momiji_app_prod_bak >/dev/null 2>&1 || true
echo "Deploy finished."
