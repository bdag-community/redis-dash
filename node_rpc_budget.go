package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type nodeRPCPriority int

const (
	nodeRPCPriorityNormal nodeRPCPriority = iota
	nodeRPCPriorityLow
)

type nodeRPCPriorityKey struct{}

type nodeRPCBackoffError struct {
	until  time.Time
	reason string
}

func (e nodeRPCBackoffError) Error() string {
	if e.until.IsZero() {
		return "node RPC backoff active"
	}
	reason := strings.TrimSpace(e.reason)
	if reason == "" {
		reason = "node RPC is busy"
	}
	return fmt.Sprintf("node RPC backoff active until %s: %s", e.until.UTC().Format(time.RFC3339), reason)
}

type nodeRPCBudget struct {
	sem     chan struct{}
	client  *http.Client
	backoff time.Duration

	mu            sync.Mutex
	backoffUntil  time.Time
	backoffReason string
}

var dashboardNodeRPCBudget = newNodeRPCBudget(
	intEnv("BDAG_NODE_RPC_MAX_CONCURRENCY", 8),
	durationEnv("BDAG_NODE_RPC_BUSY_BACKOFF_SECONDS", 10*time.Second),
)

func newNodeRPCBudget(maxConcurrent int, backoff time.Duration) *nodeRPCBudget {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if maxConcurrent > 64 {
		maxConcurrent = 64
	}
	if backoff < 0 {
		backoff = 0
	}
	return &nodeRPCBudget{
		sem:     make(chan struct{}, maxConcurrent),
		client:  newNodeHTTPClient(8 * time.Second),
		backoff: backoff,
	}
}

func newNodeHTTPClient(timeout time.Duration) *http.Client {
	maxConns := intEnv("BDAG_NODE_RPC_MAX_CONNS_PER_HOST", intEnv("BDAG_NODE_RPC_MAX_CONCURRENCY", 8))
	if maxConns < 1 {
		maxConns = 1
	}
	if maxConns > 64 {
		maxConns = 64
	}
	idleConns := intEnv("BDAG_NODE_RPC_MAX_IDLE_CONNS_PER_HOST", maxConns)
	if idleConns < 1 {
		idleConns = 1
	}
	if idleConns > maxConns {
		idleConns = maxConns
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxConnsPerHost:       maxConns,
		MaxIdleConns:          maxConns,
		MaxIdleConnsPerHost:   idleConns,
		IdleConnTimeout:       durationEnv("BDAG_NODE_RPC_IDLE_CONN_TIMEOUT_SECONDS", 30*time.Second),
		ResponseHeaderTimeout: durationEnv("BDAG_NODE_RPC_RESPONSE_HEADER_TIMEOUT_SECONDS", 10*time.Second),
		ExpectContinueTimeout: time.Second,
	}
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func withLowPriorityNodeRPC(ctx context.Context) context.Context {
	return context.WithValue(ctx, nodeRPCPriorityKey{}, nodeRPCPriorityLow)
}

func nodeRPCPriorityFromContext(ctx context.Context) nodeRPCPriority {
	if value, ok := ctx.Value(nodeRPCPriorityKey{}).(nodeRPCPriority); ok {
		return value
	}
	return nodeRPCPriorityNormal
}

func (b *nodeRPCBudget) do(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	if b == nil {
		if client == nil {
			client = http.DefaultClient
		}
		return client.Do(req)
	}
	if nodeRPCPriorityFromContext(ctx) == nodeRPCPriorityLow {
		if until, reason, ok := b.backoffState(); ok {
			return nil, nodeRPCBackoffError{until: until, reason: reason}
		}
	}
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
	case <-ctx.Done():
		b.noteError(ctx.Err())
		return nil, ctx.Err()
	}
	if client == nil {
		client = b.client
	}
	resp, err := client.Do(req)
	if err != nil {
		b.noteError(err)
		return nil, err
	}
	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
		b.noteBusy(fmt.Sprintf("%s returned %s", req.URL.Host, resp.Status))
	}
	return resp, nil
}

func (b *nodeRPCBudget) noteError(err error) bool {
	if b == nil || !isNodeRPCBusyError(err) {
		return false
	}
	b.noteBusy(err.Error())
	return true
}

func (b *nodeRPCBudget) noteBusy(reason string) {
	if b == nil || b.backoff <= 0 {
		return
	}
	until := time.Now().Add(b.backoff)
	b.mu.Lock()
	if until.After(b.backoffUntil) {
		b.backoffUntil = until
		b.backoffReason = compactRPCBusyReason(reason)
	}
	b.mu.Unlock()
}

func (b *nodeRPCBudget) backoffActive() bool {
	_, _, ok := b.backoffState()
	return ok
}

func (b *nodeRPCBudget) backoffRemaining() (time.Duration, bool) {
	until, _, ok := b.backoffState()
	if !ok {
		return 0, false
	}
	return time.Until(until), true
}

func (b *nodeRPCBudget) backoffState() (time.Time, string, bool) {
	if b == nil {
		return time.Time{}, "", false
	}
	now := time.Now()
	b.mu.Lock()
	until := b.backoffUntil
	reason := b.backoffReason
	b.mu.Unlock()
	if until.IsZero() || !until.After(now) {
		return time.Time{}, "", false
	}
	return until, reason, true
}

func isNodeRPCBusyError(err error) bool {
	if err == nil {
		return false
	}
	var backoff nodeRPCBackoffError
	if errors.As(err, &backoff) {
		return true
	}
	if isEVMSyncBackoffError(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "503 service unavailable") ||
		strings.Contains(message, "429 too many requests") ||
		strings.Contains(message, "server busy") ||
		strings.Contains(message, "too busy") ||
		strings.Contains(message, "temporarily unavailable") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "deadline exceeded") ||
		strings.Contains(message, "context deadline exceeded")
}

func compactRPCBusyReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "node RPC is busy"
	}
	if len(reason) > 240 {
		return reason[:240]
	}
	return reason
}
