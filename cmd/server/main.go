package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"lista-zakupow/internal/httpapi"
	"lista-zakupow/internal/store"
)

func main() {
	addr := getenv("ADDR", ":8080")
	dataFile := getenv("DATA_FILE", "data/shopping-list.json")

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	shoppingStore, err := store.New(dataFile)
	if err != nil {
		logger.Error("cannot initialize store", "error", err)
		os.Exit(1)
	}

	static := http.FileServer(http.Dir("web/static"))
	api := httpapi.New(shoppingStore, static, logger)

	server := &http.Server{
		Addr:              addr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("server listening", "addr", addr, "data_file", dataFile)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
