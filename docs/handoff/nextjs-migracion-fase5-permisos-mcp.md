# Instrucciones para el agente del repo Next.js — permisos de la Fase 5 del MCP y purga de auditoría

> Copia este documento completo como prompt al agente que trabaja en el repo **Next.js de BookNow** (dueño de las migraciones de Supabase).
> Fecha: 2026-09-15 · Proyecto Supabase: `book-now-hub` (`rrnysepngbycvuciodoj`)
> Precedente: la migración `20260915160346_booknow_mcp_service_role.sql` ya creó el rol `booknow_mcp_service` en producción.

---

## Contexto

El servicio Go `booknow-mcp-service` se conecta a Postgres con el rol de mínimo privilegio `booknow_mcp_service`. Hoy solo tiene `SELECT` sobre las 5 tablas de autorización del MCP. En la Fase 5 implementará las tools de lectura (`get_business_snapshot`, `get_schedule_summary`, `list_available_slots`, `list_appointments`, `search_customers`), la auditoría en `mcp_tool_calls` y el registro de uso de la conexión.

Decisiones del usuario que esta migración aplica:

- **Permisos por columna** en tablas con datos personales. El servicio no puede leer notas internas, direcciones, documentos, email o teléfono del personal, comisiones ni campos de pago, ni siquiera por error.
- **Auditoría solo de inserción**: sin `SELECT`, `UPDATE` ni `DELETE` sobre `mcp_tool_calls`.
- **Purga diaria de auditoría con `pg_cron` dentro de Supabase**, según la retención de cada negocio (`tenant_modules.config.audit_retention_days`, entre 7 y 90 días; 90 por defecto). El rol del servicio **no** recibe permiso para borrar.

Todo se validó antes desde el repo Go:

- **Base local:** Postgres 17 de Supabase, dentro de una transacción con `ROLLBACK`.
  - Ejecutada dos veces sin error y con un solo job de cron.
  - Los 9 permisos esperados existen y los 19 prohibidos no.
  - La purga respeta 7 días, el valor por defecto de 90, valores inválidos (→ 90), valores altos (500 → 90) y bajos (3 → 7).
  - Como el rol, `notes`, `SELECT *` sobre `customers`, leer la auditoría, cambiar `status` y ejecutar la purga dan `permission denied`.
- **Producción, en solo lectura:** las 80 columnas que concede la migración existen. Todavía no existen ni `pg_cron` ni la función `purge_mcp_tool_calls`.

## Archivos fuente (misma máquina)

- Migración: `C:\Users\DELL\Desktop\code\experimental\MCP\docs\handoff\sql\booknow_mcp_service_phase5_permissions.sql`
- Script de verificación local: `C:\Users\DELL\Desktop\code\experimental\MCP\docs\handoff\sql\verify_booknow_mcp_service_phase5.sql`

SHA-256 de la migración, calculado sin retornos de carro:
`b140c4cab85518f224d24f64a35f72816a64520f5970f9627a52e12f568dcdba`

## Reglas obligatorias

- **Copia el archivo de migración tal cual**, sin editarlo. Si crees que algo debe cambiar, **detente y pregunta**.
- No añadas contraseñas, ni `ALTER ROLE`, ni permisos para `anon`, `authenticated`, `public` o `service_role`.
- No modifiques políticas RLS, tablas ni otras funciones.
- No ejecutes `supabase db push` sin mostrar antes `migration list` y `db push --dry-run`, y sin un "sí" explícito del usuario.
- No uses `migration repair` sin autorización.
- No ejecutes `select public.purge_mcp_tool_calls()` en producción: la ejecuta el cron.

## Paso 1 — Crear la migración

```bash
supabase migration new booknow_mcp_service_phase5_permissions
```

Reemplaza el contenido del archivo generado con el archivo fuente, copiándolo byte a byte:

```bash
cp "C:/Users/DELL/Desktop/code/experimental/MCP/docs/handoff/sql/booknow_mcp_service_phase5_permissions.sql" \
   supabase/migrations/<timestamp>_booknow_mcp_service_phase5_permissions.sql
```

Comprueba el hash. Debe coincidir con el indicado arriba; si no coincide, **detente**:

```bash
tr -d '\r' < supabase/migrations/<timestamp>_booknow_mcp_service_phase5_permissions.sql | sha256sum
```

## Paso 2 — Validar en local (transacción con ROLLBACK)

Como en la migración anterior, este repo no puede usar `supabase db reset`. Con `MSYS_NO_PATHCONV=1`, Git Bash no convierte rutas `/c/...`: usa rutas `C:/...` en `docker cp`. Usa la base local del proyecto MCP (contenedor `supabase_db_MCP`), que tiene el esquema de producción y el rol. El script aplica la migración dos veces, ejecuta todas las comprobaciones y termina con `ROLLBACK`, así que no deja cambios.

```bash
MSYS_NO_PATHCONV=1 docker cp supabase/migrations/<timestamp>_booknow_mcp_service_phase5_permissions.sql supabase_db_MCP:/tmp/mig.sql
MSYS_NO_PATHCONV=1 docker cp "C:/Users/DELL/Desktop/code/experimental/MCP/docs/handoff/sql/verify_booknow_mcp_service_phase5.sql" supabase_db_MCP:/tmp/ver.sql
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP psql -U postgres -q -P pager=off -v migration=/tmp/mig.sql -f /tmp/ver.sql
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP rm -f /tmp/mig.sql /tmp/ver.sql
```

Resultado esperado:

| Sección | Esperado |
|---|---|
| 1. Idempotencia | `jobs = 1`, `schedule = 17 3 * * *`, `command = select public.purge_mcp_tool_calls()` |
| 2. Permisos esperados | `todos_true = t`, `fallan` vacío |
| 3. Permisos prohibidos | `alguno_true = f`, `sobran` vacío |
| 4. Purga | `filas_borradas = 9`; r7 `{2}`, rbad/rbig/rdef `{2,8,50}`, rlow `{2}` |
| 5. Consultas como el rol | 3 conteos en 0, `insert de auditoria como el rol: OK`, y exactamente **5** `ERROR: permission denied` (customers ×2, mcp_tool_calls, mcp_connections, purge_mcp_tool_calls) |

Esos 5 errores son esperados y la transacción termina en `ROLLBACK`. Cualquier otro error es una condición de parada.

Confirma después que la base local quedó limpia (debe devolver `0|0`):

```bash
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP psql -U postgres -tAc "select (select count(*) from pg_extension where extname='pg_cron') || '|' || (select count(*) from pg_proc where proname='purge_mcp_tool_calls')"
```

## Paso 3 — Preparar producción

```bash
supabase migration list        # todo sincronizado salvo la migración nueva
supabase db push --dry-run     # debe listar SOLO <timestamp>_booknow_mcp_service_phase5_permissions.sql
```

**Detente aquí.** Muestra al usuario la salida de ambos comandos, el hash comprobado y el resumen de la validación local. Pregunta: **"¿Aplico esta migración a producción (book-now-hub)?"**

## Paso 4 — Aplicar (solo con un "sí" explícito)

```bash
supabase db push
```

## Paso 5 — Verificar en producción (solo lectura)

Ejecuta con `supabase db query --linked` o en el SQL Editor:

```sql
-- A. Permisos esperados: todos_true = true
select bool_and(ok) as todos_true, string_agg(case when not ok then chk end, ', ') as fallan from (values
  ('upd last_used_at', has_column_privilege('booknow_mcp_service','public.mcp_connections','last_used_at','UPDATE')),
  ('ins tool_calls', has_column_privilege('booknow_mcp_service','public.mcp_tool_calls','safe_summary','INSERT')),
  ('sel appt customer_notes', has_column_privilege('booknow_mcp_service','public.appointments','customer_notes','SELECT')),
  ('sel customers phone', has_column_privilege('booknow_mcp_service','public.customers','phone','SELECT')),
  ('sel profiles full_name', has_column_privilege('booknow_mcp_service','public.profiles','full_name','SELECT')),
  ('sel branches hours', has_column_privilege('booknow_mcp_service','public.branches','operating_hours','SELECT')),
  ('sel services price', has_column_privilege('booknow_mcp_service','public.services','base_price','SELECT')),
  ('sel schedules', has_column_privilege('booknow_mcp_service','public.specialist_schedules','start_time','SELECT')),
  ('sel exceptions', has_column_privilege('booknow_mcp_service','public.schedule_exceptions','is_day_off','SELECT'))
) v(chk, ok);

-- B. Permisos prohibidos (las mismas 19 comprobaciones que en local): alguno_true = false
select bool_or(ok) as alguno_true, string_agg(case when ok then chk end, ', ') as sobran from (values
  ('upd connections status', has_column_privilege('booknow_mcp_service','public.mcp_connections','status','UPDATE')),
  ('sel tool_calls', has_table_privilege('booknow_mcp_service','public.mcp_tool_calls','SELECT')),
  ('upd tool_calls', has_table_privilege('booknow_mcp_service','public.mcp_tool_calls','UPDATE')),
  ('del tool_calls', has_table_privilege('booknow_mcp_service','public.mcp_tool_calls','DELETE')),
  ('sel appt internal_notes', has_column_privilege('booknow_mcp_service','public.appointments','internal_notes','SELECT')),
  ('sel appt advance_amount', has_column_privilege('booknow_mcp_service','public.appointments','advance_amount','SELECT')),
  ('ins appointments', has_table_privilege('booknow_mcp_service','public.appointments','INSERT')),
  ('sel customers notes', has_column_privilege('booknow_mcp_service','public.customers','notes','SELECT')),
  ('sel customers address', has_column_privilege('booknow_mcp_service','public.customers','address','SELECT')),
  ('sel customers document', has_column_privilege('booknow_mcp_service','public.customers','document_number','SELECT')),
  ('sel customers email', has_column_privilege('booknow_mcp_service','public.customers','email','SELECT')),
  ('sel profiles email', has_column_privilege('booknow_mcp_service','public.profiles','email','SELECT')),
  ('sel profiles commission', has_column_privilege('booknow_mcp_service','public.profiles','commission_percentage','SELECT')),
  ('sel branches phone', has_column_privilege('booknow_mcp_service','public.branches','phone','SELECT')),
  ('sel exceptions reason', has_column_privilege('booknow_mcp_service','public.schedule_exceptions','reason','SELECT')),
  ('exec purge service', has_function_privilege('booknow_mcp_service','public.purge_mcp_tool_calls()','EXECUTE')),
  ('exec purge anon', has_function_privilege('anon','public.purge_mcp_tool_calls()','EXECUTE')),
  ('exec purge authenticated', has_function_privilege('authenticated','public.purge_mcp_tool_calls()','EXECUTE')),
  ('exec purge service_role', has_function_privilege('service_role','public.purge_mcp_tool_calls()','EXECUTE'))
) v(chk, ok);

-- C. Cron y extensión: pg_cron = 1, jobs = 1, schedule '17 3 * * *', active = true
select (select count(*) from pg_extension where extname = 'pg_cron') as pg_cron,
       count(*) as jobs, max(schedule) as schedule, bool_and(active) as active
from cron.job where jobname = 'purge-mcp-tool-calls';
```

Luego `supabase migration list`: la migración nueva debe figurar en local y remoto.

## Paso 6 — Commit

Commit en el repo Next.js que incluya **solo** el archivo de migración:

```
chore(db): grant booknow_mcp_service phase 5 read/audit permissions and schedule MCP audit purge
```

No hagas push a GitHub salvo que el usuario lo pida.

## Qué reportar al terminar

1. Nombre del archivo y hash comprobado.
2. Resumen de la validación local (tabla del Paso 2) y confirmación de base limpia (`0|0`).
3. Salidas de `migration list` y `db push --dry-run`.
4. Resultados A, B y C en producción.
5. Hash del commit.
6. Cualquier desviación, sin mostrar contraseñas ni cadenas de conexión.

## Condiciones de parada

Detente y avisa al usuario si ocurre cualquiera de estos casos:

- El hash del archivo no coincide.
- `db push --dry-run` lista algo más que la migración nueva, o local y remoto no están sincronizados.
- `create extension pg_cron` falla por permisos en producción. Alternativa a proponer, sin ejecutarla: activar `pg_cron` desde Dashboard → Integrations → Cron y volver a intentar el push.
- Alguna columna no existe (`column ... does not exist`).
- La validación local o las verificaciones A, B o C no dan exactamente lo esperado.

## Nota para el servicio Go

La auditoría tiene `INSERT` sin `SELECT`: el servicio hace un `INSERT` simple, sin `RETURNING` ni `ON CONFLICT`, con un `request_id` único por llamada.

## Nota para migraciones futuras

La **Fase 6** (borradores y confirmación de citas) necesitará otra migración igual de explícita: `SELECT`/`INSERT`/`UPDATE` por columnas en `mcp_appointment_drafts`, `SELECT` en `service_variants` y `EXECUTE` sobre `confirm_mcp_appointment_draft`. Nunca se usarán `GRANT ALL` ni `ALTER DEFAULT PRIVILEGES` para este rol.
