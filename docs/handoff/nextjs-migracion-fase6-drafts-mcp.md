# Instrucciones para el agente del repo Next.js — Fase 6 del MCP: borradores, confirmación de citas y corrección de la RPC

> Copia este documento completo como prompt al agente que trabaja en el repo **Next.js de BookNow** (dueño de las migraciones de Supabase).
> Fecha: 2026-09-15 · Proyecto Supabase: `book-now-hub` (`rrnysepngbycvuciodoj`)
> Precedentes: `20260915160346_booknow_mcp_service_role.sql` (rol) y `20260915170457_booknow_mcp_service_phase5_permissions.sql` (lecturas, auditoría y purga) ya están en producción.

---

## Contexto

El servicio Go `booknow-mcp-service` va a implementar las dos tools de escritura del MCP: `create_appointment_draft` y `confirm_appointment_draft`. Para eso el rol `booknow_mcp_service` necesita permisos nuevos. Al preparar la migración, desde el repo Go se encontraron **tres problemas en producción** en la función `confirm_mcp_appointment_draft`, que esta migración también corrige:

1. **La confirmación nunca ha funcionado.** La función inserta `source = 'mcp'`, pero `appointments_source_check` solo admite `web, app, phone, walk_in, whatsapp, instagram`. Toda confirmación falla con `violates check constraint "appointments_source_check"`. En producción hay 0 borradores y 0 citas `mcp`.
2. **Cualquiera puede ejecutar la función.** Es `SECURITY DEFINER` y `anon` y `authenticated` tienen `EXECUTE` (heredado de `PUBLIC`), así que está expuesta por la Data API (`/rest/v1/rpc/...`) con la clave pública. Además confía en el `p_actor_auth_user_id` que recibe.
3. **Doble reserva con confirmaciones concurrentes.** `FOR UPDATE` bloquea el borrador, pero dos borradores **distintos** para el mismo especialista y hora pueden confirmarse a la vez, y ambos pasan la comprobación de solapamiento. Se reprodujo en local con dos sesiones: se crearon 2 citas solapadas.

Además, la función devolvía `to_jsonb(a.*)`: la cita completa, incluidas notas internas y campos de pago. El MCP en TypeScript se lo pasa tal cual al LLM.

## Qué hace la migración

| # | Cambio | Motivo |
|---|---|---|
| 0 | Recrea `appointments_source_check` con los mismos valores más `'mcp'` (`NOT VALID` + `VALIDATE`). | Problema 1. |
| 1 | `SELECT` por columnas en `service_variants` (`id, tenant_id, service_id, name, duration_modifier, price_modifier, is_active`). Sin `description`. | Validar la variante y calcular duración y precio. |
| 2 | `SELECT` por columnas en `mcp_appointment_drafts` (todas menos `updated_at`) e `INSERT` por columnas **sin** `id`, `status`, `confirmed_appointment_id` ni `confirmed_at`. Sin `UPDATE` ni `DELETE`. | El servicio crea borradores y los relee (reintentos idempotentes, comprobación de tenant), pero no puede marcarlos como confirmados ni tocar `appointments`. |
| 3 | `CREATE OR REPLACE` de `confirm_mcp_appointment_draft` con **la misma firma, las mismas claves de resultado (`success`, `idempotent`, `appointment`) y los mismos prefijos de error**. | Compatible con el MCP de Next.js hasta el corte. |
| 4 | `REVOKE ALL` de `public`, `anon` y `authenticated`; `EXECUTE` solo para `service_role` (MCP de Next.js, que usa `supabaseAdmin`) y `booknow_mcp_service`. | Problema 2. |

Cambios dentro de la función:

- `search_path = ''` (antes `public`); todos los objetos ya iban calificados con esquema.
- El actor se valida **siempre** contra el tenant del borrador (rol `owner|admin|manager` activo), también en la respuesta idempotente. Antes, quien conociera el `draft_id` recibía la cita.
- `pg_advisory_xact_lock` por especialista antes de la comprobación de solapamiento (problema 3). Solo serializa las confirmaciones MCP entre sí; las citas creadas desde el panel no toman ese lock.
- `appointment` se devuelve con 15 campos explícitos: `id, tenant_id, branch_id, customer_id, specialist_id, service_id, service_variant_id, scheduled_at, ends_at, duration_minutes, status, estimated_price, currency_code, source, created_at`.
- Se elimina el `UPDATE ... status = 'expired'` previo al `RAISE`: la excepción lo deshacía siempre, así que no tenía efecto. La expiración la define `expires_at`.

## Validación hecha desde el repo Go

- **Producción, en solo lectura:** el cuerpo actual de la función coincide con el snapshot local (md5 `40250d38275bbf1163e7f99ccd2cc1f5`). Existen las columnas de `service_variants` y `mcp_appointment_drafts`, el `CHECK` no incluye `mcp`, `anon` y `authenticated` tienen `EXECUTE`, y hay 0 borradores y 0 citas `mcp`.
- **Base local, en transacción con `ROLLBACK`:** la migración se aplica dos veces sin error. Pasan los 47 checks: 9 permisos y ajustes esperados, 15 permisos prohibidos, 14 de comportamiento como el rol y 9 operaciones denegadas.
  - Comportamiento: reintento sin duplicados, cita `pending`/`mcp`, doble confirmación idempotente, solapamiento, citas contiguas, expiración, `employee`, admin inactivo, admin de otro tenant y borrador inexistente.
  - Operaciones denegadas: incluye `anon` y `authenticated` ejecutando la función.
- **Concurrencia real con dos sesiones (datos confirmados en local y base restaurada después):**
  - Sin el advisory lock: 2 citas solapadas.
  - Con la migración: la segunda sesión recibe `SPECIALIST_UNAVAILABLE` y queda 1 cita.
  - El mismo borrador confirmado dos veces a la vez: 1 cita, y la segunda respuesta es idempotente.

## Archivos fuente (misma máquina)

- Migración: `C:\Users\DELL\Desktop\code\experimental\MCP\docs\handoff\sql\booknow_mcp_service_phase6_drafts.sql`
- Script de verificación local: `C:\Users\DELL\Desktop\code\experimental\MCP\docs\handoff\sql\verify_booknow_mcp_service_phase6.sql`

SHA-256 de la migración, calculado sin retornos de carro:
`0fd7a8b286c806b06be2953de2fdb674f286b5348517901b61d86f7f9905ff03`

## Reglas obligatorias

- **Copia el archivo de migración tal cual**, sin editarlo. Si crees que algo debe cambiar, **detente y pregunta**.
- No añadas contraseñas, ni `ALTER ROLE`, ni permisos para `anon`, `authenticated` o `public`.
- No modifiques políticas RLS, otras tablas ni otras funciones.
- No cambies código TypeScript en este encargo (ver Paso 7: solo se reporta).
- No ejecutes `supabase db push` sin mostrar antes `migration list` y `db push --dry-run`, y sin un "sí" explícito del usuario.
- No uses `migration repair` sin autorización.
- No llames a `confirm_mcp_appointment_draft` en producción ni crees borradores o citas de prueba allí.

## Paso 1 — Crear la migración

```bash
supabase migration new booknow_mcp_service_phase6_drafts
```

Reemplaza el contenido del archivo generado con el archivo fuente, copiándolo byte a byte:

```bash
cp "C:/Users/DELL/Desktop/code/experimental/MCP/docs/handoff/sql/booknow_mcp_service_phase6_drafts.sql" \
   supabase/migrations/<timestamp>_booknow_mcp_service_phase6_drafts.sql
```

Comprueba el hash. Debe coincidir con el indicado arriba; si no coincide, **detente**:

```bash
tr -d '\r' < supabase/migrations/<timestamp>_booknow_mcp_service_phase6_drafts.sql | sha256sum
```

Si Git convierte el archivo a CRLF, conviértelo a LF antes de hacer commit: el cuerpo de la función se guarda tal cual y el md5 del Paso 5 cambiaría.

## Paso 2 — Validar en local (transacción con ROLLBACK)

Igual que en la Fase 5: usa la base local del proyecto MCP (contenedor `supabase_db_MCP`), con rutas `C:/...` y `MSYS_NO_PATHCONV=1`. El script aplica la migración dos veces, crea datos de prueba, confirma borradores como el rol y termina con `ROLLBACK`.

```bash
MSYS_NO_PATHCONV=1 docker cp supabase/migrations/<timestamp>_booknow_mcp_service_phase6_drafts.sql supabase_db_MCP:/tmp/mig.sql
MSYS_NO_PATHCONV=1 docker cp "C:/Users/DELL/Desktop/code/experimental/MCP/docs/handoff/sql/verify_booknow_mcp_service_phase6.sql" supabase_db_MCP:/tmp/ver.sql
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP psql -U postgres -q -P pager=off -v migration=/tmp/mig.sql -f /tmp/ver.sql
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP rm -f /tmp/mig.sql /tmp/ver.sql
```

Resultado esperado:

- Un `NOTICE` sobre la pertenencia de `postgres` a `booknow_mcp_service` (inofensivo) y una tabla con 5 `id` de borradores.
- Tabla por sección:

  | seccion | checks | fallan |
  |---|---|---|
  | 2 | 9 | 0 |
  | 3 | 15 | 0 |
  | 4 | 14 | 0 |
  | 5 | 9 | 0 |

- "checks que fallan" vacío (`0 rows`).
- Ningún `ERROR`: el script captura los errores esperados internamente.

Confirma después que la base local quedó igual que antes. Debe devolver `40250d38275bbf1163e7f99ccd2cc1f5|true|0`:

```bash
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP psql -U postgres -tAc "select md5(prosrc) || '|' || has_function_privilege('anon','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE') || '|' || (select count(*) from public.tenants where slug like 'phase6-%') from pg_proc where proname='confirm_mcp_appointment_draft'"
```

## Paso 3 — Preparar producción

```bash
supabase migration list        # todo sincronizado salvo la migración nueva
supabase db push --dry-run     # debe listar SOLO <timestamp>_booknow_mcp_service_phase6_drafts.sql
```

Comprueba también en solo lectura que el punto de partida sigue igual. Esperado: `40250d38275bbf1163e7f99ccd2cc1f5`, `0`, `0`, y ninguna fila con `source` fuera de la lista actual:

```sql
select (select md5(prosrc) from pg_proc where proname = 'confirm_mcp_appointment_draft') as rpc_md5,
       (select count(*) from public.mcp_appointment_drafts) as drafts,
       (select count(*) from public.appointments where source not in ('web','app','phone','walk_in','whatsapp','instagram')) as source_fuera_de_lista;
```

**Detente aquí.** Muestra al usuario la salida de ambos comandos, la consulta anterior, el hash comprobado y el resumen de la validación local. Pregunta: **"¿Aplico esta migración a producción (book-now-hub)?"**

## Paso 4 — Aplicar (solo con un "sí" explícito)

```bash
supabase db push
```

## Paso 5 — Verificar en producción (solo lectura)

Ejecuta con `supabase db query --linked` o en el SQL Editor:

```sql
-- A. Permisos y ajustes esperados: todos_true = true, fallan = null
select bool_and(ok) as todos_true, string_agg(case when not ok then chk end, ', ') as fallan from (values
  ('sel variants price_modifier', has_column_privilege('booknow_mcp_service','public.service_variants','price_modifier','SELECT')),
  ('sel drafts status', has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','status','SELECT')),
  ('ins drafts idempotency_key', has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','idempotency_key','INSERT')),
  ('exec confirm service', has_function_privilege('booknow_mcp_service','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  ('exec confirm service_role', has_function_privilege('service_role','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  ('security definer', (select prosecdef from pg_proc where oid = 'public.confirm_mcp_appointment_draft(uuid,uuid)'::regprocedure)),
  ('search_path vacio', (select proconfig = array['search_path=""'] from pg_proc where oid = 'public.confirm_mcp_appointment_draft(uuid,uuid)'::regprocedure)),
  ('cuerpo nuevo', (select md5(prosrc) = '94939d27211c631208c25a31ecf5575a' from pg_proc where oid = 'public.confirm_mcp_appointment_draft(uuid,uuid)'::regprocedure)),
  ('check source con mcp', (select convalidated and pg_get_constraintdef(oid) like '%''mcp''::text%' from pg_constraint where conrelid = 'public.appointments'::regclass and conname = 'appointments_source_check'))
) v(chk, ok);

-- B. Permisos prohibidos: alguno_true = false, sobran = null
select bool_or(ok) as alguno_true, string_agg(case when ok then chk end, ', ') as sobran from (values
  ('exec confirm public', has_function_privilege('public','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  ('exec confirm anon', has_function_privilege('anon','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  ('exec confirm authenticated', has_function_privilege('authenticated','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  ('upd drafts', has_any_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','UPDATE')),
  ('del drafts', has_table_privilege('booknow_mcp_service','public.mcp_appointment_drafts','DELETE')),
  ('ins drafts status', has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','status','INSERT')),
  ('ins drafts confirmed_appointment_id', has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','confirmed_appointment_id','INSERT')),
  ('sel variants description', has_column_privilege('booknow_mcp_service','public.service_variants','description','SELECT')),
  ('ins appointments', has_any_column_privilege('booknow_mcp_service','public.appointments','INSERT')),
  ('upd appointments', has_any_column_privilege('booknow_mcp_service','public.appointments','UPDATE')),
  ('sel appt internal_notes', has_column_privilege('booknow_mcp_service','public.appointments','internal_notes','SELECT')),
  ('ins appointment_services', has_any_column_privilege('booknow_mcp_service','public.appointment_services','INSERT'))
) v(chk, ok);
```

Luego `supabase migration list`: la migración nueva debe figurar en local y remoto.

## Paso 6 — Commit

Commit en el repo Next.js que incluya **solo** el archivo de migración:

```
fix(db): allow mcp appointment source, harden confirm_mcp_appointment_draft and grant booknow_mcp_service draft permissions
```

No hagas push a GitHub salvo que el usuario lo pida.

## Paso 7 — Revisión del código Next.js (solo reportar, sin cambiar nada)

Busca y reporta con ruta y línea:

1. Dónde se usan los valores de `appointments.source` (etiquetas, filtros, tipos TS o validaciones con `walk_in`, `whatsapp`, etc.) que no contemplen `'mcp'`. Por ejemplo: `grep -rn "walk_in" src`.
2. Si algo usa `result.appointment` de `confirm_mcp_appointment_draft` con campos distintos de los 15 que ahora devuelve.
3. Si algún código llama a `confirm_mcp_appointment_draft` con un cliente que no sea `supabaseAdmin` (`service_role`), porque dejaría de funcionar.
4. Si los tipos generados de Supabase (`database.types.ts` o similar) necesitan regenerarse.

## Qué reportar al terminar

1. Nombre del archivo y hash comprobado.
2. Resumen de la validación local (tabla del Paso 2) y confirmación de base limpia.
3. Salidas de `migration list`, `db push --dry-run` y la consulta previa del Paso 3.
4. Resultados A y B en producción.
5. Hash del commit.
6. Hallazgos del Paso 7.
7. Cualquier desviación, sin mostrar contraseñas ni cadenas de conexión.

## Condiciones de parada

Detente y avisa al usuario si ocurre cualquiera de estos casos:

- El hash del archivo no coincide.
- `db push --dry-run` lista algo más que la migración nueva, o local y remoto no están sincronizados.
- En el Paso 3 el md5 de la función no es `40250d38…`: alguien la cambió después del snapshot y hay que revisar la diferencia antes de reemplazarla.
- Hay filas con `source` fuera de la lista actual: `VALIDATE CONSTRAINT` fallaría.
- La validación local o las verificaciones A o B no dan exactamente lo esperado.
- Aparece algún error de permisos, de columna inexistente o de bloqueo al aplicar.

## Nota para el servicio Go

- Inserta borradores con columnas explícitas, `ON CONFLICT (connection_id, idempotency_key) DO NOTHING` y relee la fila existente si no se insertó nada. El rol no puede escribir `status` ni los campos de confirmación.
- Antes de llamar a la función, comprueba con un `SELECT` que el borrador pertenece al tenant (y a la conexión) resueltos desde el token: la función valida el rol del actor, pero no conoce la conexión MCP.
- Interpreta los errores por prefijo (`DRAFT_NOT_FOUND`, `UNAUTHORIZED`, `INVALID_DRAFT_STATUS`, `DRAFT_EXPIRED`, `SPECIALIST_UNAVAILABLE`) y nunca reenvía el texto del error de Postgres al LLM.

## Nota para migraciones futuras

- Si la función se vuelve a crear con `DROP` + `CREATE` en lugar de `CREATE OR REPLACE`, los privilegios por defecto de Supabase vuelven a dar `EXECUTE` a `anon` y `authenticated`. Hay que repetir el `REVOKE`.
- Pendiente de decidir (no incluido aquí): purgar los borradores antiguos (`mcp_appointment_drafts` guarda `customer_notes` escritas por el LLM) con el mismo cron de la auditoría.
