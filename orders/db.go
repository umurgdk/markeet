package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

func dbGetOrder(db redis.Conn, userID, orderID string) (*order, error) {
	orderKey := fmt.Sprintf("orders:%s:%s", userID, orderID)

	orderBytes, err := redis.Bytes(db.Do("GET", orderKey))
	if err != nil {
		return nil, err
	}

	var order order
	err = json.Unmarshal(orderBytes, &order)
	return &order, err
}

func dbGetOrders(db redis.Conn, userID string) ([]order, error) {
	orderListKey := fmt.Sprintf("orders:%s", userID)
	orderKeyMatch := fmt.Sprintf("%s:*", orderListKey)

	values, err := db.Do("SORT", orderListKey, "BY", "nosort", "GET", orderKeyMatch)
	byteSlices, err := redis.ByteSlices(values, err)
	if err != nil {
		return nil, err
	}

	orders := make([]order, 0, len(byteSlices))
	for _, orderBytes := range byteSlices {
		var o order
		if err := json.Unmarshal(orderBytes, &o); err != nil {
			return nil, err
		}

		orders = append(orders, o)
	}

	return orders, nil
}

func dbInsertOrder(db redis.Conn, userID string, order order) (string, error) {
	now := time.Now().UnixNano()
	order.Id = strconv.FormatInt(now+rand.Int63n(100), 10)
	order.CreatedAt = now
	order.UserID = userID

	orderListKey := fmt.Sprintf("orders:%s", userID)
	_, err := db.Do("SADD", orderListKey, order.Id)
	if err != nil {
		return "", err
	}

	orderBytes, err := json.Marshal(order)
	if err != nil {
		return "", err
	}

	orderKey := fmt.Sprintf("%s:%s", orderListKey, order.Id)
	_, err = db.Do("SET", orderKey, orderBytes)
	return order.Id, err
}

func dbDeleteOrder(db redis.Conn, userID, orderID string) error {
	orderListKey := fmt.Sprintf("orders:%s", userID)
	ndel, err := redis.Int64(db.Do("SREM", orderListKey, orderID))
	if err != nil {
		return err
	}
	if ndel == 0 {
		return redis.ErrNil
	}

	orderKey := fmt.Sprintf("%s:%s", orderListKey, orderID)
	_, err = db.Do("DEL", orderKey)
	return err
}
