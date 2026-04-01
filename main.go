package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gin-backend/config"
	"gin-backend/middleware"
	"gin-backend/repository"
	"gin-backend/routes"
	"gin-backend/service/ingestion"
	"gin-backend/service/ingestion/embedding"
	"gin-backend/service/quiz"
	"gin-backend/service/rerank"
	"gin-backend/service/topicingest"

	"github.com/gin-gonic/gin"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if err := config.LoadEnvFile(".env"); err != nil && !os.IsNotExist(err) {
		fatalf("[startup] failed to load .env: %v", err)
	}
	configureLogging()

	for _, check := range []struct {
		name string
		fn   func() error
	}{
		{"server", config.ValidateServerConfig},
		{"groq", config.ValidateGroqConfig},
		{"jina", config.ValidateJinaConfig},
		{"deepgram", config.ValidateDeepgramConfig},
		{"gemini", config.ValidateGeminiConfig},
		{"cloudinary", config.ValidateCloudinaryConfig},
		{"extractor", config.ValidateExtractorConfig},
		{"quiz-services", config.ValidateQuizServicesConfig},
	} {
		if err := check.fn(); err != nil {
			fatalf("[startup] invalid %s config: %v", check.name, err)
		}
	}

	if err := repository.InitDefault(context.Background()); err != nil {
		fatalf("[startup] postgres init failed: %v", err)
	}
	defer repository.Default().Close()

	apiKeys := config.GetJinaAPIKeys()
	if len(apiKeys) == 0 {
		fatalf("[startup] at least one JINA_API_KEY is required")
	}
	embedSvc := embedding.NewService(embedding.NewJinaRepository(apiKeys))
	mgr := ingestion.NewManager(embedSvc, rerank.NewService(apiKeys))
	ingestion.SetDefaultManager(mgr)
	topicSvc := topicingest.NewService(embedSvc)
	topicingest.SetDefault(topicSvc)
	quizSvc := quiz.NewService()
	quiz.SetDefault(quizSvc)
	defer mgr.Shutdown()
	defer topicSvc.Shutdown()
	defer quizSvc.Shutdown()

	router := gin.New()
	if gin.Mode() != gin.ReleaseMode {
		router.Use(gin.Logger())
	}
	router.Use(gin.Recovery())
	router.Use(middleware.CORS())
	router.MaxMultipartMemory = envMB("MAX_UPLOAD_SIZE_MB", 300)
	routes.Register(router)

	srv := &http.Server{
		Addr:    config.GetServerAddr(),
		Handler: router,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatalf("[startup] server failed: %v", err)
		}
	}()

	gracefulShutdown(srv)
}

func configureLogging() {
	if isProductionLogging() {
		gin.SetMode(gin.ReleaseMode)
		return
	}

	gin.SetMode(gin.DebugMode)
	gin.DefaultWriter = os.Stdout
	gin.DefaultErrorWriter = os.Stderr
	log.SetOutput(os.Stdout)
}

func isProductionLogging() bool {
	for _, key := range []string{"APP_ENV", "ENV", "GIN_MODE"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if value == "production" || value == "release" {
			return true
		}
	}
	return false
}

func gracefulShutdown(srv *http.Server) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		fatalf("[shutdown] graceful shutdown failed: %v", err)
	}
}

func envMB(key string, fallbackMB int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallbackMB << 20
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return fallbackMB << 20
	}
	return v << 20
}

func fatalf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
