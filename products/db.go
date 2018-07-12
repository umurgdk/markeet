package main

import (
	"fmt"
	"log"
	"strconv"

	"github.com/gomodule/redigo/redis"
)

func dbGetAllProducts(db redis.Conn, startFrom string, maxItems int) ([]product, string, error) {
	if startFrom == "" {
		startFrom = "+inf"
	} else {
		startFrom = "(" + startFrom
	}

	productKeys, err := redis.Strings(db.Do("ZREVRANGEBYSCORE", "products", startFrom, "-inf", "LIMIT", 0, maxItems))
	if err != nil {
		return nil, "", err
	}

	for _, key := range productKeys {
		db.Send("HGETALL", key)
	}
	if err := db.Flush(); err != nil {
		return nil, "", err
	}

	products := make([]product, 0, len(productKeys))
	for _, _ = range productKeys {
		values, err := redis.Values(db.Receive())
		if err != nil {
			return nil, "", err
		}

		var p product
		if err := redis.ScanStruct(values, &p); err != nil {
			return nil, "", err
		}

		products = append(products, p)
	}

	if len(products) == 0 {
		return nil, "", nil
	}

	nextKey := strconv.FormatInt(products[len(products)-1].CreatedAt, 10)
	return products, nextKey, nil
}

func dbInsertProduct(db redis.Conn, product product) error {
	productKey := fmt.Sprintf("products:%s", product.Id)
	if _, err := db.Do("HSET", redis.Args{}.Add(productKey).AddFlat(&product)...); err != nil {
		return err
	}

	if _, err := db.Do("ZADD", "products", product.CreatedAt, productKey); err != nil {
		db.Do("DEL", productKey)
		return err
	}

	return nil
}

func dbDeleteProduct(db redis.Conn, productID string) error {
	productKey := fmt.Sprintf("products:%s", productID)
	if _, err := db.Do("DEL", productKey); err != nil {
		return err
	}

	if _, err := db.Do("ZREM", productKey); err != nil {
		log.Printf("WARN: product not found in list")
	}

	return nil
}
