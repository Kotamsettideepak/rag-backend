package main

import (
	"context"
	"fmt"
	"io"
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
	log.SetOutput(io.Discard)
	log.SetFlags(0)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	gin.SetMode(gin.ReleaseMode)

	if err := config.LoadEnvFile(".env"); err != nil {
		if !os.IsNotExist(err) {
			fatalf("[startup] failed to load .env: %v", err)
		}
	}
	if err := config.ValidateServerConfig(); err != nil {
		fatalf("[startup] invalid server config: %v", err)
	}
	if err := config.ValidateGroqConfig(); err != nil {
		fatalf("[startup] invalid groq config: %v", err)
	}
	if err := config.ValidateJinaConfig(); err != nil {
		fatalf("[startup] invalid jina config: %v", err)
	}
	if err := config.ValidateDeepgramConfig(); err != nil {
		fatalf("[startup] invalid deepgram config: %v", err)
	}
	if err := config.ValidateGeminiConfig(); err != nil {
		fatalf("[startup] invalid gemini config: %v", err)
	}
	if err := config.ValidateCloudinaryConfig(); err != nil {
		fatalf("[startup] invalid cloudinary config: %v", err)
	}

	serverAddr := config.GetServerAddr()

	if err := config.EnsureChromaRunning(); err != nil {
		fatalf("[startup] failed to ensure chroma is running: %v", err)
	}
	if err := config.ValidateExtractorConfig(); err != nil {
		fatalf("[startup] invalid extractor config: %v", err)
	}

	if err := store.InitDefaultStore(context.Background()); err != nil {
		fatalf("[startup] failed to initialize postgres store: %v", err)
	}
	defer store.DefaultStore().Close()

	apiKeys := config.GetJinaAPIKeys()
	if len(apiKeys) == 0 {
		fatalf("[startup] at least one JINA_API_KEY is required")
	}

	embeddingRepo := embedding.NewJinaEmbeddingRepository(apiKeys)
	embeddingService := embedding.NewService(embeddingRepo)
	manager := ingest.NewManager(embeddingService)
	ingest.SetDefaultManager(manager)
	defer manager.Shutdown()

	router := gin.New()
	router.Use(gin.Recovery())
	router.MaxMultipartMemory = getEnvBytes("MAX_UPLOAD_SIZE_MB", 300)
	router.Use(corsMiddleware())

	routes.RegisterRoutes(router)

	server := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatalf("[startup] server failed: %v", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		fatalf("[shutdown] graceful server shutdown failed: %v", err)
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

func fatalf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
