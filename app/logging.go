package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

type operationLogger func(level, message string)

var (
	operationLoggerMu      sync.RWMutex
	currentOperationLogger operationLogger
	backendLoggerOnce      sync.Once
	eventEmitReady         atomic.Bool
)

func setOperationLogger(fn operationLogger) func() {
	operationLoggerMu.Lock()
	previous := currentOperationLogger
	currentOperationLogger = fn
	operationLoggerMu.Unlock()

	return func() {
		operationLoggerMu.Lock()
		currentOperationLogger = previous
		operationLoggerMu.Unlock()
	}
}

func emitOperationLog(level, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	fmt.Printf("[%s] %s\n", level, message)

	operationLoggerMu.RLock()
	logger := currentOperationLogger
	operationLoggerMu.RUnlock()
	if logger != nil {
		logger(level, message)
		return
	}
	appStore.appendLog(0, level, message)
}

func emitOperationLogf(level, format string, args ...any) {
	emitOperationLog(level, fmt.Sprintf(format, args...))
}

func ConfigureBackendLogger() {
	backendLoggerOnce.Do(func() {
		log.SetFlags(0)
		log.SetOutput(io.MultiWriter(os.Stdout, &appLogWriter{}))
	})
}

func markEventEmitReady() {
	eventEmitReady.Store(true)
}

func canEmitEvent() bool {
	return eventEmitReady.Load()
}

type appLogWriter struct{}

func (w *appLogWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text == "" {
		return len(p), nil
	}
	for _, line := range strings.Split(text, "\n") {
		emitOperationLog("backend", line)
	}
	return len(p), nil
}
