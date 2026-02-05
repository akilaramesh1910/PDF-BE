package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/akila/document-converter/handlers"
	"github.com/akila/document-converter/workers"
)

func main() {
	numCPU := runtime.NumCPU()
	log.Printf("Starting backend with %d workers per engine", numCPU)

	// Initialize engines
	mgr := workers.NewEngineManager(numCPU)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.Start(ctx)

	// Handlers
	h := handlers.NewConversionHandler(mgr)

	mux := http.NewServeMux()
	mux.HandleFunc("/convert", h.HandleConvert)
	mux.HandleFunc("/merge", h.HandleMerge)
	mux.HandleFunc("/split", h.HandleSplit)
	mux.HandleFunc("/compress", h.HandleCompress)
	mux.HandleFunc("/extract/text", h.HandleExtractText)
	mux.HandleFunc("/extract/images", h.HandleExtractImages)
	mux.HandleFunc("/rotate", h.HandleRotate)
	mux.HandleFunc("/reorder", h.HandleReorder)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// TODO: Add other endpoints (/merge, /split, /compress, etc.)

	// CORS Middleware
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		mux.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: corsHandler,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down server...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
		cancel()
	}()

	log.Println("Server listening on :8080")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("ListenAndServe error: %v", err)
	}
}
