package pluginhost

import (
	"context"
	"errors"
	"testing"
)

func newTestService() *Service {
	return &Service{store: newMemStore(), caps: map[string]CapabilityFunc{}, enabled: map[string]bool{}}
}

func TestRegisterAndListOrdering(t *testing.T) {
	s := newTestService()
	s.Register(Plugin{Slug: "b", Sort: 2, Kind: "ui"})
	s.Register(Plugin{Slug: "a", Sort: 1, Kind: "ui"})
	s.Register(Plugin{Slug: "c", Sort: 1, Kind: "ui"})
	got := s.List()
	if len(got) != 3 {
		t.Fatalf("List = %d, want 3", len(got))
	}
	// Sort asc, then slug asc: a(1), c(1), b(2)
	if got[0].Slug != "a" || got[1].Slug != "c" || got[2].Slug != "b" {
		t.Fatalf("order = %s,%s,%s", got[0].Slug, got[1].Slug, got[2].Slug)
	}
}

func TestEnableDisableRefresh(t *testing.T) {
	s := newTestService()
	s.Register(Plugin{Slug: "search", Enabled: false})
	if s.IsEnabled("search") {
		t.Fatal("should start disabled")
	}
	if err := s.Enable("search"); err != nil {
		t.Fatal(err)
	}
	if !s.IsEnabled("search") {
		t.Fatal("Enable did not take effect after Refresh")
	}
	if err := s.Disable("search"); err != nil {
		t.Fatal(err)
	}
	if s.IsEnabled("search") {
		t.Fatal("Disable did not take effect")
	}
	if err := s.Enable("missing"); err == nil {
		t.Fatal("enabling an unknown plugin should error")
	}
	// Refresh applies state written directly to the store (no restart).
	s.store.Upsert(Plugin{Slug: "search", Enabled: true})
	s.Refresh()
	if !s.IsEnabled("search") {
		t.Fatal("Refresh did not re-apply enabled state from the store")
	}
}

func TestConfigure(t *testing.T) {
	s := newTestService()
	s.Register(Plugin{Slug: "mailer"})
	if err := s.Configure("mailer", map[string]any{"from": "no-reply@x.io", "retries": 3}); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.ConfigValue("mailer", "from"); !ok || v != "no-reply@x.io" {
		t.Fatalf("from = %v ok=%v", v, ok)
	}
	// Merge, not replace.
	if err := s.Configure("mailer", map[string]any{"retries": 5}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.ConfigValue("mailer", "from"); v != "no-reply@x.io" {
		t.Fatal("merge dropped an existing key")
	}
	if v, _ := s.ConfigValue("mailer", "retries"); v != 5 {
		t.Fatalf("retries = %v, want 5", v)
	}
	if err := s.Configure("nope", nil); err == nil {
		t.Fatal("configuring unknown plugin should error")
	}
}

func TestCapabilityRunAndGating(t *testing.T) {
	s := newTestService()
	called := false
	s.RegisterCapability("greet", func(ctx context.Context, in map[string]any) (any, error) {
		called = true
		return "hello " + in["name"].(string), nil
	})
	if caps := s.Capabilities(); len(caps) != 1 || caps[0] != "greet" {
		t.Fatalf("capabilities = %v", caps)
	}
	out, err := s.RunCapability(context.Background(), "greet", map[string]any{"name": "sam"})
	if err != nil || !called || out != "hello sam" {
		t.Fatalf("run = %v err=%v called=%v", out, err, called)
	}
	if _, err := s.RunCapability(context.Background(), "missing", nil); err == nil {
		t.Fatal("unknown capability should error")
	}

	// A capability owned by a hosted-but-disabled plugin is gated.
	s.RegisterCapability("import", func(ctx context.Context, in map[string]any) (any, error) { return "ran", nil })
	s.Register(Plugin{Slug: "import", Enabled: false})
	if _, err := s.RunCapability(context.Background(), "import", nil); err == nil {
		t.Fatal("disabled plugin's capability should be gated")
	}
	if err := s.Enable("import"); err != nil {
		t.Fatal(err)
	}
	if out, err := s.RunCapability(context.Background(), "import", nil); err != nil || out != "ran" {
		t.Fatalf("enabled capability run = %v err=%v", out, err)
	}
}

func TestCapabilityErrorPropagates(t *testing.T) {
	s := newTestService()
	s.RegisterCapability("boom", func(ctx context.Context, in map[string]any) (any, error) {
		return nil, errors.New("kaboom")
	})
	if _, err := s.RunCapability(context.Background(), "boom", nil); err == nil {
		t.Fatal("capability error should propagate")
	}
}

func TestNavOrdering(t *testing.T) {
	s := newTestService()
	s.Register(Plugin{Slug: "dash", Enabled: true, Nav: true, Sort: 1})
	s.Register(Plugin{Slug: "hidden", Enabled: true, Nav: false, Sort: 2})
	s.Register(Plugin{Slug: "off", Enabled: false, Nav: true, Sort: 0})
	s.Register(Plugin{Slug: "reports", Enabled: true, Nav: true, Sort: 3})
	nav := s.Nav()
	if len(nav) != 2 {
		t.Fatalf("nav = %d items, want 2 (enabled+nav only)", len(nav))
	}
	if nav[0].Slug != "dash" || nav[1].Slug != "reports" {
		t.Fatalf("nav order = %s,%s", nav[0].Slug, nav[1].Slug)
	}
}
