package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gomodule/redigo/redis"
)

type OrderStatus string

const (
	OrderPreparing OrderStatus = "preparing"
	OrderShipped               = "shipped"
	OrderDelivered             = "arrived"
)

type order struct {
	Id           string      `json:"id"`
	UserID       string      `json:"user_id"`
	ProductID    string      `json:"product_id"`
	Quantity     int         `json:"quantity"`
	CreatedAt    int64       `json:"created_at"`
	Status       OrderStatus `json:"status"`
	DroppedStock bool        `json:"-" redis:"-"`
}

type stockInfo struct {
	ProductID string `json:"product_id"`
	Quanity   int    `json:"quantity"`
}

var notFoundError = errors.New("not found")
var notEnoughStockError = errors.New("not enough stock")

var stockHost = "stocks"

func main() {
	if os.Getenv("STOCK_HOST") != "" {
		stockHost = os.Getenv("STOCK_HOST")
	}

	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost:6379"
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

	log.Printf("Listening at http://localhost:8080")
	http.HandleFunc("/", withLogging(withDB(pool, ordersHandler)))
	http.ListenAndServe(":8080", nil)
}

func withDB(pool *redis.Pool, handler func(redis.Conn, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn := pool.Get()
		defer conn.Close()
		handler(conn, w, r)
	}
}

func ordersHandler(db redis.Conn, w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		w.WriteHeader(404)
		w.Write([]byte("User ID not found"))
		return
	}

	switch r.Method {
	case http.MethodDelete:
		orderID := r.URL.Query().Get("order_id")
		if orderID == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		order, err := dbGetOrder(db, userID, orderID)
		if err != nil {
			if err == redis.ErrNil {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			log.Printf("ERROR: failed to get order: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := dbDeleteOrder(db, userID, orderID); err != nil {
			if err == redis.ErrNil {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			log.Printf("ERROR: failed to delete order: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := putItemsToStock(order.ProductID, order.Quantity); err != nil {
			log.Printf("CRITICAL: product '%s', stock couldn't updated\n", order.ProductID)
		}

		w.WriteHeader(http.StatusOK)
		return
	case http.MethodGet:
		orders, err := dbGetOrders(db, userID)
		if err != nil {
			log.Printf("ERROR: failed to retrieve orders for userID: '%s' with: %v\n", userID, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		payload, err := json.Marshal(orders)
		if err != nil {
			log.Printf("ERROR: failed to encode json: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(payload)
		return

	case http.MethodPost:
		var payload order
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Printf("ERROR: failed encode json: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		stockInfo, err := getStockInfo(payload.ProductID)
		if err != nil {
			if err == notFoundError {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("not enough stock"))
				return
			}

			log.Printf("ERROR: failed to get stock info: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if stockInfo.Quanity < payload.Quantity {
			w.WriteHeader(http.StatusNotAcceptable)
			w.Write([]byte("not enough stock"))
			return
		}

		orderID, err := dbInsertOrder(db, userID, payload)
		if err != nil {
			log.Printf("ERROR: failed to insert order record: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := dropFromStock(payload.ProductID, payload.Quantity); err != nil {
			// revert order record
			dbDeleteOrder(db, userID, orderID)

			if err == notEnoughStockError {
				w.WriteHeader(http.StatusNotAcceptable)
				w.Write([]byte("not enough stock"))
				return
			}

			log.Printf("ERROR: failed to drop ordered items from stock: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		responsePayload := map[string]string{"order_id": orderID}
		response, err := json.Marshal(responsePayload)
		if err != nil {
			log.Printf("ERROR: failed to encode json: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Write(response)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

func getStockInfo(productID string) (*stockInfo, error) {
	client := &http.Client{}

	reqParams := url.Values{"product_id": []string{productID}}
	reqURL := fmt.Sprintf("http://%s?%s", stockHost, reqParams.Encode())

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusNotFound {
			return nil, notFoundError
		}

		return nil, errors.New(res.Status)
	}

	var info stockInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

func dropFromStock(productID string, quantity int) error {
	client := &http.Client{}

	reqParams := url.Values{"product_id": []string{productID}}
	reqURL := fmt.Sprintf("http://%s/drop?%s", stockHost, reqParams.Encode())

	payload := struct {
		Quantity int `json:"quantity"`
	}{quantity}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodGet, reqURL, bytes.NewReader(body))
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusNotAcceptable {
			return notEnoughStockError
		}

		return errors.New(res.Status)
	}

	return nil
}

func putItemsToStock(productID string, quantity int) error {
	client := &http.Client{}

	reqParams := url.Values{"product_id": []string{productID}}
	reqURL := fmt.Sprintf("http://%s/put?%s", stockHost, reqParams.Encode())

	payload := struct {
		Quantity int `json:"quantity"`
	}{quantity}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("GET", reqURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	res, err := client.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}

	return nil
}

type loggingResponseWriter struct {
	w          http.ResponseWriter
	StatusCode int
	Body       []byte
}

func (l *loggingResponseWriter) Header() http.Header {
	return l.w.Header()
}

func (l *loggingResponseWriter) Write(bytes []byte) (int, error) {
	l.Body = append(l.Body, bytes...)
	return l.w.Write(bytes)
}

func (l *loggingResponseWriter) WriteHeader(statusCode int) {
	l.StatusCode = statusCode
	l.w.WriteHeader(statusCode)
}

func withLogging(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[%s] %v\n", r.Method, r.URL)
		writer := loggingResponseWriter{w, 0, nil}
		handler(&writer, r)
		log.Printf("-> %d -- %s", writer.StatusCode, string(writer.Body))
	}
}
