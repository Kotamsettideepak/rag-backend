package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"gin-backend/config"
	"gin-backend/embedding"
	"gin-backend/ingest"
	"gin-backend/routes"
	"gin-backend/store"

	"github.com/gin-gonic/gin"
)

func main() {
	log.Printf("[startup] starting Gin backend on :8080")

	if err := config.LoadEnvFile(".env"); err != nil {
		if !os.IsNotExist(err) {
			log.Fatalf("[startup] failed to load .env: %v", err)
		}
		log.Printf("[startup] .env not found, using existing process environment")
	}

	if err := config.EnsureChromaRunning(); err != nil {
		log.Fatalf("[startup] failed to ensure chroma is running: %v", err)
	}
	if err := config.ValidateExtractorConfig(); err != nil {
		log.Fatalf("[startup] invalid extractor config: %v", err)
	}

	if err := store.InitDefaultStore(context.Background()); err != nil {
		log.Fatalf("[startup] failed to initialize postgres store: %v", err)
	}
	defer store.DefaultStore().Close()

	apiKey := config.GetJinaAPIKey()
	if apiKey == "" {
		log.Fatal("[startup] JINA_API_KEY is required")
	}

	embeddingRepo := embedding.NewJinaEmbeddingRepository(apiKey)
	embeddingService := embedding.NewService(embeddingRepo)
	manager := ingest.NewManager(embeddingService)
	ingest.SetDefaultManager(manager)
	defer manager.Shutdown()

	router := gin.Default()
	router.MaxMultipartMemory = getEnvBytes("MAX_UPLOAD_SIZE_MB", 128)
	router.Use(corsMiddleware())

	log.Printf("[startup] registering routes")
	routes.RegisterRoutes(router)

	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		log.Printf("[startup] backend ready and listening on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[startup] server failed: %v", err)
		}
	}()

	waitForShutdown(server)
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func waitForShutdown(server *http.Server) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop
	log.Printf("[shutdown] signal received, shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[shutdown] graceful server shutdown failed: %v", err)
	}
}

func getEnvBytes(key string, fallbackMB int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallbackMB << 20
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return fallbackMB << 20
	}

	return value << 20
}
