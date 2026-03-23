package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

func GetServerPort() string {
	return strings.TrimSpace(os.Getenv("SERVER_PORT"))
}

func GetServerAddr() string {
	port := GetServerPort()
	if port == "" {
		return ""
	}

	if strings.HasPrefix(port, ":") {
		return port
	}

	return ":" + port
}

func ValidateServerConfig() error {
	port := GetServerPort()
	if port == "" {
		return fmt.Errorf("SERVER_PORT is required")
	}

	if _, err := net.LookupPort("tcp", port); err != nil {
		return fmt.Errorf("SERVER_PORT must be a valid TCP port")
	}

	return nil
}
