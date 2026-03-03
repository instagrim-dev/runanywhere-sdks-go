package runanywhere

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// ServerLauncher starts and stops the runanywhere-server binary.
// Primary use case is "user runs server manually"; this is a convenience for examples and local dev.
type ServerLauncher struct {
	// Path is the path to the runanywhere-server binary.
	// If empty, Path is resolved from RunAnywhereServerPath().
	Path string

	// ModelPath is the path to the GGUF model file (required; --model).
	ModelPath string
	// Host is the host to bind to (default "127.0.0.1").
	Host string
	// Port is the port to listen on (default 8080).
	Port int
	// Threads is the number of threads (default 4).
	Threads int
	// ContextSize is the context window size (default 8192).
	ContextSize int
	// GPULayers is the number of GPU layers to offload (default 0).
	GPULayers int
	// EnableCORS enables CORS when true; nil means default true (CORS enabled).
	EnableCORS *bool
	// Verbose enables verbose logging.
	Verbose bool

	cmd *exec.Cmd
}

// RunAnywhereServerPath returns the default path to the runanywhere-server binary.
// It checks RAC_SERVER_PATH first, then a path relative to the repo (build/runanywhere-server/tools/runanywhere-server).
// Returns empty string if not found.
func RunAnywhereServerPath() string {
	if p := os.Getenv("RAC_SERVER_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Well-known relative path from repo root (e.g. when running from repo)
	const relUnix = "build/runanywhere-server/tools/runanywhere-server"
	const relWin = "build/runanywhere-server/tools/Release/runanywhere-server.exe"
	var rel string
	if runtime.GOOS == "windows" {
		rel = relWin
	} else {
		rel = relUnix
	}
	// Try cwd and a few parents (for examples running from sdk/runanywhere-go or repo root)
	for _, dir := range []string{".", "..", "../..", "../../.."} {
		p, _ := filepath.Abs(filepath.Join(dir, rel))
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Start starts the server process. It does not block; the process runs until Stop is called or ctx is cancelled.
func (l *ServerLauncher) Start(ctx context.Context) error {
	path := l.Path
	if path == "" {
		path = RunAnywhereServerPath()
	}
	if path == "" {
		return fmt.Errorf("runanywhere-server binary not found: set RAC_SERVER_PATH or run from repo with build/runanywhere-server/tools/runanywhere-server")
	}
	if l.ModelPath == "" {
		return fmt.Errorf("ModelPath is required")
	}
	host := l.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := l.Port
	if port == 0 {
		port = 8080
	}
	threads := l.Threads
	if threads == 0 {
		threads = 4
	}
	contextSize := l.ContextSize
	if contextSize == 0 {
		contextSize = 8192
	}
	args := []string{
		"--model", l.ModelPath,
		"--host", host,
		"--port", strconv.Itoa(port),
		"--threads", strconv.Itoa(threads),
		"--context", strconv.Itoa(contextSize),
	}
	if l.GPULayers != 0 {
		args = append(args, "--gpu-layers", strconv.Itoa(l.GPULayers))
	}
	enableCORS := l.EnableCORS == nil || *l.EnableCORS
	if enableCORS {
		args = append(args, "--cors")
	} else {
		args = append(args, "--no-cors")
	}
	if l.Verbose {
		args = append(args, "--verbose")
	}
	l.cmd = exec.CommandContext(ctx, path, args...)
	l.cmd.Stdout = os.Stdout
	l.cmd.Stderr = os.Stderr
	if err := l.cmd.Start(); err != nil {
		return err
	}
	return nil
}

// Stop stops the server process (SIGTERM then SIGKILL if needed).
func (l *ServerLauncher) Stop() error {
	if l.cmd == nil || l.cmd.Process == nil {
		return nil
	}
	sig := os.Interrupt
	if runtime.GOOS != "windows" {
		sig = syscall.SIGTERM
	}
	if err := l.cmd.Process.Signal(sig); err != nil {
		_ = l.cmd.Process.Kill()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- l.cmd.Wait() }()
	select {
	case <-done:
		l.cmd = nil
		return nil
	case <-time.After(5 * time.Second):
		_ = l.cmd.Process.Kill()
		<-done
		l.cmd = nil
		return nil
	}
}

// WaitReady polls GET /health at baseURL until the server responds 200 or ctx is done.
// If ctx has no deadline, a default timeout of 60s is applied to avoid polling forever.
func (l *ServerLauncher) WaitReady(ctx context.Context, baseURL string) error {
	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}
	client := NewClient(baseURL)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := client.Health(ctx)
			if err != nil {
				continue
			}
			if resp != nil && resp.Status == "ok" {
				return nil
			}
		}
	}
}
