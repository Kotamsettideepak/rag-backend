package config

import (
	"os/exec"
	"sync"
)

var (
	extractorMu      sync.Mutex
	extractorCmd     *exec.Cmd
	extractorStarted bool
)

func EnsureExtractorRunning() error {
	return ensureManagedServiceRunning(managedService{
		mu:      &extractorMu,
		cmd:     &extractorCmd,
		started: &extractorStarted,
		name:    "extractor service",
		baseURL: GetExtractorBaseURL(),
		envKey:  "EXTRACTOR",
		port:    GetExtractorPort(),
		workdir: "extractor-service",
	})
}

func ShutdownExtractorIfStarted() {
	shutdownManagedService(managedService{
		mu:      &extractorMu,
		cmd:     &extractorCmd,
		started: &extractorStarted,
		name:    "extractor service",
		baseURL: GetExtractorBaseURL(),
		envKey:  "EXTRACTOR",
		port:    GetExtractorPort(),
		workdir: "extractor-service",
	})
}
