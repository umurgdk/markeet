#!/bin/sh

set -e

docker build -f cart/Dockerfile -t markeet-cart:dev .
docker build -f orders/Dockerfile -t markeet-orders:dev .
docker build -f products/Dockerfile -t markeet-products:dev .
docker build -f stock/Dockerfile -t markeet-stock:dev .
