package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// dockerLogProxyHandler deliberately exposes only logs of the configured stack
// containers. It never forwards arbitrary Docker API requests.
func dockerLogProxyHandler(fetch func(context.Context, string, int) (string, error), allowed map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if r.Method != http.MethodGet || len(parts) != 4 || parts[1] != "containers" || parts[3] != "logs" || !allowed[parts[2]] {
			http.NotFound(w, r)
			return
		}
		tail, err := strconv.Atoi(r.URL.Query().Get("tail"))
		if err != nil || tail < 1 || tail > 5000 {
			http.Error(w, "invalid log tail", http.StatusBadRequest)
			return
		}
		content, err := fetch(r.Context(), parts[2], tail)
		if err != nil {
			http.Error(w, "container logs unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(content))
	})
}

func runDockerLogProxy() error {
	path := env("BDAG_LOG_PROXY_SOCKET", "/run/dashboard-docker-proxy/docker.sock")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket %s", path)
		}
		if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
			conn.Close()
			return fmt.Errorf("log proxy socket already in use")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(path, 0660); err != nil {
		return err
	}
	allowed := map[string]bool{
		env("BDAG_NODE_LOG_CONTAINER", env("BDAG_NODE_CONTAINER", "node")):                true,
		env("BDAG_POOL_LOG_CONTAINER", env("BDAG_POOL_CONTAINER", "pool")):                true,
		env("BDAG_DASHBOARD_LOG_CONTAINER", env("BDAG_DASHBOARD_CONTAINER", "dashboard")): true,
	}
	dockerSocket := env("BDAG_DASHBOARD_DOCKER_SOCKET", "/var/run/docker.sock")
	server := &http.Server{
		Handler: dockerLogProxyHandler(func(ctx context.Context, container string, tail int) (string, error) {
			return dockerContainerLogs(ctx, dockerSocket, container, tail)
		}, allowed),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return server.Serve(listener)
}
