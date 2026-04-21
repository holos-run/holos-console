/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/rest"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// TestNewManager_WiresControllerRuntimeLogger verifies that constructing a
// Manager also wires controller-runtime's process-global logger. Regression
// guard for HOL-765: without the SetLogger call inside NewManager,
// controller-runtime internals (priorityqueue, cache, leader election) hit
// the "[controller-runtime] log.SetLogger(...) was never called" code path
// and dump a stack trace on first use.
func TestNewManager_WiresControllerRuntimeLogger(t *testing.T) {
	buf := &syncBuffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	_, err := NewManager(&rest.Config{Host: "http://127.0.0.1:0"}, Options{Logger: logger})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Log through controller-runtime's global Logger. If SetLogger was
	// called correctly, the record flows through our slog handler into buf.
	ctrllog.Log.WithName("test").Info("hol-765-logger-check", "ok", true)

	if out := buf.String(); !strings.Contains(out, "hol-765-logger-check") {
		t.Fatalf("controller-runtime log did not reach slog handler; buf=%q", out)
	}
}

// syncBuffer is a goroutine-safe bytes.Buffer so slog writes from
// controller-runtime's async log sinks don't race with the test goroutine's
// reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
