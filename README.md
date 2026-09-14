<div align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/togo-mark-dark.svg" /><img src=".github/assets/togo-mark.svg" alt="ToGO" height="64" /></picture>
  <h1>togo-framework/plugin-host</h1>
  <p>
    <a href="https://to-go.dev/marketplace"><img src="https://img.shields.io/badge/marketplace-to--go.dev-1F8A99" alt="marketplace" /></a>
    <a href="https://pkg.go.dev/github.com/togo-framework/plugin-host"><img src="https://pkg.go.dev/badge/github.com/togo-framework/plugin-host.svg" alt="pkg.go.dev" /></a>
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT" />
  </p>
  <p><strong>Database-driven runtime plugin registry for <a href="https://to-go.dev">togo</a> — host, toggle & configure plugins live, and compose capabilities over the workflow plugin.</strong></p>
</div>

## Install

```bash
togo install togo-framework/plugin-host
```

Where a compiled togo plugin is wired at **build time**, `plugin-host` adds a **runtime** layer (the sentra-style *plugins table*): an admin can **enable/disable** a hosted plugin, change its **config**, and reorder **navigation** — live, persisted through a pluggable store, **no restart**. Each hosted plugin can contribute **capabilities** that call other togo plugins or trigger a **workflow** as step management.

## Concepts

- **Hosted plugin record** — `Plugin{Slug, Kind, Enabled, Config, Nav, Sort, LabelEN, LabelAR}` stored via the `Store` interface (in-memory default; back it with a DB `plugins` table).
- **Runtime toggle** — `Enable`/`Disable`/`Configure` + `Refresh()` apply state without recompiling.
- **Capabilities** — `RegisterCapability(slug, fn)`; `RunCapability(ctx, slug, input)` runs it, **gated** by the owning plugin's enabled state.
- **Workflow composition** — `WorkflowCapability(k, "name")` returns a capability that triggers a workflow (uses the [`workflow`](https://to-go.dev/plugins/workflow) plugin's steps).

## Usage

```go
ph, _ := pluginhost.FromKernel(k)

// Host a plugin (persisted), shown in nav, sorted.
ph.Register(pluginhost.Plugin{Slug: "newsletter", Kind: "capability", Enabled: true, Nav: true, Sort: 10, LabelEN: "Newsletter"})

// Toggle / configure at runtime — no restart.
ph.Disable("newsletter")
ph.Configure("newsletter", map[string]any{"from": "news@acme.io", "double_optin": true})
ph.Enable("newsletter")

// Contribute a capability that composes other plugins…
ph.RegisterCapability("newsletter.send", func(ctx context.Context, in map[string]any) (any, error) {
    // call mail / notifications / billing plugins here
    return nil, nil
})

// …or compose the workflow plugin as step management.
ph.RegisterCapability("onboard", pluginhost.WorkflowCapability(k, "onboarding"))
out, err := ph.RunCapability(ctx, "onboard", map[string]any{"user_id": 42})

nav := ph.Nav() // enabled + nav-visible plugins, in sort order (for a sidebar)
```

## REST API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/plugin-host/plugins` | list hosted plugins (sorted) |
| `GET` | `/api/plugin-host/dashboard` | plugins + nav + capabilities |
| `POST` | `/api/plugin-host/plugins/{slug}/toggle` | enable/disable |
| `POST` | `/api/plugin-host/plugins/{slug}/configure` | merge config (JSON body) |
| `GET` | `/api/plugin-host/capabilities` | list capability slugs |
| `POST` | `/api/plugin-host/capabilities/{slug}/run` | run a capability (JSON input) |

## Configuration

No required env. Storage defaults to a bounded in-memory store; implement the
`Store` interface (Upsert/Get/All/Delete) and call `ph.WithStore(db)` to host
plugins in a database `plugins` table for true runtime, multi-instance control.

---

<div align="center">
  <h3>Premium sponsors</h3>
  <p>
    <a href="https://id8media.com"><strong>ID8 Media</strong></a> &nbsp;·&nbsp;
    <a href="https://one-studio.co"><strong>One Studio</strong></a>
  </p>
  <p><sub>Support togo — <a href="https://github.com/sponsors/fadymondy">become a sponsor</a>.</sub></p>
</div>
