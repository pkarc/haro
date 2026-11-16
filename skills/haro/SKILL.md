---
name: haro
description: Reviews, explains and edits HAProxy configurations (haproxy.cfg) using the deterministic tools of the haro MCP server. Always use it when the user asks to review, understand, validate or modify a haproxy.cfg or a HAProxy conf.d folder.
---

# Working with HAProxy configurations using haro

Requires the haro MCP server (`claude mcp add haro -- haro mcp --root /etc/haproxy`).
Do not guess what a configuration does or what is wrong with it: ask haro.

## Workflow

1. **Understand**: `haro_explain` with `paths` (or `text`) to see what each section and directive does, including `defaults` inheritance. For a specific backend or frontend, use `section`.
2. **Review**: `haro_lint`. Always cite `file:line` and the rule (`[backend-missing]`) when discussing a problem. Order by severity: errors, warnings, suggestions.
3. **Propose changes**: use `haro_apply` without `write` to simulate. It returns the diff and the problems that would remain. Prefer structural operations (`rename_section` with `updateRefs`, `add_directive`…) over rewriting the whole file: they touch only what is needed.
   To publish a known service (Nextcloud, Proxmox VE, Home Assistant, Jellyfin, Gitea, Grafana, Vaultwarden, PostgreSQL, Redis, Elasticsearch, Kubernetes API), use `haro_template_list` to see the parameters and `haro_template_render` to simulate: it returns the diff, the problems it introduces and the `ops`, which are applied as-is with `haro_apply`. `haro_template_render` never writes; ask the user for the domain, the servers (`host:port`) and the certificate, and do not make up their values.
4. **Validate**: `haro_check` (`haproxy -c`) is the definitive validation; the `haro_lint` detector is advisory. If `haproxy` is not installed, say so.
5. **Write**: only if the user asks, with `haro_apply` and `write: true` (requires the server to have been launched with `--allow-write`). It is validated and a `.bak` copy is left. Show the diff first.

## HAProxy best practices to watch for

- Every `frontend`/`listen` needs `timeout client`; every `backend`/`listen`, `timeout server` and `timeout connect` (in `defaults` or in the section).
- Do not mix `mode http` and `mode tcp` between a frontend and its backend.
- `default_backend`/`use_backend` must point to backends that exist, also across `conf.d` files.
- In `bind ... ssl`, declare `alpn h2,http/1.1` if you want HTTP/2; do not use `verify none` on `server` without knowing why.
- Change the example passwords (`stats auth`, `userlist`) and do not expose the statistics page without restriction.
- With `haro_apply`, section indexes are those of the unified view of all files (the ones `haro_analyze` shows); query the indexes again after adding or deleting sections.
