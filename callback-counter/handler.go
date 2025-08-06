package function

import (
	"log"
	"net/http"
	"sync/atomic"
)

var callbackCounter atomic.Int64

func Handle(w http.ResponseWriter, r *http.Request) {
	callbackCounter.Add(1)

	log.Printf("Received callback, count: %d", callbackCounter.Load())

	w.WriteHeader(http.StatusOK)
}
