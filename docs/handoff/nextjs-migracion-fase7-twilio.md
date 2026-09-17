# Instrucciones para el agente del repo Next.js — Fase 7: respuestas por WhatsApp y avisos salientes

> Copia este documento completo como prompt al agente que trabaja en el repo **Next.js de BookNow** (dueño de las migraciones de Supabase).
> Fecha: 2026-09-16 · Proyecto Supabase: `book-now-hub` (`rrnysepngbycvuciodoj`)
> Precedentes en producción: rol `booknow_mcp_service`, permisos de la Fase 5 (lecturas, auditoría, purga) y de la Fase 6 (borradores y `confirm_mcp_appointment_draft`).

---

## Contexto

El servicio Go `booknow-mcp-service` va a sustituir al webhook de Twilio (`/api/webhooks/twilio`) y a `/api/appointments/notify`. Ya tiene el endpoint `POST /webhooks/twilio` con verificación de firma. Para procesar las respuestas del cliente ("Confirmar" / "Cancelar") y enviar los avisos, el rol `booknow_mcp_service` necesita objetos y permisos nuevos.

Decisiones ya tomadas por el usuario que esta migración implementa:

- **D8.a**: el negocio y la cita se resuelven por **la última notificación de WhatsApp enviada a ese teléfono** en una ventana (48 h por defecto), nunca buscando al cliente por teléfono en todos los negocios. Si hay más de un negocio en la ventana, no se toca nada.
- **D10**: los avisos salientes (WhatsApp y email) pasan a Go. El corte real espera al despliegue en Cloud Run (Fase 9); **hasta entonces Next.js sigue enviando igual que hoy**.
- **D12**: no hay status callbacks de Twilio en esta fase.

Igual que en la Fase 6, **el servicio no actualiza `appointments` directamente**. Una función `SECURITY DEFINER` aplica la respuesta en una sola transacción.

## Qué hace la migración

| # | Cambio | Motivo |
|---|---|---|
| 1 | Tabla `public.twilio_webhook_events` (PK `message_sid`), con RLS activado, sin políticas y sin permisos para `anon` ni `authenticated`. Guarda el teléfono **enmascarado**, la intención, el resultado y un código de motivo. No guarda el texto del mensaje. | Deduplicar los reintentos de Twilio (fallo B5) y dejar traza sin PII. |
| 2 | Función `public.apply_twilio_whatsapp_reply(text, text, text, text, uuid, uuid, uuid, interval)`, `SECURITY DEFINER`, `search_path = ''`. `EXECUTE` **solo** para `booknow_mcp_service` (revocado a `public`, `anon`, `authenticated` y `service_role`). | Única vía de escritura desde una respuesta a una cita. |
| 3 | `SELECT` por columnas en `notifications` (sin `title` ni `message`) e `INSERT` por columnas (sin `id`, `delivered_at` ni `read_at`). Sin `UPDATE` ni `DELETE`. | Resolver el negocio (D8.a) y registrar los avisos que envíe Go. |
| 4 | `SELECT` de columnas sueltas: `customers (email, notify_email, notify_whatsapp, preferred_language)`, `profiles (email)`, `branches (address, city, phone, email)` y `workstations (id, tenant_id, branch_id, name, is_active)`. | Contenido del WhatsApp, del email y del `.ics`. Direcciones de clientes, documentos, notas y comisiones siguen fuera. |
| 5 | Índice parcial `idx_notifications_whatsapp_appointment_recent` en `notifications (created_at desc) where channel = 'whatsapp' and reference_type = 'appointment'`. | La consulta "WhatsApp enviados en las últimas horas". La tabla tiene 39 filas: el índice se crea al instante. |
| 6 | Función `purge_twilio_webhook_events()` (borra eventos de más de 30 días) y job de `pg_cron` diario a las 03:27 UTC. | Retención. |

### Qué hace `apply_twilio_whatsapp_reply`, en una sola transacción

1. Valida la intención (`confirm | cancel | unknown`), la ventana (1 h – 7 días) y que tenant, cliente y cita vengan juntos o ninguno.
2. Inserta el evento con `ON CONFLICT (message_sid) DO NOTHING`. Si ya existía, devuelve el resultado guardado con `duplicate = true` y no hace nada más.
3. Solo actúa si **esa cita se notificó por WhatsApp a ese cliente, en ese negocio, dentro de la ventana** (excluye las propias filas `appointment_response`).
4. Bloquea la cita (`FOR UPDATE`) y solo cambia citas futuras en `pending` o `confirmed`:
   - confirmar: `status = confirmed`, `confirmed_at = now()`;
   - cancelar: `status = cancelled`, `cancelled_at = now()`, `cancellation_reason = 'Cancelada por el cliente por WhatsApp'`.
5. Si cambió algo, inserta en `notifications` una fila `appointment_response` con texto fijo, sin teléfono ni texto del cliente, `channel = whatsapp`, `status = delivered`.
6. Devuelve `{duplicate, result, result_detail, appointment_status}`. `result` es `applied | already_in_state | not_applicable | ignored | unresolved`.

**Qué ve Next.js:** filas nuevas en `notifications` con `notification_type = 'appointment_response'` y `status = 'delivered'`, y citas que pasan a `confirmed` o `cancelled` sin que las toque la app. Hoy nada escribe esas filas en producción: hay 0 notificaciones de WhatsApp en los últimos 90 días y 0 `appointment_response`.

## Validación hecha desde el repo Go

- **Producción, en solo lectura:** no existen la tabla, las funciones, el job ni el índice. `notifications.status` admite `delivered`. El rol no tiene hoy ningún permiso en `notifications` ni `UPDATE` en `appointments`. Hay 0 teléfonos de clientes con caracteres no numéricos.
- **Base local, en transacción con `ROLLBACK`:** la migración se aplica dos veces sin error. Pasan los **83 checks**:
  - 16 permisos y ajustes esperados;
  - 26 permisos prohibidos;
  - 26 de comportamiento como el rol;
  - 15 operaciones denegadas y la purga.
- **Casos de comportamiento cubiertos:**
  - confirmar y cancelar;
  - mismo `MessageSid` repetido y respuesta repetida;
  - cita no notificada, pasada, `completed` o notificada fuera de la ventana;
  - **mismo teléfono en dos negocios** (la cita del otro negocio no se toca);
  - intención `unknown`, mensaje sin resolver, parámetros inválidos;
  - teléfono sin enmascarar rechazado por el `CHECK`.
- **Concurrencia real con dos sesiones:**
  - El mismo `MessageSid` a la vez: la segunda sesión espera a la primera y no duplica nada.
  - Un "sí" y un "no" a la vez sobre la misma cita: se aplican en orden, sin estados intermedios.
  - La base local se restauró con `supabase db reset`.

## Archivos fuente (misma máquina)

- Migración: `C:\Users\DELL\Desktop\code\experimental\MCP\docs\handoff\sql\booknow_mcp_service_phase7_twilio.sql`
- Script de verificación local: `C:\Users\DELL\Desktop\code\experimental\MCP\docs\handoff\sql\verify_booknow_mcp_service_phase7.sql`

SHA-256 de la migración, calculado sin retornos de carro:
`d9a88c5f9a7deafbe398b8597793e60bdec3692419db9d40348e5d68a6df19b7`

## Reglas obligatorias

- **Copia el archivo de migración tal cual**, sin editarlo. Si crees que algo debe cambiar, **detente y pregunta**.
- No añadas permisos para `anon`, `authenticated`, `public` ni `service_role`, ni `ALTER ROLE`.
- No modifiques políticas RLS, otras tablas ni otras funciones.
- No cambies código TypeScript en este encargo (ver Paso 7: solo se reporta).
- No ejecutes `supabase db push` sin mostrar antes `migration list` y `db push --dry-run`, y sin un "sí" explícito del usuario.
- No uses `migration repair` sin autorización.
- No llames a `apply_twilio_whatsapp_reply` en producción ni crees datos de prueba allí.

## Paso 1 — Crear la migración

```bash
supabase migration new booknow_mcp_service_phase7_twilio
cp "C:/Users/DELL/Desktop/code/experimental/MCP/docs/handoff/sql/booknow_mcp_service_phase7_twilio.sql" \
   supabase/migrations/<timestamp>_booknow_mcp_service_phase7_twilio.sql
tr -d '\r' < supabase/migrations/<timestamp>_booknow_mcp_service_phase7_twilio.sql | sha256sum
```

El hash debe coincidir con el de arriba; si no, **detente**. Si Git convierte el archivo a CRLF, pásalo a LF antes del commit (el cuerpo de las funciones se guarda tal cual y el md5 del Paso 5 cambiaría).

## Paso 2 — Validar en local (transacción con ROLLBACK)

Base local del proyecto MCP (contenedor `supabase_db_MCP`), rutas `C:/...` y `MSYS_NO_PATHCONV=1`:

```bash
MSYS_NO_PATHCONV=1 docker cp supabase/migrations/<timestamp>_booknow_mcp_service_phase7_twilio.sql supabase_db_MCP:/tmp/mig.sql
MSYS_NO_PATHCONV=1 docker cp "C:/Users/DELL/Desktop/code/experimental/MCP/docs/handoff/sql/verify_booknow_mcp_service_phase7.sql" supabase_db_MCP:/tmp/ver.sql
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP psql -U postgres -q -P pager=off -v migration=/tmp/mig.sql -f /tmp/ver.sql
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP rm -f /tmp/mig.sql /tmp/ver.sql
```

Resultado esperado:

- Algunos `NOTICE ... already exists, skipping` (segunda aplicación) y uno sobre la pertenencia de `postgres` al rol: inofensivos.
- Tabla por sección:

  | seccion | checks | fallan |
  |---|---|---|
  | 2 | 16 | 0 |
  | 3 | 26 | 0 |
  | 4 | 26 | 0 |
  | 5 | 15 | 0 |

- "checks que fallan" vacío (`0 rows`) y ningún `ERROR`.

Confirma que la base local quedó igual. Debe devolver `t|0|0`:

```bash
MSYS_NO_PATHCONV=1 docker exec supabase_db_MCP psql -U postgres -tAc "select to_regclass('public.twilio_webhook_events') is null, (select count(*) from public.tenants where slug like 'phase7-%'), (select count(*) from cron.job where jobname = 'purge-twilio-webhook-events')"
```

## Paso 3 — Preparar producción

```bash
supabase migration list        # todo sincronizado salvo la migración nueva
supabase db push --dry-run     # debe listar SOLO <timestamp>_booknow_mcp_service_phase7_twilio.sql
```

Punto de partida en solo lectura. Esperado: `true | 0 | 0 | true | 0`:

```sql
select to_regclass('public.twilio_webhook_events') is null as sin_tabla,
       (select count(*) from pg_proc where proname in ('apply_twilio_whatsapp_reply', 'purge_twilio_webhook_events')) as funciones,
       (select count(*) from cron.job where jobname = 'purge-twilio-webhook-events') as cron,
       to_regclass('public.idx_notifications_whatsapp_appointment_recent') is null as sin_indice,
       (select count(*) from public.notifications where notification_type = 'appointment_response') as respuestas;
```

**Detente aquí.** Muestra al usuario la salida de ambos comandos, la consulta anterior, el hash comprobado y el resumen de la validación local. Pregunta: **"¿Aplico esta migración a producción (book-now-hub)?"**

## Paso 4 — Aplicar (solo con un "sí" explícito)

```bash
supabase db push
```

## Paso 5 — Verificar en producción (solo lectura)

```sql
-- A. Esperados: todos_true = true, fallan = null
select bool_and(ok) as todos_true, string_agg(case when not ok then chk end, ', ') as fallan from (values
  ('exec apply service', has_function_privilege('booknow_mcp_service','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  ('apply definer + search_path', (select prosecdef and proconfig = array['search_path=""'] from pg_proc where oid = 'public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)'::regprocedure)),
  ('apply cuerpo', (select md5(prosrc) = '10a1fc857bfb7adddd16bf6b6a1e9a8e' from pg_proc where oid = 'public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)'::regprocedure)),
  ('purge cuerpo', (select md5(prosrc) = 'c651f94c98115ea68ca9c73eda160022' from pg_proc where oid = 'public.purge_twilio_webhook_events()'::regprocedure)),
  ('rls eventos', (select relrowsecurity from pg_class where oid = 'public.twilio_webhook_events'::regclass)),
  ('cron', exists (select 1 from cron.job where jobname = 'purge-twilio-webhook-events' and schedule = '27 3 * * *')),
  ('indice', to_regclass('public.idx_notifications_whatsapp_appointment_recent') is not null),
  ('sel notifications reference_id', has_column_privilege('booknow_mcp_service','public.notifications','reference_id','SELECT')),
  ('ins notifications status', has_column_privilege('booknow_mcp_service','public.notifications','status','INSERT')),
  ('sel customers email', has_column_privilege('booknow_mcp_service','public.customers','email','SELECT')),
  ('sel profiles email', has_column_privilege('booknow_mcp_service','public.profiles','email','SELECT')),
  ('sel branches address', has_column_privilege('booknow_mcp_service','public.branches','address','SELECT')),
  ('sel workstations name', has_column_privilege('booknow_mcp_service','public.workstations','name','SELECT'))
) v(chk, ok);

-- B. Prohibidos: alguno_true = false, sobran = null
select bool_or(ok) as alguno_true, string_agg(case when ok then chk end, ', ') as sobran from (values
  ('exec apply anon', has_function_privilege('anon','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  ('exec apply authenticated', has_function_privilege('authenticated','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  ('exec apply service_role', has_function_privilege('service_role','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  ('exec purge service', has_function_privilege('booknow_mcp_service','public.purge_twilio_webhook_events()','EXECUTE')),
  ('eventos anon', has_table_privilege('anon','public.twilio_webhook_events','SELECT')),
  ('eventos authenticated', has_table_privilege('authenticated','public.twilio_webhook_events','SELECT')),
  ('eventos service', has_any_column_privilege('booknow_mcp_service','public.twilio_webhook_events','SELECT')),
  ('sel notifications message', has_column_privilege('booknow_mcp_service','public.notifications','message','SELECT')),
  ('upd notifications', has_any_column_privilege('booknow_mcp_service','public.notifications','UPDATE')),
  ('upd appointments', has_any_column_privilege('booknow_mcp_service','public.appointments','UPDATE')),
  ('sel customers address', has_column_privilege('booknow_mcp_service','public.customers','address','SELECT')),
  ('sel profiles phone', has_column_privilege('booknow_mcp_service','public.profiles','phone','SELECT'))
) v(chk, ok);
```

Luego `supabase migration list`: la migración nueva debe figurar en local y remoto.

## Paso 6 — Commit

Commit en el repo Next.js que incluya **solo** el archivo de migración:

```
feat(db): twilio webhook events, apply_twilio_whatsapp_reply and booknow_mcp_service notification grants
```

No hagas push a GitHub salvo que el usuario lo pida.

## Paso 7 — Revisión del código Next.js (solo reportar, sin cambiar nada)

Busca y reporta con ruta y línea:

1. Código que lea `notifications` y pueda confundirse con filas `notification_type = 'appointment_response'` y `status = 'delivered'` (listados, contadores de no leídas, badges).
2. Si los tipos generados de Supabase (`database.types.ts` o similar) necesitan regenerarse por la tabla y la función nuevas.
3. Si algo de la app filtra citas por `cancellation_reason` o espera un valor concreto.

## Qué reportar al terminar

1. Nombre del archivo y hash comprobado.
2. Salida de la validación local (tabla por sección y la comprobación `t|0|0`).
3. Salida de `migration list` y `db push --dry-run`.
4. Resultado de las consultas A y B del Paso 5.
5. Hallazgos del Paso 7.
