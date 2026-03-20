package config

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	extractorMu      sync.Mutex
	extractorCmd     *exec.Cmd
	extractorStarted bool
)

func EnsureExtractorRunning() error {
	if extractorHealthy() {
		return nil
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv("EXTRACTOR_AUTOSTART")), "false") {
		return fmt.Errorf("extractor service is not reachable at %s", GetExtractorBaseURL())
	}

	extractorMu.Lock()
	defer extractorMu.Unlock()

	if extractorHealthy() {
		return nil
	}

	pythonBin := strings.TrimSpace(os.Getenv("EXTRACTOR_PYTHON_BIN"))
	if pythonBin == "" {
		pythonBin = "python"
	}

	workdir, err := resolveExtractorWorkdir()
	if err != nil {
		return err
	}

	cmd := exec.Command(pythonBin, "-m", "uvicorn", "main:app", "--host", "127.0.0.1", "--port", "8090")
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start extractor service: %w", err)
	}

	extractorCmd = cmd
	extractorStarted = true

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if extractorHealthy() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	extractorCmd = nil
	extractorStarted = false
	return fmt.Errorf("extractor service did not become ready within 20s")
}

func ShutdownExtractorIfStarted() {
	extractorMu.Lock()
	defer extractorMu.Unlock()

	if !extractorStarted || extractorCmd == nil || extractorCmd.Process == nil {
		return
	}

	_ = extractorCmd.Process.Kill()
	_, _ = extractorCmd.Process.Wait()
	extractorCmd = nil
	extractorStarted = false
}

func extractorHealthy() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(GetExtractorBaseURL() + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func resolveExtractorWorkdir() (string, error) {
	if workdir := strings.TrimSpace(os.Getenv("EXTRACTOR_WORKDIR")); workdir != "" {
		return workdir, nil
	}

	candidates := []string{
		filepath.Join("..", "extractor-service"),
		"extractor-service",
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not locate extractor-service directory; set EXTRACTOR_WORKDIR")
}
