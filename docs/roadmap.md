# Roadmap — booknow-mcp-service

Checklist vivo de la migración del MCP, los webhooks de Twilio y la API de BookNow desde Next.js/Vercel a Go en Cloud Run.
Márcalo en el mismo commit que completa cada tarea.

**Leyenda:** `[x]` hecho y verificado · `[ ]` pendiente · 🔒 requiere acción manual del usuario (Supabase, Vercel, GCP, Twilio) · ⚠️ decisión abierta

**Fuentes:** `migracion/specs/*` (comportamiento), `migracion/docs/guia-migracion-next-scp-go-cloud-run.md` (fases y checklist de producción), `migracion/docs/seguridad-backend-go-cloud-run.md`, `CLAUDE.md` (reglas no negociables).

> La numeración sigue la guía de migración, pero el orden se ajustó a la prioridad del proyecto: primero MCP (Fases 4–6), luego Twilio (7) y el fallback REST (8). La guía proponía REST → webhooks → MCP.
> Desde el 2026-09-21 el foco es **terminar la migración del MCP** (Fases 8–10 sin Twilio); Twilio sigue en `docs/plan-twilio.md`.

## Resumen

| Fase | Estado |
|---|---|
| 1. Inventario y contrato | 🟡 parcial |
| 2. Esqueleto Go | ✅ hecho (falta despliegue a staging, ver Fase 9) |
| 3. Base de datos, autenticación y tenant | ✅ hecho y validado contra producción |
| 4. Endpoint `/mcp` y seguridad de transporte | ✅ hecho y validado contra producción (solo falta `last_used_at`, que requiere permiso) |
| 5. Plataforma transversal y tools de lectura | ✅ hecho y validado contra producción |
| 6. Escrituras en dos pasos (drafts) | ✅ hecho y validado contra producción (cita real creada por MCP) |
| 7. Twilio WhatsApp (webhook y notificaciones) | ⏸️ aplazada → `docs/plan-twilio.md` (7.1–7.2 hechas) |
| 8. Fallback REST `/api/actions/*` | ⬜ pendiente |
| 9. Despliegue en Cloud Run | 🟡 staging desplegado y validado; faltan producción, alertas, rollback y dominio |
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
- [x] `internal/platform/pii.MaskPhone`: `+593******567` para internacionales de 10+ dígitos, solo los 3 últimos en locales, máscara completa con 6 dígitos o menos, `nil` sin teléfono. Fuzzing (3 M de entradas): nunca revela más de 6 dígitos ni el número completo.
- [x] `internal/platform/audit` + `postgres.AuditStore`: una fila por tool call al terminar (estado `succeeded|failed`, riesgo, duración, códigos estables `INVALID_ARGUMENT|NOT_FOUND|INTERNAL`). Resumen sin PII (en `search_customers` solo `queryLength`/`byPhone`, nunca el texto). `INSERT` simple sin `RETURNING`. Se registra aunque el cliente se desconecte y un fallo de auditoría no rompe la tool.
- [x] Purga de auditoría: `pg_cron` en Supabase (D11); la retención por negocio se aplica en la función SQL.
- [x] Scopes: la v1 autoriza solo por rol (D5); no se exigen scopes por tool.
- [x] Rate limit en memoria por instancia (`internal/platform/ratelimit`): `MCP_RATE_LIMIT_PER_MINUTE` (60 por defecto, 1–10000) por conexión; 429 JSON-RPC `RATE_LIMITED` con `Retry-After`; las llamadas rechazadas no consumen cupo; limpieza de conexiones inactivas.
- [x] `last_used_at`: como mucho una escritura por conexión por minuto por instancia (memoria) y una cada 5 minutos en total (SQL); best effort con timeout de 1s.
- [x] Tests de integración ejecutados **como `booknow_mcp_service`** (`SET LOCAL ROLE`, pertenencia local en `supabase/roles.sql`): confirman que los permisos de producción bastan.
- [ ] Cancelación: con respuestas JSON sin stream no aplica; revisar `PropagateRequestCancellation` si una tool se vuelve lenta.

### Tools (`internal/application/business` + `postgres.BusinessStore` + `internal/mcpserver`)
Todas: tenant solo desde la conexión (test que intenta pasar `tenantId`/`tenant_id` como argumento), anotadas como solo lectura, auditadas, errores seguros para el LLM, tiempos en la zona horaria del negocio o de la sucursal y tests de integración con dos tenants.
- [x] `get_business_snapshot` — sucursales, especialistas y servicios activos, citas de hoy (zona horaria del negocio, no UTC como el TS), citas por estado y top 5 servicios de los últimos 30 días.
- [x] `get_schedule_summary` — rango ≤ 31 días en la zona horaria del negocio, sin canceladas, por día local (una cita a las 23:30 locales cuenta en su día, no en el de UTC) y por especialista; filtros de sucursal y especialista validados como UUID.
- [x] `list_available_slots` — respeta `specialist_schedules` (con descanso), `branches.operating_hours`, `schedule_exceptions` (día libre, ausencias parciales, horario especial por especialista o sucursal), citas `pending|confirmed|in_progress`, buffer del servicio y horas ya pasadas; rejilla de 30 min; fecha de hoy a +90 días. Excepciones sin sucursal ni especialista (sin tenant atribuible) se ignoran. Corrige el TS, que ignoraba horarios y usaba 08:00–19:00 UTC.
- [x] `list_appointments` — paginación (20 por defecto, máx. 100), estado validado contra el enum, teléfono enmascarado, sin `internal_notes`, fechas locales.
- [x] `search_customers` — 3–100 caracteres, `ILIKE` con comodines escapados y búsqueda por dígitos del teléfono (≥ 3 dígitos) con SQL parametrizado (el TS era inyectable), teléfono enmascarado.
- [x] Mutaciones detectadas: 8 en SQL (aislamiento por tenant, canceladas, zona horaria) y 10 en reglas de la capa de tools y huecos.
- [x] 🔒 Smoke en producción (2026-09-15) con token OAuth real, binario local → pooler con `booknow_mcp_service`:
  - `tools/list` = 6 tools sin parámetro de tenant; snapshot, resumen, listado, búsqueda y huecos responden con datos reales de Elvis Studio.
  - 34 teléfonos devueltos, todos enmascarados; sin `internal_notes`; horas con el offset del negocio (`-04:00`) o de la sucursal (`-05:00`).
  - Resumen 13 citas (sin canceladas) frente a 19 en el listado; huecos del miércoles 09:00–17:30 según horario real; sábado sin horario = 0.
  - Rechazos correctos: búsqueda de 1 carácter, fecha pasada, servicio inexistente, rango de 60 días, estado inválido; `%_%` se busca literal.
  - 17 filas de auditoría escritas como el rol: códigos `INVALID_ARGUMENT`/`NOT_FOUND`, `request_id` únicos, sin el texto buscado; `last_used_at` actualizado. Logs sin tokens, cadenas de conexión ni teléfonos.
- [x] ⚠️ Dato de Elvis Studio corregido en la Fase 6: la sucursal con horarios tenía `America/Guayaquil` y ahora usa `America/Caracas`. La sucursal San Cristóbal sigue sin horarios, así que no ofrece huecos.

## Fase 6 — Escrituras en dos pasos

> ⚠️ **Hallazgos en producción** (verificados en solo lectura el 2026-09-15) sobre `confirm_mcp_appointment_draft`:
> 1. **Nunca ha funcionado**: inserta `source = 'mcp'`, pero `appointments_source_check` no admite `mcp` (0 borradores y 0 citas MCP en producción).
> 2. Es `SECURITY DEFINER` con `EXECUTE` para `anon` y `authenticated` (vía `PUBLIC`): expuesta por la Data API y confía en el `p_actor_auth_user_id` recibido.
> 3. Doble reserva: dos borradores distintos del mismo especialista confirmados a la vez crean 2 citas solapadas (reproducido en local con dos sesiones).
> 4. Devuelve `to_jsonb(a.*)` (notas internas y pagos incluidos), que el TS reenvía al LLM; la respuesta idempotente no valida al actor.

### Prerrequisito: migración de permisos y corrección de la RPC (repo Next.js)
- [x] SQL preparado y validado en local: `docs/handoff/sql/booknow_mcp_service_phase6_drafts.sql` (sha256 `0fd7a8b2…`) + `verify_booknow_mcp_service_phase6.sql` (47/47 checks, idempotente, `ROLLBACK`). Concurrencia con dos sesiones: sin lock 2 citas, con la migración 1 (`SPECIALIST_UNAVAILABLE`); mismo borrador concurrente → 1 cita e idempotente. Contenido:
  - `CHECK` de `source` con `mcp`.
  - `SELECT` por columnas en `service_variants` y `mcp_appointment_drafts`; `INSERT` en drafts sin `id`, `status` ni campos de confirmación; sin `UPDATE`/`DELETE`.
  - RPC con misma firma y contrato: `search_path = ''`, actor validado siempre, advisory lock por especialista, 15 campos explícitos en el resultado.
  - `EXECUTE` solo para `service_role` (TS hasta el corte) y `booknow_mcp_service`.
- [x] 🔒 Pasos 1–2 del handoff hechos en Next.js (`20260915212242_booknow_mcp_service_phase6_drafts.sql`, hash correcto, 47/47 checks, base local limpia). Paso 3 OK tras el mantenimiento: 8/8 migraciones sincronizadas, el dry-run lista solo la nueva, md5 de la RPC `40250d38…`, 0 drafts, 0 `source` fuera de lista y archivo en LF.
- [x] 🔒 Aplicada con el agente de Next.js (`20260915212242_booknow_mcp_service_phase6_drafts.sql`, commit `5c22145` en book-now-hub, sin push) siguiendo `docs/handoff/nextjs-migracion-fase6-drafts-mcp.md`. Verificado desde aquí en solo lectura: md5 del cuerpo `94939d27…`, `search_path=""`, `EXECUTE` solo para `service_role` y `booknow_mcp_service`, permisos de drafts y variantes correctos, `CHECK` con `mcp` validado y 0 drafts.
- [x] Snapshot local regenerado. `supabase/seed.sql` aplica en local los mismos `REVOKE` que producción: al reaplicar el dump, los privilegios por defecto de Supabase local volvían a dar `EXECUTE` a `anon` y `authenticated` sobre `confirm_mcp_appointment_draft` y también sobre `purge_mcp_tool_calls`, desde la Fase 5. `go test ./...` con integración pasa.
- [x] D14 corregido en Next.js (commit `f451ed7`, sin push): `source: "app"` y sin enviar el error de Postgres al navegador. 🔒 Pendiente: push y una reserva real desde `/c/[tenant]`.
- [ ] Next.js (menor, no bloquea): `route.ts:78` todavía devuelve `slotsError.message` al navegador.

### Tools (`internal/application/drafts` + `postgres.DraftStore` + `internal/mcpserver`)
Ambas anotadas como escritura no destructiva e idempotente, auditadas con riesgo `write`; los argumentos extra (p. ej. `tenantId`) los rechaza el SDK antes de llegar al store.
- [x] `create_appointment_draft`:
  - [x] TTL `MCP_DRAFT_TTL_MINUTES` (10 por defecto, 1–60).
  - [x] Validación cross-tenant de cliente activo, variante activa del servicio y, vía `business.CheckSlot`, servicio, sucursal y especialista.
  - [x] Horario validado con las mismas reglas que `list_available_slots` (rejilla de 30 min, turnos, descansos, excepciones, citas y buffer) y con la duración de la variante; especialista obligatorio si el servicio lo requiere; fecha pasada o a más de 90 días rechazada.
  - [x] Snapshot de duración, precio (`base_price + price_modifier`) y moneda.
  - [x] `idempotencyKey` por conexión: reintento con los mismos datos → mismo borrador sin volver a comprobar el horario; misma clave con otros datos → `CONFLICT`; carrera de dos inserts → gana uno y el otro lo reutiliza.
  - [x] `humanSummary` en español con hora local de la sucursal, sin notas ni teléfonos; indica expiración y que se pida aprobación explícita.
  - [x] No inserta en `appointments` (el rol no puede).
- [x] `confirm_appointment_draft`:
  - [x] Exige borrador del mismo tenant **y conexión** con la misma `idempotencyKey` (D13); si no, `NOT_FOUND`.
  - [x] Expirado o cancelado → `CONFLICT` sin llamar a la RPC; ya confirmado → respuesta idempotente aunque haya pasado el TTL.
  - [x] RPC `confirm_mcp_appointment_draft`; errores por prefijo → `NOT_FOUND`, `FORBIDDEN` (estado `denied`), `CONFLICT`; nunca el texto de Postgres.
- [x] Códigos de auditoría nuevos: `CONFLICT` y `FORBIDDEN`. El resumen de auditoría solo lleva IDs, `scheduledAt` y `hasNotes` (sin notas ni clave).
- [x] Tests:
  - Unitarios: borrador válido, horario no disponible, IDs de otro tenant, especialista obligatorio, reintento, clave reutilizada con otros datos, carrera, expiración, conexión o clave distinta, mapeo de errores de la RPC. 12 mutaciones detectadas en el servicio y 5 en la capa MCP.
  - Integración como `booknow_mcp_service`: lecturas por tenant, insert idempotente, nombres sin fuga entre tenants, confirmación e idempotencia, errores reales de la RPC (expirado, solapado, rol `employee`, cancelado, inexistente) y **confirmaciones concurrentes con datos confirmados** (solapadas → 1 cita; mismo borrador ×2 → 1 cita + 1 idempotente). 8 mutaciones de SQL detectadas.
  - End-to-end local: cliente MCP → huecos → borrador → reintento → confirmación → repetición → listado → el hueco desaparece.
- [x] 🔒 Smoke en producción (2026-09-15) con token OAuth real, binario local → pooler con `booknow_mcp_service`:
  - `tools/list` = 8 tools; las dos nuevas anotadas como escritura no destructiva e idempotente y sin parámetro de tenant (un `tenantId` extra lo rechaza el SDK).
  - Borrador creado (TTL 10 min, `humanSummary` sin notas), reintento con la misma clave reutilizado, misma clave con otros datos → `CONFLICT`, clave incorrecta al confirmar → `NOT_FOUND`, `draftId` inválido → `INVALID_ARGUMENT`.
  - Cita real creada a petición del usuario para el cliente `Ps3udo` (su propio usuario): `49f54686` Corte de cabello en Tariba con Miguel, 2026-09-25 13:30–14:00 `America/Caracas`, `pending`, `source = 'mcp'`, con su fila en `appointment_services` y sin `internal_notes`. Segunda confirmación idempotente con la misma cita y el hueco deja de ofrecerse. Se deja creada a propósito para revisarla en el panel.
  - 8 filas de auditoría con riesgo `write` y códigos correctos; ningún resumen contiene notas ni claves. Logs sin token, sin cadena de conexión y sin teléfonos.
  - Queda un borrador de prueba sin confirmar que expira solo (el rol no puede borrar; lo purgará D12 cuando se implemente).
- [x] ⚠️ Zona horaria de Elvis Studio corregida: la sucursal Tariba (Venezuela) tenía `America/Guayaquil` y ofrecía horarios con una hora de desfase. Script ejecutado por el usuario en el SQL Editor: `docs/handoff/sql/fix_elvis_studio_tariba_timezone.sql` (6 horarios activos, 0 citas futuras afectadas). Ambas sucursales quedan en `America/Caracas`.
- [ ] Revisar las citas `mcp` en el panel de Next.js (etiqueta de `source`) antes de exponer las tools a usuarios reales.

## Fase 7 — Twilio WhatsApp ⏸️ aplazada

Separada del camino crítico el 2026-09-21: pagos y número de WhatsApp del SaaS se definen en una reunión con el equipo.
Plan, checklist y decisiones de Twilio en **`docs/plan-twilio.md`**. Lo ya hecho (7.1 firma, 7.2 endpoint) queda en `main`:
si `TWILIO_AUTH_TOKEN` y `TWILIO_WEBHOOK_URL` están vacías la ruta `/webhooks/twilio` no se sirve, así que no bloquea desplegar el MCP.

## Fase 8 — Fallback REST `/api/actions/*`

- [ ] ⚠️ Confirmar que sigue siendo necesario (clientes sin MCP nativo).
- [ ] Reutilizar los mismos casos de uso de las tools (sin duplicar lógica) con el mismo middleware auth + tenant.
- [ ] DTOs con `DisallowUnknownFields`, límite de body y errores uniformes (400/401/403/404/409/413/415/422/429).
- [ ] CORS explícito (el TS usaba `*`).
- [ ] Tests de contrato contra los ejemplos capturados en la Fase 1.

## Fase 9 — Despliegue en Cloud Run

Runbook: **`docs/runbook-cloud-run.md`**. Manifiesto y scripts en `deploy/cloudrun/`. Lo marcado como "preparado" está en el repo y verificado en local, pero todavía no en GCP.

- [x] Preparado en el repo:
  - `service.yaml` declarativo y `render.sh`, que exige imagen por digest, rechaza `<placeholders>` y lee el `.env` como datos, sin ejecutarlo.
  - Valores de staging y producción sin secretos; `smoke.sh` de solo lectura.
  - CI con ShellCheck y build de la imagen.
- [x] Imagen de producción verificada en local contra Supabase local: respeta `$PORT`, corre como `nonroot`, apagado limpio con SIGTERM, `/webhooks/twilio` no se sirve sin configuración, `smoke.sh` pasa. Con `APP_ENV=staging` arranca contra el JWKS de producción.
- [x] Logs con `severity`/`message` para Cloud Logging (`internal/platform/logging`). Con `level`/`msg`, todas las entradas quedaban sin severidad y no se podía alertar por errores. 3 mutaciones detectadas.
- [x] 🔒 Proyecto GCP `agendia-mcp`, Artifact Registry `agendia-mcp` (`us-west1`) y cuenta de runtime `agendia-mcp-runner`. Verificado el 2026-09-22: a nivel de proyecto la cuenta solo tiene Logs Writer y Monitoring Metric Writer.
- [x] 🔒 Secretos en Secret Manager con versiones fijadas: `booknow-mcp-staging-database-url` y `booknow-mcp-database-url` (v1). Formato verificado sin leer el valor y `secretAccessor` solo para la cuenta de runtime en cada secreto. Los de Twilio y Resend, en `docs/plan-twilio.md`.
- [x] Conectividad desde Cloud Run al Session Pooler con `booknow_mcp_service`: `/readyz` en 200 y la fila de auditoría de `health` escrita en producción.
- [x] 🔒 `max_instances × DB_MAX_CONNS` dentro del pool: 1×2 (staging) + 3×4 (producción) = 14 frente a un Pool Size de 15 (confirmado por el usuario el 2026-09-22).
- [x] Startup probe a `/readyz` (pasa al segundo intento) y liveness a `/healthz`, que pasa dentro de la instancia. Desde fuera, el frontend de Cloud Run reserva `/healthz` y responde 404 antes de llegar al contenedor, así que `smoke.sh` usa `/readyz`.
- [x] Concurrencia 40, timeout 60 s (sin SSE) y máximo de instancias aplicados con `services replace`.
- [x] Staging privado desplegado el 2026-09-22 (`booknow-mcp-staging`, imagen `@sha256:c4fc5a11…` del commit `9939666`):
  - Sin IAM → 403.
  - `smoke.sh` con IAM (`X-Serverless-Authorization`) y token OAuth real: 17/17 checks (metadata, 401, Origin 403, `initialize`, 8 tools, `health`, GET 405).
  - Logs con severidad correcta y sin tokens, cadenas de conexión ni contraseñas.
- [ ] Imagen escaneada (Artifact Registry) y despliegue por digest: preparados. SBOM y provenance quedan para el pipeline con Workload Identity Federation.
- [x] Alertas en producción (2026-09-22, `deploy/monitoring/apply.sh`, idempotente): 5xx, pico de 4xx, latencia p95, JWKS, Postgres y tools con error interno. Avisan a `v.pseudo.developer@gmail.com`. Sentry, más adelante.
- [x] Rollback probado en staging (2026-09-22): tráfico devuelto a la revisión 00001, smoke en verde y vuelta a la última revisión.
- [ ] 🔒 Dominio con HTTPS: `mcp.agendia.store` (Cloudflare, CNAME en Solo DNS) con domain mapping. Falta verificar `agendia.store` en Search Console (pasos en el runbook).
- [ ] 🔒 Servicio de producción `booknow-mcp` (público, mismo digest que staging). El auto mode de Claude Code bloqueó el despliegue: lo ejecuta el usuario.

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
- [ ] 🔒 Cambiar los clientes MCP a la URL de Go (sin cambiar a la vez URL, proveedor y semántica). El webhook de Twilio, en `docs/plan-twilio.md`.
- [ ] Retirar de Next.js: `src/lib/mcp/`, `src/lib/capabilities/`, `src/app/api/mcp/`, `src/app/api/actions/` (`src/app/api/webhooks/twilio/` se retira con el plan de Twilio).
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
| D6 | `MCP_ALLOWED_ROLES` en `.env.example` | Roles fijos en código (`owner`, `admin`, `manager`); variable eliminada, junto con `MCP_AUDIT_RETENTION_DAYS` (la retención la aplica la función SQL). | ✅ Hecho |
| D7 | Rate limiting multi-instancia | En memoria por instancia, 60 llamadas/min por conexión; peor caso = límite × `max-instances`. Cloud Armor por IP como capa opcional en la Fase 9. | ✅ Confirmado |
| D9 | Pooler de Supabase en producción | Session Pooler (5432) con el rol `booknow_mcp_service`. | ✅ Validado con el smoke test |
| D11 | Purga de auditoría MCP | `pg_cron` en Supabase con `purge_mcp_tool_calls()`; el rol del servicio no puede borrar. | ✅ Aplicado en producción (job diario 03:17 UTC) |
| D12 | Retención de `mcp_appointment_drafts` (guarda `customer_notes` del LLM) | Purgar borradores no confirmados antiguos con el mismo cron; no incluido en la migración de la Fase 6. | Pendiente |
| D14 | `source: "client_app"` en la app cliente de Next.js, rechazado por el `CHECK` | La ruta se usa (botón de reservar en `/c/[tenant]`) y en producción solo hay citas `web` (38): ninguna reserva del cliente se había guardado. Se usa `app` (solo código). | ✅ Commit `f451ed7` en Next.js, pendiente de push |
| D13 | Confirmar un borrador desde otra conexión del mismo tenant | Go exige mismo tenant, misma conexión y la misma `idempotencyKey` antes de llamar a la RPC (la RPC solo valida el rol del actor). | ✅ Confirmado |

D8, D10 y D15–D18 (Twilio) se movieron a `docs/plan-twilio.md`.
