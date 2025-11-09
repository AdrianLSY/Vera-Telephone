package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"message":   "Hello from test backend!",
			"timestamp": time.Now().Format(time.RFC3339),
			"method":    r.Method,
			"path":      r.URL.Path,
			"query":     r.URL.RawQuery,
			"headers":   r.Header,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Error encoding response: %v", err)
		}

		log.Printf("%s %s - 200 OK", r.Method, r.URL.Path)
	})

	http.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{
			"status":  "ok",
			"message": "Test endpoint working!",
			"path":    "/test",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Error encoding response: %v", err)
		}

		log.Printf("%s %s - 200 OK", r.Method, r.URL.Path)
	})

	http.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"method":  r.Method,
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			"headers": r.Header,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Error encoding response: %v", err)
		}

		log.Printf("%s %s - 200 OK", r.Method, r.URL.Path)
	})

	log.Println("Test backend server starting on :8080")
	log.Println("Endpoints:")
	log.Println("  GET /")
	log.Println("  GET /test")
	log.Println("  GET /echo")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
