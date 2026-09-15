# Instrucciones para el agente del repo Next.js — migración del rol `booknow_mcp_service`

> Copia este documento completo como prompt al agente que trabaja en el repo **Next.js de BookNow** (el dueño de las migraciones de Supabase).
> Fecha: 2026-09-15 · Proyecto Supabase: `book-now-hub` (`rrnysepngbycvuciodoj`)

---

## Contexto

Estamos migrando el MCP, los webhooks de Twilio y parte de la API a un servicio Go dedicado (`booknow-mcp-service`) en Cloud Run. Ese servicio se conecta a Postgres por el **Session Pooler** con un rol propio de mínimo privilegio, `booknow_mcp_service`, en lugar de `postgres`.

El rol **ya existe en producción**. Se creó a mano desde el SQL Editor, con contraseña, y ya está validado con un smoke test de solo lectura. Lo que falta es que el esquema versionado lo refleje. Como este repo es el único que gestiona las migraciones de Supabase, la migración vive aquí.

Estado actual en producción:

- Rol `booknow_mcp_service` con `LOGIN`, `BYPASSRLS` y `NOINHERIT`.
- `USAGE` sobre el esquema `public`.
- Solo `SELECT` sobre `public.mcp_connections`, `public.tenants`, `public.tenant_users`, `public.tenant_modules` y `public.modules`.

**Por qué `BYPASSRLS`:** las políticas RLS dependen de `auth.uid()`, que es nulo para un servicio, así que sin ese atributo el rol no vería filas. El límite real lo ponen los `GRANT` (solo esas tablas y solo lectura), y el servicio Go filtra siempre por el tenant resuelto desde el token.

## Tarea

1. Crear una migración nueva, idempotente, que declare el rol y sus permisos.
2. Validarla en local.
3. Aplicarla a producción **solo tras confirmación explícita del usuario**.

## Reglas obligatorias

- **Nunca incluyas una contraseña** en la migración, ni `ALTER ROLE ... PASSWORD`, ni la pidas o la muestres. La contraseña de producción ya está puesta y la migración no debe tocarla.
- La migración debe ser **idempotente**: en producción el rol ya existe y en local o en preview no.
- **No modifiques** políticas RLS, tablas, funciones ni otros roles.
- **No concedas** nada a `anon`, `authenticated`, `public` ni `service_role` en esta migración.
- **No ejecutes** `supabase db push` sin mostrar antes el resultado de `--dry-run` y recibir un "sí" explícito del usuario.
- Si `supabase migration list` muestra que local y remoto están desincronizados, **detente y avisa**. No uses `migration repair` sin autorización.

## Paso 1 — Crear la migración

Crea el archivo con el nombre que genera la CLI:

```bash
supabase migration new booknow_mcp_service_role
```

Contenido exacto de `supabase/migrations/<timestamp>_booknow_mcp_service_role.sql`:

```sql
-- Least-privilege database role for booknow-mcp-service (Go, Cloud Run).
-- The password is managed outside migrations (set once in production, never versioned).
-- BYPASSRLS: RLS policies rely on auth.uid(), which is null for a service connection;
-- access is bounded by the explicit GRANTs below and the service always filters by the
-- tenant resolved from the caller's token and active mcp_connections row.

do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'booknow_mcp_service') then
    create role booknow_mcp_service with login bypassrls noinherit;
  end if;
end
$$;

-- Enforce attributes on an existing role without touching its password.
alter role booknow_mcp_service with login bypassrls noinherit;

grant usage on schema public to booknow_mcp_service;

-- Read-only access required to authorize MCP requests.
grant select on table
  public.mcp_connections,
  public.tenants,
  public.tenant_users,
  public.tenant_modules,
  public.modules
to booknow_mcp_service;
```

> Este SQL ya se validó en una base local de Supabase (Postgres 17) ejecutándolo como `postgres`, que no es superusuario: se puede ejecutar dos veces seguidas sin error y deja exactamente los 5 privilegios `SELECT`.

## Paso 2 — Validar en local

```bash
supabase start          # si no está levantado
supabase db reset       # reaplica todas las migraciones, incluida la nueva
```

Ejecuta estas consultas contra la base local (`postgresql://postgres:postgres@127.0.0.1:54322/postgres`) y compara con lo esperado:

```sql
-- 1. Atributos del rol. Esperado: rolcanlogin = true, rolbypassrls = true, rolinherit = false
select rolname, rolcanlogin, rolbypassrls, rolinherit
from pg_roles
where rolname = 'booknow_mcp_service';

-- 2. Privilegios sobre tablas de public. Esperado: exactamente 5 filas, todas SELECT:
--    mcp_connections, modules, tenant_modules, tenant_users, tenants
select c.relname as tabla, a.privilege_type as privilegio
from pg_class c
cross join lateral aclexplode(c.relacl) a
where c.relnamespace = 'public'::regnamespace
  and a.grantee = 'booknow_mcp_service'::regrole
order by 1, 2;

-- 3. Controles negativos. Esperado: usage_public = true, select_customers = false, update_connections = false
select has_schema_privilege('booknow_mcp_service', 'public', 'USAGE') as usage_public,
       has_table_privilege('booknow_mcp_service', 'public.customers', 'SELECT') as select_customers,
       has_table_privilege('booknow_mcp_service', 'public.mcp_connections', 'UPDATE') as update_connections;
```

Comprueba además que la migración es idempotente aplicándola dos veces seguidas en local, sin errores:

```bash
psql "postgresql://postgres:postgres@127.0.0.1:54322/postgres" -v ON_ERROR_STOP=1 \
  -f supabase/migrations/<timestamp>_booknow_mcp_service_role.sql
```

Si la app tiene checks (typecheck, lint, tests), ejecútalos. Esta migración no debería afectarlos.

## Paso 3 — Preparar la aplicación en producción

```bash
supabase migration list        # local y remoto deben coincidir, salvo la migración nueva
supabase db push --dry-run     # debe listar SOLO la migración nueva
```

**Detente aquí.** Muestra al usuario:

- la salida de `migration list`;
- la salida de `db push --dry-run`;
- el contenido de la migración.

Y pregunta: **"¿Aplico esta migración a producción (book-now-hub)?"**

## Paso 4 — Aplicar (solo con un "sí" explícito)

```bash
supabase db push
```

Resultado esperado en producción: el bloque `do` no hace nada (el rol ya existe), `alter role` reafirma los mismos atributos y los `grant` no cambian nada porque ya existen. La contraseña **no cambia**, así que el servicio Go sigue conectando igual.

## Paso 5 — Verificar en producción

Ejecuta de nuevo las tres consultas del Paso 2 contra producción (SQL Editor o `psql` con la conexión enlazada) y confirma el mismo resultado esperado.

Después:

```bash
supabase migration list        # la migración nueva debe aparecer aplicada en remoto
```

## Paso 6 — Commit

Commit en el repo Next.js con solo el archivo de migración, por ejemplo:

```
chore(db): declare booknow_mcp_service least-privilege role
```

## Qué reportar al terminar

1. Nombre del archivo de migración y hash del commit.
2. Salida de las tres consultas de verificación en local y en producción.
3. Confirmación de que `supabase migration list` quedó sincronizado.
4. Cualquier desviación o error, sin mostrar contraseñas ni cadenas de conexión.

## Condiciones de parada

Detente y avisa al usuario si ocurre cualquiera de estos casos:

- `db push --dry-run` lista migraciones además de la nueva.
- Local y remoto no están sincronizados.
- `create role` o `alter role ... bypassrls` fallan por permisos.
- La verificación muestra privilegios distintos a los 5 `SELECT` esperados.
- Algo en el flujo pide o expone una contraseña.

## Nota para migraciones futuras

Cuando el servicio Go necesite más permisos, cada uno llegará como una migración nueva e igual de explícita. Por ejemplo: `INSERT` en `mcp_tool_calls` para auditoría, `SELECT`/`INSERT` en `mcp_appointment_drafts`, o `EXECUTE` sobre `confirm_mcp_appointment_draft`. **Nunca** se usarán `GRANT ALL` ni `ALTER DEFAULT PRIVILEGES` para este rol.
