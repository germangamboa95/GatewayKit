package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	go func() { log.Fatal(http.ListenAndServe(":9090", mux)) }()
	log.Fatal(http.ListenAndServe(":8080", mux))
}
