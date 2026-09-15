package core

import (
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
)

// Subscribe used to dereference an observer that nothing ever assigned, so the
// first caller panicked. clash api's GET /logs is that caller, reached whenever
// an operator enables experimental.clash_api and opens the dashboard's log tab.
func TestSubscribeWithoutObserverReportsInsteadOfPanicking(t *testing.T) {
	factory := NewDefaultFactory(t.Context(), log.Formatter{}, nil, "", false)
	subscription, _, err := factory.Subscribe()
	if err == nil {
		t.Fatal("Subscribe reported success on a factory that has nothing to subscribe to")
	}
	if subscription != nil {
		t.Error("a failed Subscribe must not hand back a subscription")
	}
	// The unsubscribe half is reached through the handler's defer, so it has to
	// survive the same state.
	factory.UnSubscribe(subscription)
}

// Fixing the panic by always refusing would leave the log tab permanently
// empty, which is the same feature broken a quieter way: an observable factory
// has to actually deliver what it logs.
func TestObservableFactoryDeliversEntries(t *testing.T) {
	factory := NewDefaultFactory(t.Context(), log.Formatter{}, nil, "", true)
	if err := factory.Start(); err != nil {
		t.Fatalf("start factory: %v", err)
	}
	t.Cleanup(func() { _ = factory.Close() })

	subscription, _, err := factory.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer factory.UnSubscribe(subscription)

	factory.NewLogger("test-tag").Info("hello")

	select {
	case entry := <-subscription:
		if entry.Level != log.LevelInfo {
			t.Errorf("level = %v, want info", entry.Level)
		}
		if entry.Message == "" {
			t.Error("entry carries no message")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("logged an entry but the subscription never received it")
	}
}
