package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gomodule/redigo/redis"
)

var redisHost = "localhost:6379"
var ErrInsufficientAmount = errors.New("insufficient amount")

type quantityOp struct {
	product string
	amount  int
	cb      chan error
}

func main() {
	if os.Getenv("REDIS_HOST") != "" {
		redisHost = os.Getenv("REDIS_HOST")
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

	log.Println("start listening at http://localhost:8083")

	http.HandleFunc("/drop", withLogging(withDB(pool, dropHandler)))
	http.HandleFunc("/put", withLogging(withDB(pool, putHandler)))
	http.HandleFunc("/", withLogging(withDB(pool, indexHandler)))
	http.ListenAndServe(":8083", nil)
}

func withDB(pool *redis.Pool, handler func(redis.Conn, http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn := pool.Get()
		defer conn.Close()
		if err := handler(conn, w, r); err != nil {
			log.Printf("ERROR: %v\n", err)
		}
	}
}

func dropHandler(db redis.Conn, w http.ResponseWriter, r *http.Request) error {
	productID := r.URL.Query().Get("product_id")
	if productID == "" {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	var payload struct {
		Quantity int64 `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("quantity has to be a positive number greater than zero"))
		return nil
	}
	if payload.Quantity <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("quantity has to be a positive number greater than zero"))
		return nil
	}

	if err := dbIncrQuantity(db, productID, -payload.Quantity); err != nil {
		if err == ErrInsufficientAmount {
			w.WriteHeader(http.StatusNotAcceptable)
			w.Write([]byte("insufficient quantity"))
			return nil
		}

		w.WriteHeader(http.StatusInternalServerError)
		return err
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func putHandler(db redis.Conn, w http.ResponseWriter, r *http.Request) error {
	productID := r.URL.Query().Get("product_id")
	if productID == "" {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	var payload struct {
		Quantity int64 `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}
	if payload.Quantity <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("quantity can't be equal or greater than zero"))
		return nil
	}

	if err := dbIncrQuantity(db, productID, payload.Quantity); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return err
	}

	w.WriteHeader(http.StatusOK)
	return nil
}

func indexHandler(db redis.Conn, w http.ResponseWriter, r *http.Request) error {
	productID := r.URL.Query().Get("product_id")
	if productID == "" {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	quantity, err := dbGetProductStock(db, productID)
	if err != nil {
		if err == redis.ErrNil {
			w.WriteHeader(http.StatusNotFound)
			return nil
		}

		w.WriteHeader(http.StatusInternalServerError)
		return err
	}

	payload := struct {
		ProductID string `json:"product_id"`
		Quantity  int64  `json:"quantity"`
	}{productID, quantity}

	respBody, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return err
	}

	w.WriteHeader(200)
	w.Write(respBody)
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
