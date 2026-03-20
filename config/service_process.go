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

type managedService struct {
	mu      *sync.Mutex
	cmd     **exec.Cmd
	started *bool
	name    string
	baseURL string
	envKey  string
	port    string
	workdir string
}

func ensureManagedServiceRunning(service managedService) error {
	if isServiceHealthy(service.baseURL) {
		return nil
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv(service.envKey+"_AUTOSTART")), "false") {
		return fmt.Errorf("%s is not reachable at %s", service.name, service.baseURL)
	}

	service.mu.Lock()
	defer service.mu.Unlock()

	if isServiceHealthy(service.baseURL) {
		return nil
	}

	pythonBin := strings.TrimSpace(os.Getenv(service.envKey + "_PYTHON_BIN"))
	if pythonBin == "" {
		pythonBin = "python"
	}

	workdir, err := resolveManagedServiceWorkdir(service)
	if err != nil {
		return err
	}

	cmd := exec.Command(pythonBin, "-m", "uvicorn", "main:app", "--host", "127.0.0.1", "--port", service.port)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", service.name, err)
	}

	*service.cmd = cmd
	*service.started = true

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if isServiceHealthy(service.baseURL) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	*service.cmd = nil
	*service.started = false
	return fmt.Errorf("%s did not become ready within 30s", service.name)
}

func shutdownManagedService(service managedService) {
	service.mu.Lock()
	defer service.mu.Unlock()

	if !*service.started || *service.cmd == nil || (**service.cmd).Process == nil {
		return
	}

	_ = (**service.cmd).Process.Kill()
	_, _ = (**service.cmd).Process.Wait()
	*service.cmd = nil
	*service.started = false
}

func isServiceHealthy(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func resolveManagedServiceWorkdir(service managedService) (string, error) {
	if workdir := strings.TrimSpace(os.Getenv(service.envKey + "_WORKDIR")); workdir != "" {
		return workdir, nil
	}

	candidates := []string{
		filepath.Join("..", service.workdir),
		service.workdir,
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not locate %s directory; set %s_WORKDIR", service.workdir, service.envKey)
}
