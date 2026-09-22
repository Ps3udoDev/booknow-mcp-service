# Plan Twilio WhatsApp — booknow-mcp-service

Plan separado del roadmap del MCP el **2026-09-21**. Antes era la Fase 7 de `docs/roadmap.md`.
Se aplazó porque el equipo va a definir en una reunión los pagos y la contratación del número de WhatsApp del SaaS,
y eso puede cambiar la resolución de tenant (D8) y la configuración de Twilio.

**Leyenda:** igual que `docs/roadmap.md` (`[x]` hecho · `[ ]` pendiente · 🔒 acción manual · ⚠️ decisión abierta).

**Detalle de trabajo:** `docs/handoff/fase7/` (tareas 7.1–7.9 con criterios de aceptación, configuración de Twilio/Meta/Resend,
decisiones y un informe por tarea en `reportes/`). Sus reglas de trabajo siguen vigentes.

## Estado al aplazar

- ✅ **7.1** firma `X-Twilio-Signature` y **7.2** endpoint `POST /webhooks/twilio`, en `main`. La ruta solo se sirve si
  `TWILIO_AUTH_TOKEN` + `TWILIO_WEBHOOK_URL` + `TWILIO_ACCOUNT_SID` están configuradas: el MCP se puede desplegar sin ellas.
- ⏸️ **7.3** SQL listo y validado en local (`docs/handoff/sql/booknow_mcp_service_phase7_twilio.sql`).
  **No aplicarlo en Next.js hasta después de la reunión**: si cambia el modelo de número (D8.b), puede cambiar el esquema.
- ⬜ 7.4–7.9 sin empezar. 7.5 (parser de intención) no depende de nada externo.

## Para retomar después de la reunión

1. 🔒 Anotar lo decidido sobre el número y los pagos en `docs/handoff/fase7/04-decisiones-abiertas.md` (D8.b) y aquí.
   - ¿Un número compartido del SaaS o uno por tenant? Si es por tenant desde el principio, la resolución pasa a la opción C
     (`To` → tenant) y hace falta la tabla número → tenant en la migración de 7.3.
   - ¿Quién paga los mensajes (el SaaS o cada negocio) y si eso exige registrar consumo por tenant?
   - Cuenta de Twilio de pago, verificación del negocio en Meta y aprobación de plantillas (`01-configuracion-twilio-whatsapp.md`).
2. Revisar el SQL de 7.3 con esas decisiones y volver a validarlo en local (`verify_booknow_mcp_service_phase7.sql`).
3. 🔒 Aplicarlo con el agente de Next.js (`docs/handoff/nextjs-migracion-fase7-twilio.md`) y seguir el orden de `docs/handoff/fase7/README.md`.

## Checklist

> ⚠️ **Hallazgos en el código TS actual** (`migracion/twilio-whatsapp/`), relevantes también para producción hoy:
> 1. `twilio-webhook-route.ts` **no verifica `X-Twilio-Signature`**: cualquiera puede enviar un POST con un `From` falso y confirmar o cancelar citas.
> 2. Busca el cliente por los últimos 10 dígitos del teléfono **en todos los tenants** y modifica la cita más reciente de cualquiera de ellos.
> 3. Detecta la intención con `includes` sobre subcadenas (`"no"` coincide con `"buenos"`, `"si"` con `"casi"`).
> 4. No deduplica reintentos de Twilio.
> 5. En el archivo copiado, `notify-route.ts` no autentica al llamador. Hay que confirmar si el middleware de Next.js lo protege. Además, interpola datos sin escapar en el HTML de los emails.

> **Traspaso:** `docs/handoff/fase7/` tiene el plan por tareas (7.1–7.9), lo que debe configurar el usuario en Twilio/WhatsApp/Resend, las decisiones abiertas y un informe por tarea en `docs/handoff/fase7/reportes/`.

- [x] **7.1** `internal/integration/twilio`: firma HMAC-SHA1 sobre la URL configurada y los campos del formulario, comparación en tiempo constante, `TWILIO_AUTH_TOKEN` + `TWILIO_WEBHOOK_URL` validadas juntas y sin filtrarse en errores. Fuzzing (1,7 M) y 6 mutaciones detectadas. Informe: `docs/handoff/fase7/reportes/7.1-verificacion-firma-twilio.md`.
- [x] **7.2** Endpoint `POST /webhooks/twilio`: content type, cuerpo ≤ 64 KiB, firma sobre el cuerpo original y `AccountSid` (nueva `TWILIO_ACCOUNT_SID` obligatoria) antes de leer campos; 403 sin detalles; TwiML vacío; 500 si falla el procesador para que Twilio reintente; logs solo con `MessageSid`. 6 mutaciones detectadas. Informe: `docs/handoff/fase7/reportes/7.2-endpoint-webhook-twilio.md`.
- [ ] ⚠️ Resolución de tenant en el webhook (p. ej. por número destino `To` o por la cita notificada), nunca por teléfono global.
- [ ] ⚠️ **7.3** Deduplicación persistente por `MessageSid` y aplicación de respuestas: SQL listo y validado en local (83 checks + concurrencia): tabla `twilio_webhook_events`, función `apply_twilio_whatsapp_reply` (`SECURITY DEFINER`, sin `UPDATE` directo en `appointments`) y permisos de `notifications` y datos de avisos. **Pendiente de aplicar por el repo Next.js** (`docs/handoff/nextjs-migracion-fase7-twilio.md`). Informe: `docs/handoff/fase7/reportes/7.3-migracion-twilio.md`.
- [ ] Parser de intención por palabra completa y normalizada (tildes, mayúsculas), con tests table-driven y fuzzing.
- [ ] Caso de uso: confirmar o cancelar la cita del cliente en el tenant resuelto y registrar en `notifications`.
- [ ] Respuesta 200 rápida; trabajo costoso fuera del request si hace falta.
- [ ] Notificaciones salientes (`notify`): autenticación del llamador, WhatsApp con `TWILIO_CONTENT_SID` o fallback, email vía Resend (`internal/integration/resend`) con HTML escapado y `.ics`.
- [ ] Tests: firma válida, firma alterada, reintento duplicado, cliente en dos tenants, intención ambigua, timeout del proveedor.
- [ ] ⚠️ `campaigns-send-route.ts` es un stub: decidir si se migra.

## Despliegue y corte (antes en las Fases 9 y 10 del roadmap)

- [ ] 🔒 Secretos en Secret Manager: `TWILIO_AUTH_TOKEN`, `TWILIO_ACCOUNT_SID`, `RESEND_API_KEY`.
- [ ] 🔒 Cambiar el webhook de Twilio a la URL de Go en Cloud Run (sin cambiar a la vez URL, proveedor y semántica).
- [ ] Retirar de Next.js `src/app/api/webhooks/twilio/` y las notificaciones que pasen a Go.
- [ ] Status callbacks de Twilio (D17), a reevaluar tras el despliegue.

## Decisiones de Twilio (movidas desde `docs/roadmap.md`)

| # | Decisión | Propuesta actual | Estado |
|---|---|---|---|
| D8 | Deduplicación y resolución de tenant en Twilio | Tabla de eventos procesados (`message_sid` único) + tenant resuelto **por la notificación enviada** en una ventana de 48 h; si hay más de un tenant en la ventana → `unresolved`, no se toca nada. Un número por tenant queda para más adelante. Requiere migración en Next.js. | ✅ Confirmado (ver `docs/handoff/fase7/04-decisiones-abiertas.md`) |
| D10 | Hallazgos de seguridad del webhook Twilio en producción actual | Arreglar **B1 + B2 + B3 ya en Next.js** (el webhook está implementado pero no se usa, así que el cambio es inocuo y permite borrar ese código al migrar). B4, B5 y B7 los cubre Go. Antes de tocar, el repo Next.js verifica que el webhook no esté en uso. | ✅ Confirmado (ver `docs/handoff/fase7/04-decisiones-abiertas.md`) |
| D15 | Plantillas de WhatsApp: botones o texto libre | **Botones de respuesta rápida** con payloads fijos `CONFIRM` / `CANCEL`; el parser de texto libre se implementa igual como respaldo (precedencia `ButtonPayload` → `ButtonText` → `Body`). | ✅ Confirmado (Fase 7, D9 del handoff) |
| D16 | Quién envía las notificaciones salientes (WhatsApp y email) | **Pasan a Go en la Fase 7**, autenticadas con **OIDC de Google (Cloud Run)** verificado también en código. El corte real de Next.js a Go espera a que haya URL de Cloud Run (Fase 9). Requiere `insert` en `notifications` para el rol. | ✅ Confirmado (Fase 7, D10+D11 del handoff) |
| D17 | Status callbacks de Twilio (entregado, leído, fallido) | No se implementan en la Fase 7; la Status Callback URL se deja vacía. Se reevalúa después de la Fase 9. | ✅ Aplazado |
| D18 | Tenant y teléfono de pruebas de la Fase 7 | Tenant nuevo `dev-test` (Loja, Ecuador), copia reducida de Elvis Studio: cliente `v.pseudo.11@gmail.com`, owner `javiercalva@teams4soft.com`, especialista "Miguel" sobre un **usuario de auth nuevo** (`profiles.id` es PK y FK a `auth.users`: un usuario = un tenant). Scripts en `docs/handoff/sql/dev_test_tenant.sql`. | ✅ Ejecutado en producción el 2026-09-16; `verify_dev_test_tenant.sql` 16/16 ok |
