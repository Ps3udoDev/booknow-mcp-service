# Plan de la Fase 7 — tareas, entregables y criterios de aceptación

Para el agente que implemente. Lee antes `README.md` (reglas) y `CLAUDE.md` (reglas no negociables del proyecto).
Cada tarea termina con su informe en `reportes/7.X-<slug>.md` y con la línea correspondiente marcada en `docs/roadmap.md`.

Dependencias: `7.3` necesita al usuario (migración) y `7.4`, `7.6`, `7.7` necesitan que estén respondidas las decisiones de `04-decisiones-abiertas.md`.

---

## 7.1 — Verificación de la firma de Twilio ✅ hecha (ver `reportes/7.1-verificacion-firma-twilio.md`)

Paquete `internal/integration/twilio`: validación de `X-Twilio-Signature` y configuración asociada.

Entregables:
- `ValidateSignature` según el algoritmo de Twilio: URL completa + pares `clave+valor` del formulario ordenados por clave, HMAC-SHA1 con el Auth Token, base64, comparación en tiempo constante.
- Variables `TWILIO_AUTH_TOKEN` y `TWILIO_WEBHOOK_URL` en `internal/config` (obligatorias solo si el webhook está activo).
- Tests con vector oficial de Twilio, cuerpo alterado, parámetro añadido o quitado, token incorrecto, firma vacía o mal codificada, y fuzzing.

## 7.2 — Endpoint `POST /webhooks/twilio` ✅ hecha (ver `reportes/7.2-endpoint-webhook-twilio.md`)

Entregables:
- Handler en `internal/httpapi` que lee el cuerpo **original** con límite (64 KiB), **verifica la firma antes de parsear** y responde 403 sin detalles si no cuadra.
- Respuesta 200 rápida y vacía (o TwiML vacío) en todos los casos procesables; Twilio reintenta ante 5xx, así que los errores internos no deben devolver 5xx si el evento ya se registró.
- Content-Type distinto de `application/x-www-form-urlencoded` → 415.
- Logs estructurados **sin cuerpo, sin teléfonos y sin firma**: solo `MessageSid`, resultado e identificadores.
- Tests `httptest`: firma válida, firma alterada, sin cabecera, cuerpo mayor que el límite, método equivocado, content-type equivocado.

Criterio de aceptación: sin `TWILIO_AUTH_TOKEN` configurado, la ruta no se registra (o responde 503), nunca procesa sin validar.

## 7.3 — 🔒 Migración: tabla de eventos de Twilio (repo Next.js)

Entregables en este repo (no se aplica aquí):
- `docs/handoff/sql/twilio_webhook_events.sql` con la tabla y los permisos mínimos para `booknow_mcp_service`.
- Script de verificación `docs/handoff/sql/verify_twilio_webhook_events.sql` con el mismo estilo que los de las Fases 5 y 6.
- Documento de instrucciones `docs/handoff/nextjs-migracion-fase7-twilio.md` (copia el formato de `nextjs-migracion-fase6-drafts-mcp.md`: hash, validación local con `ROLLBACK`, `dry-run`, pausa para aprobación, verificación en producción, commit).

Esquema propuesto (ajústalo a lo que salga de D8):

```sql
create table public.twilio_webhook_events (
  message_sid text primary key,
  tenant_id uuid references public.tenants(id) on delete cascade,
  from_phone_masked text,
  to_phone text,
  intent text,                       -- confirm | cancel | unknown
  appointment_id uuid references public.appointments(id) on delete set null,
  result text not null,              -- applied | ignored | duplicate | unresolved
  received_at timestamptz not null default now(),
  processed_at timestamptz
);
```

Permisos para `booknow_mcp_service`: `insert` por columnas, `select (message_sid, result, appointment_id)` y `update (intent, appointment_id, result, processed_at)`. Nada de `delete`. Retención: añadir estos eventos a la purga de `pg_cron` (relacionado con D12 del roadmap).

Criterio de aceptación: el `INSERT ... ON CONFLICT (message_sid) DO NOTHING` sirve de candado de deduplicación, sin necesidad de transacciones largas.

## 7.4 — Resolución del negocio (tenant) del mensaje entrante

Depende de **D8**. Entregables:
- `internal/application/whatsapp` (nombre sugerido) con la resolución elegida: por número destino `To` o por la última notificación enviada a ese teléfono.
- Nunca buscar el cliente por teléfono a nivel global (fallo B2).
- Store en `internal/repository/postgres` con SQL parametrizado y filtrado por negocio.
- Tests de integración con dos negocios y el mismo teléfono en ambos: cada mensaje afecta solo a su negocio.

## 7.5 — Parser de intención

Entregables:
- Normalización: minúsculas, sin tildes ni diacríticos, sin signos de puntuación ni emojis, espacios colapsados.
- Comparación por **palabra completa** contra listas de confirmación y cancelación.
- Si coinciden ambas o ninguna → `unknown` y no se toca nada.
- Soporte de botones de plantilla: si llega `ButtonPayload` o `ButtonText`, manda sobre el texto libre.
- Tests table-driven con los casos del documento de bugs ("buenos días", "casi no llego", "no sé si podré", "SÍ", "👍", "si!!") y fuzzing que compruebe que nunca hay pánico y que `unknown` es el valor por defecto.

## 7.6 — Caso de uso: confirmar o cancelar la cita notificada

Entregables:
- Registro del evento (7.3) **antes** de actuar; si el `MessageSid` ya existía, no se repite el trabajo.
- Cambio de estado de la cita concreta dentro del negocio resuelto, en transacción, solo desde `pending` o `confirmed` y solo si la cita no ha pasado.
- Cancelación: marca `cancelled_at` y un motivo estable.
- Registro en `notifications` con teléfono enmascarado y sin el texto libre del cliente.
- Tests de integración: confirmación, cancelación, reintento duplicado, cita ya cancelada, cita de otro negocio, mensaje sin cita asociada.

⚠️ Este caso de uso **escribe en `appointments`**, que hoy el rol `booknow_mcp_service` no puede tocar. Requiere ampliar permisos (`update (status, cancelled_at, cancellation_reason, updated_at)`) y `insert` en `notifications`: va en la misma migración de 7.3 o en una segunda. Decídelo al preparar 7.3 y documenta el porqué.

## 7.7 — Notificaciones salientes

Entregables:
- `internal/integration/twilio` — envío con `TWILIO_CONTENT_SID` (plantilla aprobada) y variables; timeout, reintentos acotados y errores tipados.
- Endpoint o caso de uso equivalente a `notify` **con autenticación del llamador** (fallo B6). Define cómo: token de servicio, OIDC de Cloud Run o JWT de Supabase (pregúntalo si no está en D8).
- Registro en `notifications` de cada envío (`sent` o `failed`) sin PII innecesaria.
- Tests con servidor HTTP falso: éxito, 4xx de Twilio, 5xx, timeout, plantilla no configurada.

## 7.8 — Email con Resend y `.ics`

Entregables:
- `internal/integration/resend` con timeout y errores tipados.
- Plantillas de email con **todo el contenido dinámico escapado** (fallo B7).
- Generación del `.ics` portada de `migracion/twilio-whatsapp/ics-generator.ts`, con tests de formato (saltos CRLF, escapes, UID estable, zona horaria).
- Test que compruebe que un nombre con `<script>` sale escapado.

## 7.9 — Pruebas de punta a punta y smoke

Entregables:
- Test end-to-end local: firma real calculada con un token de prueba → endpoint → parser → caso de uso → base local, con dos negocios.
- 🔒 Smoke con el sandbox de Twilio y un teléfono de prueba, documentando el resultado sin PII.
- Informe final de la fase y actualización del roadmap.

---

## Fuera de alcance (anotar, no implementar)

- `campaigns-send-route.ts` (fallo B8): decidir si se migra.
- Status callbacks de entrega y lectura de mensajes: solo si se responde que sí en D8.
