package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/log"
)

// recordingLogger keeps whatever Error was given. The rest of the interface
// comes from the embedded nil value: closeGuarded only reaches Error and the
// Trace adapter.LogElapsed emits, and both are overridden here.
type recordingLogger struct {
	log.ContextLogger
	errors []string
}

func (l *recordingLogger) Error(args ...any) {
	l.errors = append(l.errors, fmt.Sprint(args...))
}

func (l *recordingLogger) ErrorContext(_ context.Context, args ...any) { l.Error(args...) }
func (l *recordingLogger) Trace(...any)                                {}
func (l *recordingLogger) TraceContext(context.Context, ...any)        {}

// A panic while closing a component must not escape: the panel tears the box
// down and rebuilds it in process, and on the paths that restart the core off a
// goroutine (cronjob/resetTrafficJob, service/tgbot) there is no recover above
// us. Recovering also means the same panic returns on every restart, so the
// stack has to reach the log -- "invalid memory address" repeated every few
// seconds names nothing anyone can act on.
func TestCloseGuardedReportsPanicWithStack(t *testing.T) {
	logger := &recordingLogger{}
	box := &Box{logger: logger}

	err := box.closeGuarded("boom", func() error { panic("kaboom") })

	if err == nil {
		t.Fatal("a panic must come back as an error, not escape")
	}
	if !strings.Contains(err.Error(), "kaboom") {
		t.Errorf("the panic value must reach the caller, got %v", err)
	}
	if len(logger.errors) != 1 {
		t.Fatalf("expected one logged error, got %v", logger.errors)
	}
	logged := logger.errors[0]
	if !strings.Contains(logged, "boom") || !strings.Contains(logged, "kaboom") {
		t.Errorf("the log must name the component and the panic, got %q", logged)
	}
	if !strings.Contains(logged, "goroutine ") || !strings.Contains(logged, "closeGuarded") {
		t.Errorf("the log must carry the stack, got %q", logged)
	}
}

// The ordinary path stays ordinary: no panic, the error comes straight back.
func TestCloseGuardedPassesErrorsThrough(t *testing.T) {
	logger := &recordingLogger{}
	box := &Box{logger: logger}

	want := fmt.Errorf("listener already closed")
	if err := box.closeGuarded("inbound", func() error { return want }); err != want {
		t.Errorf("close error must come back unchanged, got %v", err)
	}
	if len(logger.errors) != 0 {
		t.Errorf("a plain error is the caller's to report, got %v", logger.errors)
	}
	if err := box.closeGuarded("inbound", func() error { return nil }); err != nil {
		t.Errorf("a clean close must report nothing, got %v", err)
	}
}
