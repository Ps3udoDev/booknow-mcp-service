# booknow-mcp-service

Servidor dedicado en Go para BookNow: **MCP (Streamable HTTP)**, **webhooks/notificaciones Twilio WhatsApp** y API REST de negocio, sobre **Supabase** (Postgres + Auth) y desplegado en **Cloud Run**. Reemplaza gradualmente las rutas `/api/mcp`, `/api/actions/*` y `/api/webhooks/twilio` de la app Next.js en Vercel.

## Material de referencia (local, no versionado)

`migracion/` está en `.gitignore` y es **solo lectura** (un hook bloquea su edición). Consúltalo antes de implementar cada pieza:

- `migracion/README.md` — catálogo de las 7 tools MCP, flujo OAuth, reglas PII, env vars.
- `migracion/docs/guia-go-cloud-run-supabase.md` — cómo conectar Go con Supabase (pgx, pooler, JWT, RLS).
- `migracion/docs/guia-migracion-next-scp-go-cloud-run.md` — layout, capas, webhooks, MCP, Cloud Run, fases.
- `migracion/docs/comparativa-go-http.md` — por qué `net/http` + chi.
- `migracion/docs/seguridad-backend-go-cloud-run.md` — checklist de seguridad.
- `migracion/specs/*.md` — diseño y contrato de implementación del MCP (fuente de verdad del comportamiento).
- `migracion/mcp/**/*.ts`, `migracion/twilio-whatsapp/*.ts` — implementación TS actual a portar.
- `migracion/mcp/database/*.sql` — esquema `mcp_connections`, `mcp_appointment_drafts`, `mcp_tool_calls`, RPC `confirm_appointment_draft`.

## Stack y decisiones

| Área | Decisión |
|---|---|
| HTTP | `net/http` + `github.com/go-chi/chi/v5`; handlers estándar `(w, r)` |
| MCP | SDK oficial `github.com/modelcontextprotocol/go-sdk` (Streamable HTTP), endpoint único `/mcp` |
| DB | `github.com/jackc/pgx/v5` (`pgxpool`) contra Postgres de Supabase; pool pequeño (`max_instances × max_conns`) |
| Auth | Supabase Auth como IdP; Go valida JWT ES256 vía JWKS (firma, `iss`, `aud`, `exp`, `nbf`, alg fijo) |
| Logs | `log/slog` JSON a stdout (Cloud Logging) |
| Deploy | Contenedor distroless nonroot en Cloud Run; secretos en Secret Manager |

Añade dependencias solo cuando el código las importe (`go mod tidy` elimina las no usadas).

## Layout

```
cmd/api-server/        main: config, logger, router, http.Server, shutdown SIGTERM
internal/config/       carga y validación de env vars
internal/httpapi/      router chi, middleware, handlers (solo transporte)
```

Paquetes previstos (créalos cuando haya código real, no antes):
`internal/auth` (JWT Supabase), `internal/tenant` (resolución de contexto/roles), `internal/mcp` (server + tools),
`internal/application/<dominio>` (casos de uso: appointments, customers, analytics, drafts),
`internal/repository/postgres`, `internal/integration/{twilio,resend}`, `internal/platform/{audit,pii}`.

## Comandos

```bash
go run ./cmd/api-server          # servidor local en :8080 (GET /healthz)
go test -race ./...              # tests (race necesita CGO; en Windows sin gcc usa: go test ./...)
golangci-lint run                # lint (config en .golangci.yml)
golangci-lint fmt                # formatea con gofumpt + goimports
go vet ./...
govulncheck ./...
docker build -t booknow-mcp .    # imagen de producción
```

Antes de dar una tarea por terminada: `golangci-lint run` y `go test ./...` deben pasar.

## Reglas no negociables (heredadas del MCP en producción)

- **Multitenancy**: `tenant_id` se resuelve SOLO desde el token + `mcp_connections` activa. Nunca como parámetro de una tool ni desde el body.
- **Roles**: solo `owner|admin|manager` en `tenant_users`; revocación/degradación corta el acceso inmediatamente.
- **PII**: teléfonos siempre enmascarados (`+58******123`); nunca notas privadas, direcciones ni facturación hacia el LLM.
- **Escrituras en dos pasos**: `create_appointment_draft` (TTL 10 min, `human_summary`) → `confirm_appointment_draft` (RPC transaccional con `FOR UPDATE`). Ambas con `idempotency_key`.
- **Auditoría**: cada tool call a `mcp_tool_calls` con parámetros sanitizados, riesgo, duración y error.
- **SQL**: siempre parametrizado; nunca concatenar input.
- **Webhooks Twilio**: verificar `X-Twilio-Signature` sobre el body original ANTES de parsear; deduplicar; responder rápido.
- **MCP**: validar `Origin` (403 si inválido), negociar `Accept`, no aplicar timeouts cortos de REST al stream SSE.
- **Secretos**: nunca en código, logs, imagen ni `.env` versionado. No loguear `Authorization`, tokens ni connection strings.
- **Service role key** de Supabase: nunca exponerla ni usarla como atajo para saltar autorización en Go.

## Convenciones

- Errores envueltos con `%w` y contexto; se manejan una sola vez (loguear O devolver).
- `context.Context` como primer parámetro en todo lo que haga I/O; nunca guardarlo en structs.
- Interfaces pequeñas definidas en el paquete consumidor, solo si hay sustitución real (tests o 2 implementaciones).
- Tests table-driven con `t.Parallel()`; handlers con `httptest`.
- Comentarios de código y identificadores en inglés; documentación en español.
