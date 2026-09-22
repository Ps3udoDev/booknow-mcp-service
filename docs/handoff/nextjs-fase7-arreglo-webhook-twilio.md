# Instrucciones para el agente del repo Next.js — Fase 7: cerrar B1, B2, B3 y comprobar B6

> Copia este documento completo como prompt al agente que trabaja en el repo **Next.js de BookNow**.
> Fecha: 2026-09-16 · Decisión **D13** del usuario. Independiente de la migración `booknow_mcp_service_phase7_twilio` (se puede hacer antes o después).

---

## Contexto

El webhook `src/app/api/webhooks/twilio/route.ts` tiene tres fallos de seguridad. El servicio Go lo reemplazará, pero el corte no llega hasta la Fase 9. El usuario decidió cerrarlos ya en Next.js para no dejar el agujero abierto en el intervalo:

- **B1 🔴** No verifica `X-Twilio-Signature`. Cualquiera que conozca la URL puede confirmar o cancelar citas ajenas.
- **B2 🔴** Busca al cliente por los últimos 10 dígitos del teléfono **en todos los negocios** (`like '%<digitos>'` sin `tenant_id`).
- **B3 🟠** Cambia "la cita más reciente" del cliente, no la que se le notificó.

B4 (palabras con `includes`), B5 (reintentos duplicados) y B7 (HTML sin escapar) **no se tocan aquí**: los cubre Go.

### Premisa: el cambio es inocuo

- ✅ Comprobado desde el repo Go en producción: **0 filas** en `notifications` con `channel = 'whatsapp'` en los últimos 90 días. El flujo no se está usando.
- ⏳ El usuario debe confirmar si la URL del webhook está configurada en la consola de Twilio. Si **no** lo está, B1 ya está cerrado de hecho y este arreglo es preventivo.

Si al revisar el código encuentras algo que contradiga la premisa (por ejemplo, otro sitio que envíe WhatsApp y no registre en `notifications`), **detente y avisa** antes de cambiar nada.

## Parte 1 — Arreglar el webhook (B1 + B2 + B3)

### B1: verificar la firma antes de leer nada

- Lee el formulario y valida con `twilio.validateRequest(authToken, signature, url, params)` **antes** de usar ningún campo.
- La `url` sale de una variable de entorno (`TWILIO_WEBHOOK_URL`), **exactamente** la configurada en Twilio. No la construyas con `request.url` ni con las cabeceras `Host` / `X-Forwarded-*`: detrás de Vercel no coinciden y además se pueden manipular.
- Firma ausente o inválida → `403` con cuerpo genérico. Sin `TWILIO_AUTH_TOKEN` o sin `TWILIO_WEBHOOK_URL` → `503`, nunca procesar sin validar.
- Comprueba también que `AccountSid` sea igual a `TWILIO_ACCOUNT_SID`; si no, `403`.

### B2 + B3: resolver negocio y cita por la notificación enviada

Sustituye la búsqueda por teléfono y "la cita más reciente" por esta regla (decisión D8.a, la misma que usará Go):

1. Normaliza `From`: quita `whatsapp:` y deja E.164 (`+` y dígitos).
2. Busca las notificaciones de WhatsApp **enviadas a ese teléfono en las últimas 48 h**:

   ```sql
   select n.tenant_id, n.reference_id as appointment_id, n.recipient_id as customer_id
   from public.notifications n
   join public.customers c on c.id = n.recipient_id and c.tenant_id = n.tenant_id
   where n.channel = 'whatsapp'
     and n.recipient_type = 'customer'
     and n.reference_type = 'appointment'
     and n.notification_type <> 'appointment_response'
     and n.status in ('sent', 'delivered', 'read')
     and n.created_at > now() - interval '48 hours'
     and c.phone_country_code || regexp_replace(c.phone, '\D', '', 'g') = $1
   order by n.created_at desc;
   ```

3. **Si los resultados son de más de un `tenant_id` → no hagas nada** (responde 200). Nunca "gana la más reciente" entre negocios.
4. Si no hay resultados → no hagas nada (200).
5. Actúa **solo** sobre `appointment_id` de la fila más reciente, con `update ... where id = $appointment and tenant_id = $tenant and customer_id = $customer`, y solo si la cita es futura y está en `pending` o `confirmed`.
6. Nunca compares "los últimos 10 dígitos".

Con Supabase JS no hay join cómodo con concatenación. Vale hacerlo en dos pasos dentro del mismo negocio: primero `customers` por `phone_country_code` + `phone` exactos (normalizados), después `notifications` filtrando por esos `recipient_id` y por `tenant_id`. La regla del punto 3 no cambia.

### Registro en `notifications`

Si registras la respuesta, **no incluyas el teléfono ni el texto del cliente** en `message`: hoy se guarda `Cliente respondio "${body}" desde ${from}`. Usa un texto fijo ("El cliente confirmó la cita por WhatsApp.") y `notification_type = 'appointment_response'`, que es lo que filtra la consulta de arriba para no tomarse a sí misma como notificación enviada.

### Tests mínimos

- Firma válida → procesa; firma alterada, ausente o de otra cuenta → 403.
- Mismo teléfono en dos negocios con notificaciones en la ventana → no toca ninguna cita.
- Cliente con dos citas → cambia la notificada, no la de `scheduled_at` más alto.
- Notificación de hace más de 48 h → no hace nada.

## Parte 2 — B6: `POST /api/appointments/notify`

En la copia que tiene el repo Go (`src/app/api/appointments/notify/route.ts`), la ruta recibe `{ appointmentId }` y envía emails y WhatsApp **sin autenticar a quien llama**, usando `SUPABASE_SERVICE_ROLE_KEY`.

1. **Comprueba y reporta** si el middleware de Next.js cubre esa ruta (con ruta y línea). Prueba también, sin sesión, contra un entorno de preview: `curl -X POST <preview>/api/appointments/notify -H 'content-type: application/json' -d '{"appointmentId":"00000000-0000-0000-0000-000000000000"}'`. Un `404 Cita no encontrada` significa que **no** está protegida. **No** lo pruebes contra producción con un id real.
2. Si no está protegida, **detente y muestra al usuario** qué propones antes de cambiarlo. Lo esperable: exigir sesión y comprobar que el usuario pertenece al `tenant_id` de la cita (vía `tenant_users` o el helper que ya use la app). Busca antes todos los sitios que llaman a la ruta, para no romper el flujo de reserva.

## Reglas

- Sin migraciones en este encargo (no hacen falta).
- No cambies el envío de avisos (`notify`) más allá de la autenticación: se migra a Go en la Fase 9.
- Nada de secretos en el código, en logs ni en el commit.
- Commit separado por parte; sin push salvo que el usuario lo pida.

## Qué reportar al terminar

1. Si la URL del webhook está configurada en Twilio (lo confirma el usuario).
2. Diff resumido de la Parte 1 y resultado de sus tests.
3. Hallazgo de B6: protegida o no, con evidencia, y la propuesta si hace falta.
