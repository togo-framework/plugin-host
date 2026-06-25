// Package pluginhost is a database-driven runtime plugin/capability registry for
// togo (the sentra-style "plugins table"): host plugins over a data store,
// enable/disable/configure them at runtime without a restart, and let each
// hosted plugin contribute capabilities/steps that compose other togo plugins —
// including the workflow plugin as step management.
//
// Where a compiled togo plugin is wired at build time, plugin-host adds a
// *runtime* layer: an admin can toggle a plugin on/off, change its config, and
// reorder navigation live, all persisted through a pluggable Store.
package pluginhost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/togo-framework/togo"
	"github.com/togo-framework/workflow"
)

// Plugin is a hosted plugin/capability record (the row in the plugins table).
type Plugin struct {
	Slug    string         `json:"slug"`
	Kind    string         `json:"kind"`              // e.g. capability, source, integration, ui
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config,omitempty"`
	Nav     bool           `json:"nav"`               // show in navigation
	Sort    int            `json:"sort"`              // nav/order weight
	LabelEN string         `json:"label_en,omitempty"`
	LabelAR string         `json:"label_ar,omitempty"`
}

// Store persists hosted plugins (swap the in-memory default for a DB store).
type Store interface {
	Upsert(p Plugin)
	Get(slug string) (Plugin, bool)
	All() []Plugin
	Delete(slug string)
}

// CapabilityFunc is a runtime capability a hosted plugin contributes. It can
// call other togo plugins or trigger a workflow.
type CapabilityFunc func(ctx context.Context, input map[string]any) (any, error)

// Service is the plugin-host runtime stored on the kernel (k.Get("plugin-host")).
type Service struct {
	k       *togo.Kernel
	store   Store
	mu      sync.RWMutex
	caps    map[string]CapabilityFunc
	enabled map[string]bool // cache applied on Refresh
}

func init() {
	togo.RegisterProviderFunc("plugin-host", togo.PriorityService, func(k *togo.Kernel) error {
		s := &Service{k: k, store: newMemStore(), caps: map[string]CapabilityFunc{}, enabled: map[string]bool{}}
		k.Set("plugin-host", s)
		s.Refresh()
		if k.Router != nil {
			s.mountRoutes(k.Router)
		}
		return nil
	})
}

// FromKernel returns the plugin-host Service.
func FromKernel(k *togo.Kernel) (*Service, bool) {
	v, ok := k.Get("plugin-host")
	if !ok {
		return nil, false
	}
	s, ok := v.(*Service)
	return s, ok
}

// WithStore swaps the backing store (e.g. a DB-backed plugins table).
func (s *Service) WithStore(store Store) *Service {
	s.store = store
	s.Refresh()
	return s
}

// Register adds or updates a hosted plugin record, then refreshes runtime state.
func (s *Service) Register(p Plugin) {
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	s.store.Upsert(p)
	s.Refresh()
}

// Enable / Disable toggle a plugin at runtime (persisted + applied immediately).
func (s *Service) Enable(slug string) error  { return s.setEnabled(slug, true) }
func (s *Service) Disable(slug string) error { return s.setEnabled(slug, false) }

func (s *Service) setEnabled(slug string, on bool) error {
	p, ok := s.store.Get(slug)
	if !ok {
		return fmt.Errorf("plugin-host: no plugin %q", slug)
	}
	p.Enabled = on
	s.store.Upsert(p)
	s.Refresh()
	return nil
}

// Configure merges config into a plugin's record (typed values).
func (s *Service) Configure(slug string, cfg map[string]any) error {
	p, ok := s.store.Get(slug)
	if !ok {
		return fmt.Errorf("plugin-host: no plugin %q", slug)
	}
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	for k, v := range cfg {
		p.Config[k] = v
	}
	s.store.Upsert(p)
	s.Refresh()
	return nil
}

// ConfigValue returns a single config value for a plugin.
func (s *Service) ConfigValue(slug, key string) (any, bool) {
	p, ok := s.store.Get(slug)
	if !ok || p.Config == nil {
		return nil, false
	}
	v, ok := p.Config[key]
	return v, ok
}

// Refresh re-reads the store and applies the enabled-state cache. Call after a
// store mutation made outside this Service (the "no restart" reload).
func (s *Service) Refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = map[string]bool{}
	for _, p := range s.store.All() {
		s.enabled[p.Slug] = p.Enabled
	}
}

// IsEnabled reports whether a plugin is currently enabled.
func (s *Service) IsEnabled(slug string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled[slug]
}

// List returns hosted plugins sorted by Sort then Slug.
func (s *Service) List() []Plugin {
	ps := s.store.All()
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Sort != ps[j].Sort {
			return ps[i].Sort < ps[j].Sort
		}
		return ps[i].Slug < ps[j].Slug
	})
	return ps
}

// Get returns one hosted plugin.
func (s *Service) Get(slug string) (Plugin, bool) { return s.store.Get(slug) }

// Nav returns the enabled, nav-visible plugins in sort order (for a sidebar).
func (s *Service) Nav() []Plugin {
	var out []Plugin
	for _, p := range s.List() {
		if p.Enabled && p.Nav {
			out = append(out, p)
		}
	}
	return out
}

// --- capabilities / workflow composition ---

// RegisterCapability registers a runtime capability under a slug.
func (s *Service) RegisterCapability(slug string, fn CapabilityFunc) {
	s.mu.Lock()
	s.caps[slug] = fn
	s.mu.Unlock()
}

// Capabilities lists registered capability slugs.
func (s *Service) Capabilities() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.caps))
	for slug := range s.caps {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// RunCapability invokes a capability. It errors if the owning plugin is
// disabled, so toggling a plugin gates its capabilities at runtime.
func (s *Service) RunCapability(ctx context.Context, slug string, input map[string]any) (any, error) {
	s.mu.RLock()
	fn, ok := s.caps[slug]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugin-host: no capability %q", slug)
	}
	if _, hosted := s.store.Get(slug); hosted && !s.IsEnabled(slug) {
		return nil, fmt.Errorf("plugin-host: capability %q is disabled", slug)
	}
	return fn(ctx, input)
}

// WorkflowCapability builds a CapabilityFunc that triggers a named workflow via
// the workflow plugin — this is how a hosted plugin uses workflow as step
// management. Register it with RegisterCapability.
//
//	s.RegisterCapability("onboard", pluginhost.WorkflowCapability(k, "onboarding"))
func WorkflowCapability(k *togo.Kernel, workflowName string) CapabilityFunc {
	return func(ctx context.Context, input map[string]any) (any, error) {
		wf, ok := workflow.FromKernel(k)
		if !ok {
			return nil, fmt.Errorf("plugin-host: workflow plugin not installed")
		}
		return wf.Trigger(ctx, workflowName, input)
	}
}

// --- in-memory store ---

type memStore struct {
	mu sync.RWMutex
	m  map[string]Plugin
}

func newMemStore() *memStore { return &memStore{m: map[string]Plugin{}} }

func (s *memStore) Upsert(p Plugin) {
	s.mu.Lock()
	s.m[p.Slug] = p
	s.mu.Unlock()
}
func (s *memStore) Get(slug string) (Plugin, bool) {
	s.mu.RLock()
	p, ok := s.m[slug]
	s.mu.RUnlock()
	return p, ok
}
func (s *memStore) All() []Plugin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Plugin, 0, len(s.m))
	for _, p := range s.m {
		out = append(out, p)
	}
	return out
}
func (s *memStore) Delete(slug string) {
	s.mu.Lock()
	delete(s.m, slug)
	s.mu.Unlock()
}

// --- REST ---

func (s *Service) mountRoutes(r chi.Router) {
	r.Route("/api/plugin-host", func(r chi.Router) {
		r.Get("/plugins", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, 200, s.List())
		})
		r.Get("/dashboard", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, 200, map[string]any{"plugins": s.List(), "nav": s.Nav(), "capabilities": s.Capabilities()})
		})
		r.Post("/plugins/{slug}/toggle", func(w http.ResponseWriter, req *http.Request) {
			slug := chi.URLParam(req, "slug")
			err := s.Enable(slug)
			if p, ok := s.Get(slug); ok && p.Enabled {
				err = s.Disable(slug)
			}
			writeErr(w, err, map[string]any{"slug": slug, "enabled": s.IsEnabled(slug)})
		})
		r.Post("/plugins/{slug}/configure", func(w http.ResponseWriter, req *http.Request) {
			var cfg map[string]any
			_ = json.NewDecoder(req.Body).Decode(&cfg)
			writeErr(w, s.Configure(chi.URLParam(req, "slug"), cfg), map[string]string{"ok": "configured"})
		})
		r.Get("/capabilities", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, 200, s.Capabilities())
		})
		r.Post("/capabilities/{slug}/run", func(w http.ResponseWriter, req *http.Request) {
			var input map[string]any
			_ = json.NewDecoder(req.Body).Decode(&input)
			out, err := s.RunCapability(req.Context(), chi.URLParam(req, "slug"), input)
			writeErr(w, err, out)
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error, ok any) {
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, ok)
}
