package main

import (
	"fmt"

	"github.com/gomodule/redigo/redis"
)

func dbIncrQuantity(db redis.Conn, productID string, amount int64) (err error) {
	defer func() {
		if err != nil {
			db.Do("DISCARD")
		}
	}()

	stockKey := fmt.Sprintf("stock:%s", productID)

	// Incrementing the quantity must be atomic, Redis has INCRBY method but first we need
	// to check quantity doesn't goes below zero
	for {
		if _, err := db.Do("WATCH", stockKey); err != nil {
			return err
		}

		quantity, err := redis.Int64(db.Do("GET", stockKey))
		if err != nil {
			if err != redis.ErrNil {
				return err
			}

			quantity = 0
		}

		if quantity+amount < 0 {
			return ErrInsufficientAmount
		}

		db.Send("MULTI")
		db.Send("SET", stockKey, quantity+amount)

		val, err := db.Do("EXEC")
		if err != nil {
			return err
		}
		if val != nil {
			break
		}
	}

	return nil
}

func dbGetProductStock(db redis.Conn, productID string) (int64, error) {
	stockKey := fmt.Sprintf("stock:%s", productID)
	return redis.Int64(db.Do("GET", stockKey))
}
