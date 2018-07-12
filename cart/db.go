package main

import (
	"fmt"

	"github.com/gomodule/redigo/redis"
)

func dbCartGetItems(db redis.Conn, userID string) ([]cartItem, error) {
	cartKey := fmt.Sprintf("cart:%s", userID)
	itemQuantity := fmt.Sprintf("%s:*->quantity", cartKey)

	res, err := db.Do("SORT", cartKey, "BY", "nosort", "GET", "#", "GET", itemQuantity)
	values, err := redis.Values(res, err)
	if err != nil {
		return nil, err
	}

	var items []cartItem
	if err := redis.ScanSlice(values, &items); err != nil {
		return nil, err
	}

	return items, nil
}

func dbCartAddItem(db redis.Conn, userID, productID string, quantity int) error {
	cartKey := fmt.Sprintf("cart:%s", userID)
	_, err := db.Do("SADD", cartKey, productID)
	if err != nil {
		return err
	}

	itemKey := fmt.Sprintf("%s:%s", cartKey, productID)
	_, err = db.Do("HINCRBY", itemKey, "quantity", quantity)
	return err
}

func dbCartDeleteItem(db redis.Conn, userID, productID string, quantity int) error {
	cartKey := fmt.Sprintf("cart:%s", userID)
	itemKey := fmt.Sprintf("%s:%s", productID)

	newQuantity, err := redis.Int(db.Do("HINCRBY", itemKey, "quantity", -quantity))
	if err != nil {
		return err
	}

	if newQuantity <= 0 {
		_, err := db.Do("SREM", cartKey, productID)
		if err != nil {
			return err
		}

		if _, err := db.Do("DEL", itemKey); err != nil {
			return err
		}
	}

	return nil
}

func dbRemoveCartItems(db redis.Conn, userID string, cartItems []cartItem) error {
	var productIds []string
	for _, item := range cartItems {
		productIds = append(productIds, item.ProductID)
	}

	cartKey := fmt.Sprintf("cart:%s", userID)
	_, err := db.Do("SREM", redis.Args{}.Add(cartKey).AddFlat(&productIds))
	if err != nil {
		return err
	}

	var itemKeys []string
	for _, item := range cartItems {
		itemKeys = append(itemKeys, fmt.Sprintf("cart:%s:%s", userID, item.ProductID))
	}

	_, err = db.Do("DEL", redis.Args{}.AddFlat(&itemKeys))
	return err
}
