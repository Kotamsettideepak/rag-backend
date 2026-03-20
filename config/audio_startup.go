package config

import (
	"os/exec"
	"sync"
)

var (
	audioServiceMu      sync.Mutex
	audioServiceCmd     *exec.Cmd
	audioServiceStarted bool
)

func EnsureAudioServiceRunning() error {
	return ensureManagedServiceRunning(managedService{
		mu:      &audioServiceMu,
		cmd:     &audioServiceCmd,
		started: &audioServiceStarted,
		name:    "audio service",
		baseURL: GetAudioServiceBaseURL(),
		envKey:  "AUDIO_SERVICE",
		port:    "8001",
		workdir: "audio-service",
	})
}

func ShutdownAudioServiceIfStarted() {
	shutdownManagedService(managedService{
		mu:      &audioServiceMu,
		cmd:     &audioServiceCmd,
		started: &audioServiceStarted,
		name:    "audio service",
		baseURL: GetAudioServiceBaseURL(),
		envKey:  "AUDIO_SERVICE",
		port:    "8001",
		workdir: "audio-service",
	})
}
