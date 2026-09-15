# Roadmap — booknow-mcp-service

Checklist vivo de la migración del MCP, los webhooks de Twilio y la API de BookNow desde Next.js/Vercel a Go en Cloud Run.
Márcalo en el mismo commit que completa cada tarea.

**Leyenda:** `[x]` hecho y verificado · `[ ]` pendiente · 🔒 requiere acción manual del usuario (Supabase, Vercel, GCP, Twilio) · ⚠️ decisión abierta

**Fuentes:** `migracion/specs/*` (comportamiento), `migracion/docs/guia-migracion-next-scp-go-cloud-run.md` (fases y checklist de producción), `migracion/docs/seguridad-backend-go-cloud-run.md`, `CLAUDE.md` (reglas no negociables).

> La numeración sigue la guía de migración, pero el orden se ajustó a la prioridad del proyecto: primero MCP (Fases 4–6), luego Twilio (7) y el fallback REST (8). La guía proponía REST → webhooks → MCP.

## Resumen

| Fase | Estado |
|---|---|
| 1. Inventario y contrato | 🟡 parcial |
| 2. Esqueleto Go | ✅ hecho (falta despliegue a staging, ver Fase 9) |
| 3. Base de datos, autenticación y tenant | ✅ hecho y validado contra producción |
| 4. Endpoint `/mcp` y seguridad de transporte | ✅ hecho y validado contra producción (solo falta `last_used_at`, que requiere permiso) |
| 5. Plataforma transversal y tools de lectura | ⏭️ **siguiente** (permisos ya aplicados en producción) |
| 6. Escrituras en dos pasos (drafts) | ⬜ pendiente |
| 7. Twilio WhatsApp (webhook y notificaciones) | ⬜ pendiente |
| 8. Fallback REST `/api/actions/*` | ⬜ pendiente |
| 9. Despliegue en Cloud Run | ⬜ pendiente |
| 10. QA integral, corte y retirada de Next.js | ⬜ pendiente |

---

## Fase 1 — Inventario y contrato

- [x] Catálogo de las 7 tools MCP, scopes y reglas de PII (`migracion/README.md`).
- [x] Spec de diseño y contrato de implementación del MCP (`migracion/specs/`).
- [x] Esquema MCP presente en el snapshot local: `mcp_connections`, `mcp_appointment_drafts`, `mcp_tool_calls`, RPC `confirm_mcp_appointment_draft`.
- [ ] Capturar ejemplos reales de request/response de `/api/mcp`, `/api/actions/*` y del webhook Twilio (sin PII) para usarlos como tests de contrato.
- [ ] ⚠️ Definir la URL pública final del servicio (p. ej. `mcp.booknow.app`) y si `/api/actions/*` se mantiene o se retira.
- [x] Confirmar los cambios de comportamiento de autorización frente al TS (D1–D3).

## Fase 2 — Esqueleto Go

- [x] `go.mod`, `cmd/api-server` con timeouts de `http.Server` y apagado ordenado con `SIGTERM`. `1401ad7`
- [x] `internal/config` con validación de `PORT`, `APP_ENV` y `LOG_LEVEL`. `1401ad7`
- [x] Router chi con `RequestID`, `Recoverer` y logger `slog` JSON que no registra headers ni bodies. `1401ad7`
- [x] `GET /healthz`. `1401ad7`
- [x] Dockerfile distroless nonroot. `1401ad7`
- [x] CI: golangci-lint, `go vet`, `go test -race`, `go mod verify`, `govulncheck`. `1401ad7`
- [x] Supabase local y snapshot de solo lectura del esquema remoto. `d7ca918`
- [ ] Despliegue privado a staging para validar el ciclo build → deploy → observe (movido a Fase 9; conviene adelantarlo antes de exponer `/mcp`).

## Fase 3 — Base de datos, autenticación y tenant

### 3.1 Postgres — `a92dc27`
- [x] `DATABASE_URL` obligatoria y `DB_MAX_CONNS` (1–50, default 5) sin filtrar credenciales en errores.
- [x] `internal/repository/postgres.NewPool`: límites del pool, `application_name` y ping al arrancar (timeout 10s).
- [x] `GET /readyz` con ping de 2s; 503 sin detalles.
- [x] Test de integración contra la base local.
- [x] Actualización de `golang.org/x/text` a v0.42.0 (GO-2026-5970).

### 3.2 Autenticación JWT — `689e544`
- [x] `SUPABASE_URL` (solo origen; https fuera de development) y `SUPABASE_JWT_AUDIENCE` (default `authenticated`).
- [x] `internal/auth.Verifier`: ES256 fijo, `kid` obligatorio, `iss`, `aud`, `exp` obligatorio, `nbf`, `iat`, margen de reloj de 30s.
- [x] Reglas Supabase: `sub` UUID, `role = authenticated`, sin usuarios anónimos; expone `client_id`.
- [x] JWKS con fallo al arrancar, timeout de 5s y refresco limitado ante `kid` desconocido.
- [x] `BearerToken(r)`.
- [x] Tests: 21 tokens inválidos, rotación, RS256 bien firmado rechazado, token real de Supabase local y 10 mutaciones detectadas.

### 3.3 Autorización de tenant — `a407c8b`
- [x] `internal/tenant.Resolver`: conexión exacta `(auth_user_id, oauth_client_id)`, tenant `active`, membresía activa, rol `owner|admin|manager` y módulo `business-mcp`.
- [x] Errores estables que envuelven `ErrAccessDenied` (403); los fallos de base no se confunden con denegaciones.
- [x] Sin caché: revocación y degradación efectivas en la siguiente petición (test de integración).
- [x] `postgres.MCPAccessStore` con `DBTX` (pool o tx) y 10 casos de integración; 11 mutaciones detectadas.
- [x] 🔒 Rol de base de datos `booknow_mcp_service` en producción: `login`, `bypassrls`, `noinherit`, solo `SELECT` sobre `mcp_connections`, `tenants`, `tenant_users`, `tenant_modules` y `modules`. SQL validado antes en local (lee pese al RLS, sin escritura, sin acceso a `customers`).
- [x] Session Pooler validado: `aws-0-us-west-2.pooler.supabase.com:5432`, usuario `booknow_mcp_service.<ref>`, Postgres 17.6, consultas con prepared statements OK.
- [x] Smoke test remoto de solo lectura (`TestRemoteSmoke`, variables `SMOKE_*` en `.env.smoke`): token OAuth real (Inspector) → JWKS de producción → pooler → `elvis-studio` / `admin`; sin `client_id` → rechazado.
- [x] D1, D2 y D3 confirmados por el usuario.
- [x] 🔒 Migración del rol en el repo Next.js (`20260915160346_booknow_mcp_service_role.sql`, commit `2b8c7b7` en book-now-hub), aplicada en producción con `db push` y verificada en solo lectura desde aquí. Instrucciones usadas: `docs/handoff/nextjs-migracion-rol-booknow-mcp-service.md`.
- [x] Historial de migraciones remoto reparado: `20260910120000_booknow_business_mcp` estaba aplicada a mano pero no registrada. Antes de `migration repair --status applied` se verificó que el esquema y los datos de producción coincidían con la migración.
- [x] Snapshot local regenerado con los `GRANT` del rol y `supabase/roles.sql` para que `supabase db reset` cree el rol en local (verificado).
- [ ] 🔒 Repo Next.js sin migración baseline de las tablas originales (`supabase db reset` no funciona allí): [book-now-hub#3](https://github.com/Ps3udoDev/book-now-hub/issues/3).

## Fase 4 — Endpoint `/mcp` y seguridad de transporte

- [x] SDK oficial `github.com/modelcontextprotocol/go-sdk` v1.8.0: Streamable HTTP en un único `/mcp`, **stateless** (cualquier instancia de Cloud Run atiende cualquier request) y con **respuestas JSON** (sin stream SSE abierto).
- [x] Servidor MCP construido **por request** con el `tenant.Access` resuelto (`internal/mcpserver`), como el TS: una tool no puede ver el acceso de otra request (test con dos tokens alternados).
- [x] `auth.Verifier`, `tenant.Resolver` y `MCPAccessStore` construidos en `main.go`; si no se puede cargar el JWKS, el arranque falla.
- [x] Middleware MCP propio (el `RequireBearerToken` del SDK devuelve el texto del error y no distingue 403):
  - [x] Sin token o cabecera mal formada → 401 `WWW-Authenticate: Bearer resource_metadata="…"`.
  - [x] Token inválido → 401 `Bearer error="invalid_token", resource_metadata="…"`, sin detalles del verificador.
  - [x] `ErrAccessDenied` → 403 JSON-RPC `-32002` con `errorCode` estable (`MCP_CLIENT_REQUIRED`, `MCP_CONNECTION_NOT_FOUND`, `MCP_CONNECTION_REVOKED`, `TENANT_INACTIVE`, `MEMBER_INACTIVE`, `ROLE_FORBIDDEN`, `MODULE_DISABLED`).
  - [x] Error de base → 500 `-32603` sin detalles (el error va al log con `request_id`).
  - [x] `tenant.Access` viaja en el `context` de la request; las tools no tienen parámetro de tenant (test).
- [x] `/.well-known/oauth-protected-resource/mcp` y `/.well-known/oauth-protected-resource` (RFC 9728) con `resource = MCP_PUBLIC_URL/mcp` y el issuer de Supabase.
- [x] Validación de `Origin` antes de autenticar: sin `Origin` pasa (clientes nativos); fuera de `MCP_ALLOWED_ORIGINS` (incluido `null`) → 403; permitido → CORS con eco del origen, `Vary: Origin` y exposición de `WWW-Authenticate`. Preflight 204/403.
- [x] Reglas de transporte del SDK verificadas: `Accept` incompatible → 400, `Content-Type` incorrecto → 415, notificación → 202, GET/DELETE → 405.
- [x] Timeouts: al no haber SSE, el `WriteTimeout` de 60s no corta streams (decisión documentada en `main.go`).
- [x] Límite de body de 1 MiB → 413.
- [x] Tool `health` (solo lectura): estado, slug del negocio y rol; sin IDs ni datos de negocio.
- [x] Config validada: `MCP_PUBLIC_URL` (origen, https fuera de development) y `MCP_ALLOWED_ORIGINS` (lista de orígenes, sin `*`).
- [x] Tests `httptest` + cliente real del SDK; 11 mutaciones de reglas de seguridad detectadas.
- [x] Binario real contra Supabase local: metadata, 401 con challenge, Origin ajeno 403 y token real sin `client_id` → 403 `MCP_CLIENT_REQUIRED`; logs sin tokens.
- [x] 🔒 Smoke end-to-end con token OAuth real del Inspector: binario Go en local → JWKS y Session Pooler de producción (rol `booknow_mcp_service`) → `initialize` 200, `tools/list` = `[health]`, `tools/call health` = `elvis-studio` / `admin`. Logs sin tokens ni cadenas de conexión.
- [x] 🔒 `GRANT UPDATE (last_used_at)` aplicado en producción con la migración de la Fase 5; la implementación en Go se hace en la Fase 5.
- [ ] Cancelación: con respuestas JSON sin stream no aplica; revisar si una tool larga necesita `PropagateRequestCancellation` en la Fase 5.

## Fase 5 — Plataforma transversal y tools de lectura

### Prerrequisito: migración de permisos (repo Next.js)
- [x] SQL preparado y validado en local (idempotente; 9/9 permisos esperados, 0/19 prohibidos; purga en 5 escenarios; accesos denegados como el rol) y columnas comprobadas en producción (80/80): `docs/handoff/sql/booknow_mcp_service_phase5_permissions.sql` (sha256 `b140c4ca…`).
- [x] 🔒 Aplicada con el agente de Next.js (`20260915170457_booknow_mcp_service_phase5_permissions.sql`, commit `86d4ba3` en book-now-hub) siguiendo `docs/handoff/nextjs-migracion-fase5-permisos-mcp.md`. Verificado desde aquí en solo lectura: 8 migraciones sincronizadas, 9/9 permisos esperados, 0/19 prohibidos, `pg_cron` activo con un único job `17 3 * * *`. Contenido:
  - `UPDATE (last_used_at)` en `mcp_connections`; `INSERT` por columnas en `mcp_tool_calls` (sin `SELECT`, `UPDATE` ni `DELETE`).
  - `SELECT` por columnas en `appointments`, `customers`, `profiles`, `branches`, `services`, `specialist_schedules` y `schedule_exceptions` (sin notas internas, direcciones, documentos, contacto del personal, comisiones ni pagos).
  - `pg_cron` + función `purge_mcp_tool_calls()` (retención por negocio 7–90 días, 90 por defecto; sin `EXECUTE` para `anon`, `authenticated` ni `service_role`), programada a diario a las 03:17 UTC.
- [x] Snapshot local regenerado (incluye `pg_cron`, `purge_mcp_tool_calls` y 80 `GRANT` por columna) y `supabase db reset` verificado. El job de cron no está en local porque `db dump` no copia datos.

### Transversal
- [ ] `internal/platform/pii`: máscara determinista de teléfono (`+593******123`); `null` si no hay teléfono; máscara completa si es muy corto. Tests con fuzzing.
- [ ] `internal/platform/audit`: una fila en `mcp_tool_calls` por tool call al terminar (parámetros sanitizados, riesgo, duración, estado `succeeded|failed|denied`, código de error), sin secretos ni PII. Solo inserción.
- [x] Purga de auditoría: `pg_cron` en Supabase (D11); la retención por negocio se aplica en la función SQL.
- [x] Scopes: la v1 autoriza solo por rol (D5); no se exigen scopes por tool.
- [ ] Rate limit en memoria por instancia: 60 llamadas/min por conexión MCP (`MCP_RATE_LIMIT_PER_MINUTE`), respuesta 429 con `Retry-After` y limpieza de contadores inactivos. Peor caso acotado por `max-instances` de Cloud Run (D7).
- [ ] `last_used_at`: `UPDATE ... WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes')`, sin bloquear la respuesta si falla.

### Tools (cada una: filtro por `tenant_id` del contexto, auditoría, tests de integración cross-tenant y comparación con la salida del TS)
- [ ] `get_business_snapshot` — agregado sin PII.
- [ ] `get_schedule_summary` — rango ≤ 31 días, filtros `branchId?` y `specialistId?`.
- [ ] `list_available_slots` — servicio + sucursal + fecha. ⚠️ El TS **ignora** `specialist_schedules`, `schedule_exceptions` y `branches.operating_hours`, así que ofrece horas en las que el especialista no trabaja; en Go se respetan. `schedule_exceptions` no tiene `tenant_id`: se filtra por especialista y sucursal del tenant.
- [ ] `list_appointments` — paginación ≤ 100, teléfono enmascarado, sin `internal_notes`.
- [ ] `search_customers` — consulta ≥ 3 caracteres, sin notas privadas, direcciones ni facturación. ⚠️ El TS interpola la búsqueda en `.or()` de PostgREST (inyectable); en Go, SQL parametrizado con escape de `%` y `_`.

## Fase 6 — Escrituras en dos pasos

- [ ] `create_appointment_draft` — `appointments:write`:
  - [ ] TTL `MCP_DRAFT_TTL_MINUTES` (10).
  - [ ] Validación cross-tenant de cliente, servicio, variante, sucursal y especialista.
  - [ ] Snapshot de duración, precio y moneda.
  - [ ] `idempotency_key` (un reintento devuelve el mismo draft).
  - [ ] `human_summary` para aprobación humana.
  - [ ] No inserta en `appointments`.
- [ ] `confirm_appointment_draft` — `appointments:write`:
  - [ ] Llama a la RPC `confirm_mcp_appointment_draft` (transaccional, `FOR UPDATE`).
  - [ ] Idempotente; cita `pending` con `source = 'mcp'`.
- [ ] Tests: draft válido, slot inválido, IDs de otro tenant, reintento con la misma key, draft expirado, doble confirmación y **dos confirmaciones concurrentes** (una sola cita).

## Fase 7 — Twilio WhatsApp

> ⚠️ **Hallazgos en el código TS actual** (`migracion/twilio-whatsapp/`), relevantes también para producción hoy:
> 1. `twilio-webhook-route.ts` **no verifica `X-Twilio-Signature`**: cualquiera puede enviar un POST con un `From` falso y confirmar o cancelar citas.
> 2. Busca el cliente por los últimos 10 dígitos del teléfono **en todos los tenants** y modifica la cita más reciente de cualquiera de ellos.
> 3. Detecta la intención con `includes` sobre subcadenas (`"no"` coincide con `"buenos"`, `"si"` con `"casi"`).
> 4. No deduplica reintentos de Twilio.
> 5. En el archivo copiado, `notify-route.ts` no autentica al llamador. Hay que confirmar si el middleware de Next.js lo protege. Además, interpola datos sin escapar en el HTML de los emails.

- [ ] `internal/integration/twilio`: verificación de `X-Twilio-Signature` sobre la URL pública y el body original **antes** de parsear; `TWILIO_AUTH_TOKEN` desde Secret Manager.
- [ ] ⚠️ Resolución de tenant en el webhook (p. ej. por número destino `To` o por la cita notificada), nunca por teléfono global.
- [ ] ⚠️ Deduplicación persistente por `MessageSid` (tabla nueva → migración en el repo Next.js).
- [ ] Parser de intención por palabra completa y normalizada (tildes, mayúsculas), con tests table-driven y fuzzing.
- [ ] Caso de uso: confirmar o cancelar la cita del cliente en el tenant resuelto y registrar en `notifications`.
- [ ] Respuesta 200 rápida; trabajo costoso fuera del request si hace falta.
- [ ] Notificaciones salientes (`notify`): autenticación del llamador, WhatsApp con `TWILIO_CONTENT_SID` o fallback, email vía Resend (`internal/integration/resend`) con HTML escapado y `.ics`.
- [ ] Tests: firma válida, firma alterada, reintento duplicado, cliente en dos tenants, intención ambigua, timeout del proveedor.
- [ ] ⚠️ `campaigns-send-route.ts` es un stub: decidir si se migra.

## Fase 8 — Fallback REST `/api/actions/*`

- [ ] ⚠️ Confirmar que sigue siendo necesario (clientes sin MCP nativo).
- [ ] Reutilizar los mismos casos de uso de las tools (sin duplicar lógica) con el mismo middleware auth + tenant.
- [ ] DTOs con `DisallowUnknownFields`, límite de body y errores uniformes (400/401/403/404/409/413/415/422/429).
- [ ] CORS explícito (el TS usaba `*`).
- [ ] Tests de contrato contra los ejemplos capturados en la Fase 1.

## Fase 9 — Despliegue en Cloud Run

- [ ] 🔒 Proyecto GCP, Artifact Registry y cuenta de servicio de runtime por entorno con permisos mínimos.
- [ ] 🔒 Secretos en Secret Manager con versiones fijadas: `DATABASE_URL`, `TWILIO_AUTH_TOKEN`, `RESEND_API_KEY`.
- [ ] Conectividad a Supabase: Session Pooler (IPv4) o conexión directa (IPv6).
- [ ] `max_instances × DB_MAX_CONNS` dentro del límite de conexiones del plan de Supabase.
- [ ] Startup probe a `/readyz` y liveness a `/healthz` (la liveness no depende de la base).
- [ ] Concurrencia, timeout de request compatible con SSE y máximo de instancias.
- [ ] Staging privado; smoke tests por digest.
- [ ] Imagen escaneada, SBOM y provenance; despliegue por digest.
- [ ] Logs JSON en Cloud Logging y alertas de 401/403/429/5xx, latencia y fallos de JWKS.
- [ ] Rollback probado.
- [ ] 🔒 Dominio con HTTPS.

## Fases futuras (v2) — fuera del alcance actual

- [ ] **MCP de clientes finales** (`/mcp/customer` en el mismo servicio): resolver propio (token → `customers.user_id` en el negocio), tools limitadas a lo del propio cliente (`my_appointments`, `check_availability`, `request_appointment` con borrador y confirmación), y consentimiento y auditoría separados del MCP de negocio.
- [ ] **Asistente de la landing** para usuarios nuevos: registro y alta guiada vía MCP (tools de registro), con controles antiabuso (rate limit por IP, verificación de email o teléfono) porque todavía no hay cuenta autenticada.
- [ ] Scopes por tool cuando Supabase admita scopes propios (o una autorización equivalente por conexión).

## Fase 10 — QA integral, corte y retirada de Next.js

- [ ] 🔒 Supabase Auth: OAuth Server activo, Authorization Path `/oauth/consent` (la pantalla de consentimiento sigue en Next.js), ES256 como clave actual y DCR para MCP Inspector.
- [ ] 🔒 Usuarios QA del tenant piloto con roles admin, owner y manager, más un employee y un no miembro para las pruebas de denegación.
- [ ] QA: OAuth aprobado, denegado y expirado; conexión revocada; módulo deshabilitado; scope faltante; aislamiento cross-tenant; PII enmascarada; auditoría sin secretos; drafts con reintento y expiración; confirmación concurrente.
- [ ] Comparar métricas, errores y latencia entre Go y Next.js.
- [ ] 🔒 Revocar la conexión MCP activa con `client_id` legacy (`unknown_client`/`mcp-client`): inventario del 2026-09-15 → 3 conexiones activas válidas en Go y 1 legacy que dejará de funcionar.
- [ ] 🔒 Cambiar clientes MCP y el webhook de Twilio a la URL de Go (sin cambiar a la vez URL, proveedor y semántica).
- [ ] Retirar de Next.js: `src/lib/mcp/`, `src/lib/capabilities/`, `src/app/api/mcp/`, `src/app/api/actions/`, `src/app/api/webhooks/twilio/`.
- [ ] Handoff: guía de conexión (MCP Inspector y un host real), inventario de tools y scopes, runbooks de revocación y de desactivación del módulo, variables documentadas sin valores, resultados de QA y pendientes de v2.

---

## Decisiones abiertas

| # | Decisión | Propuesta actual | Estado |
|---|---|---|---|
| D1 | Conexión MCP sin match exacto `(usuario, client_id)` | Rechazar; sin fallback a otra conexión ni alta automática (el TS sí lo hacía). | ✅ Confirmado |
| D2 | Tokens sin `client_id` (sesión directa de Supabase) | Rechazar (`ErrClientRequired`): el `client_id` es lo que enlaza el token con la conexión consentida y su tenant. | ✅ Confirmado |
| D3 | Varias conexiones activas del mismo cliente en distintos tenants | Gana la de `updated_at` más reciente (último consentimiento); el TS usaba `created_at`. | ✅ Confirmado |
| D4 | Tenants en `trial` | Sin acceso, solo `active` (paridad TS). | Implementado; confirmar si es lo deseado |
| D5 | Scopes / quién usa el MCP | v1: solo personal (`owner`, `admin`, `manager`), autorizado por rol; sin scopes por tool (Supabase no emite scopes propios). Clientes finales y asistente de la landing → fases v2 con un MCP separado. | ✅ Confirmado |
| D6 | `MCP_ALLOWED_ROLES` en `.env.example` | Roles fijos en código (`owner`, `admin`, `manager`); eliminar la variable. | Pendiente |
| D7 | Rate limiting multi-instancia | En memoria por instancia, 60 llamadas/min por conexión; peor caso = límite × `max-instances`. Cloud Armor por IP como capa opcional en la Fase 9. | ✅ Confirmado |
| D8 | Deduplicación y resolución de tenant en Twilio | Tabla de eventos procesados + tenant por número destino o por cita notificada. Requiere migración en Next.js. | Pendiente (Fase 7) |
| D9 | Pooler de Supabase en producción | Session Pooler (5432) con el rol `booknow_mcp_service`. | ✅ Validado con el smoke test |
| D10 | Hallazgos de seguridad del webhook Twilio en producción actual | Corregir ya en Next.js o acelerar la Fase 7. | Pendiente |
| D11 | Purga de auditoría MCP | `pg_cron` en Supabase con `purge_mcp_tool_calls()`; el rol del servicio no puede borrar. | ✅ Aplicado en producción (job diario 03:17 UTC) |
