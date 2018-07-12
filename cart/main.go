package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

type cartItem struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

var redisHost = "localhost:6379"
var ordersHost = "orders"

var ErrNotFound = errors.New("not found")
var ErrServiceInternal = errors.New("service returned error")

func main() {
	if env := os.Getenv("REDIS_HOST"); env != "" {
		redisHost = env
	}
	if env := os.Getenv("ORDERS_HOST"); env != "" {
		ordersHost = env
	}

	pool := &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", redisHost) },
	}

	conn := pool.Get()
	_, err := conn.Do("PING")
	if err != nil {
		log.Fatalf("failed to ping redis: %v", err)
	}
	conn.Close()

	http.HandleFunc("/", withDB(pool, dispatchCart))
	http.HandleFunc("/checkout", withDB(pool, checkoutHandler))
	log.Println("listening at http://localhost:8082")
	http.ListenAndServe(":8082", nil)
}

func withDB(pool *redis.Pool, handler func(redis.Conn, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn := pool.Get()
		defer conn.Close()
		handler(conn, w, r)
	}
}

func checkoutHandler(db redis.Conn, w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	cartItems, err := dbCartGetItems(db, userID)
	if err != nil {
		if err == redis.ErrNil {
			w.WriteHeader(http.StatusNotAcceptable)
			w.Write([]byte("trying to checkout an empty cart"))
			return
		}
	}

	orderIDs := make([]string, 0, len(cartItems))
	for _, item := range cartItems {
		orderID, err := makeOrder(userID, item)
		if err != nil {
			if err == ErrNotFound {
				http.NotFound(w, r)
				return
			}

			log.Printf("ERROR: failed to make order: %#v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		orderIDs = append(orderIDs, orderID)
	}

	dbRemoveCartItems(db, userID, cartItems)

	body, err := json.Marshal(orderIDs)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write(body)
}

func dispatchCart(db redis.Conn, w http.ResponseWriter, r *http.Request) {
	var err error

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("user_id paramter is missing"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		err = listCart(db, userID, w, r)
	case http.MethodPost:
		err = addToCart(db, userID, w, r)
	case http.MethodDelete:
		err = removeFromCart(db, userID, w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if err != nil {
		w.WriteHeader(500)
		log.Printf("ERROR: %v", err)
		return
	}
}

func listCart(db redis.Conn, userID string, w http.ResponseWriter, r *http.Request) error {
	cartItems, err := dbCartGetItems(db, userID)
	if err != nil {
		if err != redis.ErrNil {
			return err
		}

		cartItems = []cartItem{}
	}

	if cartItems == nil {
		cartItems = []cartItem{}
	}

	body, err := json.Marshal(cartItems)
	if err != nil {
		return err
	}

	w.WriteHeader(http.StatusOK)
	w.Write(body)
	return nil
}

func addToCart(db redis.Conn, userID string, w http.ResponseWriter, r *http.Request) error {
	var payload cartItem
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid payload"))
		return nil
	}

	if err := dbCartAddItem(db, userID, payload.ProductID, payload.Quantity); err != nil {
		return err
	}

	w.WriteHeader(http.StatusCreated)
	return nil
}

func removeFromCart(db redis.Conn, userID string, w http.ResponseWriter, r *http.Request) error {
	productID := r.URL.Query().Get("product_id")
	quantityStr := r.URL.Query().Get("quantity")
	quantity := 1

	if productID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	if quantityStr != "" {
		var err error
		quantity, err = strconv.Atoi(quantityStr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid quantity parameter"))
			return nil
		}
	}

	if err := dbCartDeleteItem(db, userID, productID, quantity); err != nil {
		if err == redis.ErrNil {
			w.WriteHeader(http.StatusNotFound)
			return nil
		}

		return err
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

type order struct {
	UserID    string `json:"user_id"`
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

func makeOrder(userID string, item cartItem) (string, error) {
	order := order{
		UserID:    userID,
		ProductID: item.ProductID,
		Quantity:  item.Quantity,
	}

	reqBody, err := json.Marshal(order)
	if err != nil {
		return "", err
	}

	reqParams := url.Values{}
	reqParams.Add("user_id", userID)
	reqURL := fmt.Sprintf("http://%s?%s", ordersHost, reqParams.Encode())
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}

	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	if res.StatusCode != http.StatusCreated {
		if res.StatusCode == http.StatusNotFound {
			return "", ErrNotFound
		}

		return "", ErrServiceInternal
	}

	var payload struct {
		OrderID string `json:"order_id"`
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("failed to read order response body: %v\nRESPONSE: %s", err, body)
	}

	return payload.OrderID, nil
}
