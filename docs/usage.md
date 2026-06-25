# plugin-host — usage

## The plugins model
A hosted plugin is `Plugin{Slug, Kind, Enabled, Config, Nav, Sort, LabelEN, LabelAR}`
persisted via the `Store` interface. The default store is in-memory; back it with
a DB `plugins` table via `ph.WithStore(db)` for runtime, multi-instance control.

## Register & toggle at runtime
```go
ph, _ := pluginhost.FromKernel(k)
ph.Register(pluginhost.Plugin{Slug:"newsletter", Kind:"capability", Enabled:true, Nav:true, Sort:10})
ph.Disable("newsletter")                 // live, no restart
ph.Configure("newsletter", map[string]any{"from":"news@acme.io"})
ph.Enable("newsletter")
ph.Refresh()                             // re-apply state written directly to the store
```

## Capabilities (compose other plugins)
```go
ph.RegisterCapability("newsletter.send", func(ctx context.Context, in map[string]any) (any, error) {
    // call mail/notifications/billing plugins
    return nil, nil
})
out, err := ph.RunCapability(ctx, "newsletter.send", input) // gated by the plugin's enabled state
```

## Workflow as step management
```go
// uses the workflow plugin: triggers the "onboarding" workflow
ph.RegisterCapability("onboard", pluginhost.WorkflowCapability(k, "onboarding"))
ph.RunCapability(ctx, "onboard", map[string]any{"user_id": 42})
```

## Navigation
`ph.Nav()` returns enabled + nav-visible plugins in `Sort` order — feed it to a
sidebar. `ph.List()` returns all hosted plugins.

## REST
`GET /api/plugin-host/plugins`, `/dashboard`, `POST /plugins/{slug}/toggle`,
`/plugins/{slug}/configure`, `GET /capabilities`, `POST /capabilities/{slug}/run`.

## Custom (DB) store
Implement `Store` (Upsert/Get/All/Delete) over a `plugins` table and install with
`ph.WithStore(myStore)`; `Refresh()` reloads runtime state from it.
