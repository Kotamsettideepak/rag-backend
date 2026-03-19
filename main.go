package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gin-backend/config"
	"gin-backend/routes"
	"gin-backend/service"

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

	manager := service.DefaultManager()
	defer manager.Shutdown()

	router := gin.Default()
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
