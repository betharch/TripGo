#!/bin/sh
set -eu

IMAGE=${IMAGE:-trip-service:dev}
HOST_PORT=${HOST_PORT:-18080}

if ! docker info >/dev/null 2>&1; then
    echo "Docker не запущен или недоступен" >&2
    exit 1
fi
if [ ! -f .env ]; then
    echo "нет .env: выполните tripgoctl environment start" >&2
    exit 1
fi
set -a
. ./.env
set +a

node=$(docker ps --filter label=io.x-k8s.kind.cluster=tripgo-local --format '{{.Names}}' | head -n 1)
if [ -z "$node" ]; then
    echo "узел kind-кластера tripgo-local не найден: выполните tripgoctl cluster start" >&2
    exit 1
fi

node_port() {
    port=$(docker inspect -f '{{range $p, $b := .NetworkSettings.Ports}}{{range $b}}{{if eq .HostPort "'"$2"'"}}{{$p}} {{end}}{{end}}{{end}}' "$node" | awk '{print $1}')
    port=${port%/tcp}
    case "$port" in
        '' | *[!0-9]*)
            echo "не удалось найти порт $1 на узле kind (порт в .env: ${2:-?})" >&2
            return 1
            ;;
    esac
    printf '%s' "$port"
}

db_port=$(printf '%s' "$DATABASE_URL" | sed -n 's#^[^@]*@[^:/]*:\([0-9][0-9]*\)/.*#\1#p')
db_node_port=$(node_port PostgreSQL "$db_port")
DATABASE_URL=$(printf '%s' "$DATABASE_URL" | sed "s#@[^:/]*:[0-9][0-9]*/#@${node}:${db_node_port}/#")
export DATABASE_URL

push_port=$(printf '%s' "${PUSH_HTTP_URL:-}" | sed -n 's#^http://[^:/]*:\([0-9][0-9]*\).*#\1#p')
push_node_port=$(node_port "Push Service" "$push_port")
PUSH_HTTP_URL="http://${node}:${push_node_port}"
export PUSH_HTTP_URL

shutdown_secs=$(printf '%s' "${SHUTDOWN_TIMEOUT:-}" | sed -n 's/^\([0-9][0-9]*\)s$/\1/p')
stop_timeout=$(( ${shutdown_secs:-10} + 5 ))

echo "trip-service: http://127.0.0.1:${HOST_PORT}, PostgreSQL: ${node}:${db_node_port}, Push Service: ${node}:${push_node_port} (сеть kind), stop-timeout ${stop_timeout}s"

exec docker run --rm --name trip-service \
    --network kind \
    --stop-timeout "$stop_timeout" \
    -p "127.0.0.1:${HOST_PORT}:8080" \
    --env-file .env \
    -e HTTP_ADDR=:8080 \
    -e DATABASE_URL \
    -e PUSH_HTTP_URL \
    "$IMAGE"
