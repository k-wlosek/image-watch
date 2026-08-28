package notify

import (
	"context"
	"errors"
	"testing"
)

func TestBuild_UnknownTypeReturnsError(t *testing.T) {
	_, err := Build("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestRegister_And_Build(t *testing.T) {
	called := false
	Register("test_dummy", func(params map[string]string) (Notifier, error) {
		called = true
		return dummyNotifier{}, nil
	})

	n, err := Build("test_dummy", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if !called {
		t.Error("expected builder to be called")
	}
	ctx := context.Background()
	if err := n.Notify(ctx, Notification{}); err != nil {
		t.Fatalf("Notify error: %v", err)
	}
}

func TestRegister_OverwritesPrevious(t *testing.T) {
	Register("overwrite_test", func(params map[string]string) (Notifier, error) {
		return dummyNotifier{tag: "first"}, nil
	})
	Register("overwrite_test", func(params map[string]string) (Notifier, error) {
		return dummyNotifier{tag: "second"}, nil
	})

	n, err := Build("overwrite_test", nil)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	d := n.(dummyNotifier)
	if d.tag != "second" {
		t.Errorf("expected second registration to win, got tag %q", d.tag)
	}
}

func TestRegister_BuilderErrorReturnsError(t *testing.T) {
	Register("error_test", func(params map[string]string) (Notifier, error) {
		return nil, errors.New("boom")
	})

	_, err := Build("error_test", nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected builder error, got %v", err)
	}
}

type dummyNotifier struct{ tag string }

func (d dummyNotifier) Notify(context.Context, Notification) error { return nil }
