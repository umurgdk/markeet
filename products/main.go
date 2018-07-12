package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

type product struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	Category  string `json:"category"`
}

func main() {
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
		log.Fatalf("FATAL: failed to ping redis: %v\n", err)
	}
	conn.Close()

	log.Println("listening at http://localhost:8081")

	http.HandleFunc("/", withDB(pool, handleProducts))
	http.ListenAndServe(":8081", nil)
}

func withDB(pool *redis.Pool, handler func(redis.Conn, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := pool.Get()
		defer db.Close()
		handler(db, w, r)
	}
}

func handleProducts(db redis.Conn, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		products, next, err := dbGetAllProducts(db, r.URL.Query().Get("from"), 20)
		if err != nil {
			if r.URL.Query().Get("from") != "" && err == redis.ErrNil {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			log.Printf("ERROR: failed to get products: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if products == nil {
			products = []product{}
		}

		body, err := json.Marshal(struct {
			Products []product `json:"products"`
			NextKey  string    `json:"next_key"`
		}{products, next})
		if err != nil {
			log.Printf("ERROR: failed to encode product json: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
	case http.MethodPost:
		var payload product
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid payload"))
			return
		}

		payload.CreatedAt = time.Now().UnixNano()
		payload.Id = strconv.FormatInt(payload.CreatedAt, 10)

		if err := dbInsertProduct(db, payload); err != nil {
			log.Printf("ERROR: failed to insert product: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(payload.Id))
		return
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if err := dbDeleteProduct(db, id); err != nil {
			if err == redis.ErrNil {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusNotFound)
}
