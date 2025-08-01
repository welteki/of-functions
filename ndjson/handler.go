package function

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Data represents the structure of the data we'll be streaming.
type Data struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Counter   int    `json:"counter"`
}

func Handle(w http.ResponseWriter, r *http.Request) {
	// 1. Set the Content-Type header FIRST.
	w.Header().Set("Content-Type", "application/x-ndjson")

	// 2. Do NOT call w.WriteHeader(http.StatusOK) here.
	// The first w.Write() call will implicitly send the 200 OK status.

	// Flush the headers immediately to the client
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	counter := 0

	for counter < 100 { // Loop up to 100 events
		select {
		case <-ticker.C:
			counter++
			data := Data{
				Timestamp: time.Now().Format(time.RFC3339),
				Message:   fmt.Sprintf("This is message number %d", counter),
				Counter:   counter,
			}

			// Marshal the data to JSON
			jsonData, err := json.Marshal(data)
			if err != nil {
				log.Printf("Error marshalling JSON: %v", err)
				// Attempt to send an error message and continue if possible,
				// or return if the error is severe.
				fmt.Fprintf(w, `{"error": "Failed to marshal JSON for message %d"}`+"\n", counter)
				flusher.Flush()
				continue
			}

			// Write the JSON data followed by a newline character
			_, err = w.Write(jsonData)
			if err != nil {
				log.Printf("Error writing JSON data to response: %v", err)
				return // Client disconnected or an error occurred, stop streaming
			}
			_, err = w.Write([]byte("\n")) // Crucial for NDJSON
			if err != nil {
				log.Printf("Error writing newline to response: %v", err)
				return // Client disconnected or an error occurred, stop streaming
			}

			// Flush the buffer to send the data immediately to the client
			flusher.Flush()

		case <-ctx.Done():
			// Client disconnected
			log.Println("Client disconnected.")
			return
		}
	}
	log.Println("Finished streaming 100 events.")
}
