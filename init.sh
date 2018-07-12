#!/bin/sh

set -e
set -o xtrace

PRODUCTS=:8081
STOCK=:8083
CART=:8082
ORDERS=:8080

p1_id=$(http post $PRODUCTS name="Logicool mouse" category="oem")
p2_id=$(http post $PRODUCTS name="Kingston 8GB ram" category="oem")

echo "Product 1: ${p1_id}"
echo "Product 2: ${p2_id}"

http post $STOCK/put?product_id=${p1_id} quantity:=10
http post $STOCK/put?product_id=${p2_id} quantity:=3

http post $CART?user_id=umurgdk product_id=${p1_id} quantity:=2 
http post $CART?user_id=umurgdk product_id=${p2_id} quantity:=1
http post $CART?user_id=umurgdk product_id=${p2_id} quantity:=1

http post $CART/checkout?user_id=umurgdk


http $STOCK?product_id=${p1_id}
http $STOCK?product_id=${p2_id}
