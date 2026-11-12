package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPIClientReturnsStalePayloadWhileRefreshing(t *testing.T) {
	var calls atomic.Int32
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			fmt.Fprint(w, `{"value":"one"}`)
		case 2:
			close(refreshStarted)
			<-releaseRefresh
			fmt.Fprint(w, `{"value":"two"}`)
		default:
			fmt.Fprint(w, `{"value":"two"}`)
		}
	}))
	defer server.Close()

	client := newAPIClient(server.URL, server.Client())
	payload, err := client.get(context.Background(), "/data", time.Millisecond)
	if err != nil {
		t.Fatalf("initial get failed: %v", err)
	}
	if got := stringValue(payload, "value"); got != "one" {
		t.Fatalf("initial payload value = %q, want one", got)
	}
	time.Sleep(5 * time.Millisecond)

	started := time.Now()
	payload, err = client.get(context.Background(), "/data", time.Millisecond)
	if err != nil {
		t.Fatalf("stale get failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("stale get blocked for %s", elapsed)
	}
	if got := stringValue(payload, "value"); got != "one" {
		t.Fatalf("stale payload value = %q, want one", got)
	}

	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
	close(releaseRefresh)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		payload, err = client.get(context.Background(), "/data", time.Second)
		if err != nil {
			t.Fatalf("refresh get failed: %v", err)
		}
		if got := stringValue(payload, "value"); got == "two" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("refreshed payload did not replace stale payload")
}

func TestAPIClientSingleflightsColdMiss(t *testing.T) {
	var calls atomic.Int32
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(requestStarted)
		}
		<-releaseRequest
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"value":"shared"}`)
	}))
	defer server.Close()

	client := newAPIClient(server.URL, server.Client())
	const requestCount = 8
	var wg sync.WaitGroup
	errs := make(chan error, requestCount)
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload, err := client.get(context.Background(), "/shared", time.Minute)
			if err != nil {
				errs <- err
				return
			}
			if got := stringValue(payload, "value"); got != "shared" {
				errs <- fmt.Errorf("value = %q, want shared", got)
			}
		}()
	}

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("collector requests before release = %d, want 1", got)
	}
	close(releaseRequest)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("collector requests = %d, want 1", got)
	}
}
