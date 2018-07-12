#!/bin/sh

set -e

echo Checking markeet network status...
if ! docker network inspect markeet > /dev/null 2>&1; then
  docker network create markeet
fi

echo Stopping previous containers...
docker container rm markeet-cart     > /dev/null 2>&1 || true
docker container rm markeet-orders   > /dev/null 2>&1 || true
docker container rm markeet-products > /dev/null 2>&1 || true
docker container rm markeet-stock    > /dev/null 2>&1 || true

echo Running markeet containers...
docker run -d --rm --name markeet-cart     markeet-cart:dev      > /dev/null
docker run -d --rm --name markeet-orders   markeet-orders:dev    > /dev/null
docker run -d --rm --name markeet-products markeet-products:dev  > /dev/null
docker run -d --rm --name markeet-stock    markeet-stock:dev     > /dev/null

echo OK
