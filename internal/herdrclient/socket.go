package herdrclient

import (
	"os"
	"path/filepath"
)

const socketPathEnvVar = "HERDR_SOCKET_PATH"

func SocketPath() string {
	if p := os.Getenv(socketPathEnvVar); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return defaultSocketPath(home, SessionName)
}

func defaultSocketPath(home, session string) string {
	return filepath.Join(home, ".config", "herdr", "sockets", session+".sock")
}
