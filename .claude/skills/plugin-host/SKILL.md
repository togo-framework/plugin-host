---
name: plugin-host
description: Host togo plugins over a database at runtime — register/enable/disable/configure plugins live (sentra-style plugins table), and compose capabilities that call other plugins or trigger workflows as steps.
---

# togo plugin-host

Use this skill to add a database-driven runtime plugin registry to a togo app.

## Host & toggle plugins (no restart)
```go
ph, _ := pluginhost.FromKernel(k)
ph.Register(pluginhost.Plugin{Slug:"newsletter", Kind:"capability", Enabled:true, Nav:true, Sort:10})
ph.Disable("newsletter"); ph.Enable("newsletter")
ph.Configure("newsletter", map[string]any{"from":"news@acme.io"})
ph.Refresh() // re-apply state written directly to the store
```

## Capabilities + workflow steps
```go
ph.RegisterCapability("newsletter.send", func(ctx, in) (any,error){ /* call other plugins */ return nil,nil })
ph.RegisterCapability("onboard", pluginhost.WorkflowCapability(k, "onboarding")) // composes the workflow plugin
out, _ := ph.RunCapability(ctx, "onboard", map[string]any{"user_id":42})         // gated by enabled state
```

## Admin REST
`GET /api/plugin-host/plugins|dashboard|capabilities`, `POST /plugins/{slug}/toggle|configure`, `POST /capabilities/{slug}/run`.

## Notes
- Default store is in-memory; implement the `Store` interface + `ph.WithStore(db)` to host plugins in a DB `plugins` table (real runtime, multi-instance).
- Capabilities are **gated**: a disabled hosted plugin's capability refuses to run.
- Use `ph.Nav()` (enabled + nav, sorted) to build a sidebar.
