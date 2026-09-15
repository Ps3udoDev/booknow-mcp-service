# Fallos del webhook de Twilio que están hoy en producción

Los encontré al leer `migracion/twilio-whatsapp/*.ts`, que es una copia del código desplegado en Vercel. **No son problemas futuros: afectan al sistema que está funcionando ahora.** Go los corrige en la Fase 7, pero conviene decidir si se mitigan antes en Next.js.

Orden por gravedad.

---

## B1 — El webhook no verifica la firma de Twilio 🔴 crítico

`twilio-webhook-route.ts` procesa el `POST` sin comprobar la cabecera `X-Twilio-Signature`.

**Qué permite:** cualquiera que conozca la URL puede enviar un formulario con `From` y `Body` falsos y **confirmar o cancelar la cita de otro cliente**. No hace falta autenticación ninguna. Combinado con B2, basta con acertar los últimos 10 dígitos de un teléfono.

**Arreglo:** validar `X-Twilio-Signature` sobre la URL pública y el cuerpo **original**, antes de parsearlo, y responder 403 si no cuadra. En Next.js: `twilio.validateRequest(authToken, signature, url, params)`. En Go lo hace la tarea 7.1.

**Mitigación inmediata posible sin código:** quitar la URL del webhook en la consola de Twilio hasta que haya validación, si el flujo de respuestas por WhatsApp no está en uso real.

## B2 — Busca al cliente en todos los negocios 🔴 crítico

```ts
const lastDigits = rawPhone.slice(-10);
const { data: customers } = await supabase.from("customers").select("id, tenant_id").like("phone", `%${lastDigits}`).limit(5);
```

No filtra por negocio y luego modifica la cita más reciente de **cualquiera** de los clientes encontrados. Dos clientes de negocios distintos con los mismos últimos 10 dígitos (o el mismo cliente en dos negocios) se pisan entre sí. Rompe el aislamiento multitenant, que es regla no negociable del proyecto.

**Arreglo:** resolver primero el negocio (por número destino `To` o por la cita notificada) y buscar el cliente **dentro** de ese negocio. Es la decisión D8 de `04-decisiones-abiertas.md`.

## B3 — Modifica "la cita más reciente", no la notificada 🟠 alto

Toma la cita `pending` o `confirmed` con `scheduled_at` más alto. Si el cliente tiene varias citas, responde "sí" a un recordatorio de la cita del martes y le confirma la del viernes. Con `order by scheduled_at desc` y citas futuras, casi siempre es la equivocada.

**Arreglo:** enlazar la respuesta con la notificación que la provocó (`notifications.reference_id` de la última notificación de WhatsApp a ese cliente, dentro de una ventana de tiempo).

## B4 — Interpreta la intención con `includes` 🟠 alto

```ts
const isConfirm = CONFIRM_KEYWORDS.some((kw) => body.includes(kw));
```

Busca subcadenas en texto libre en minúsculas:

| Mensaje del cliente | Interpretación actual | Correcto |
|---|---|---|
| "buenos días" | **confirmar** (contiene "no"… y "si" no; pero "buenos" contiene "no") → en realidad cancela | ninguna |
| "casi no llego" | confirmar y cancelar a la vez (gana confirmar por el orden) | ninguna / ambigua |
| "no sé si podré" | confirmar | ninguna / ambigua |
| "SÍ" con tilde y mayúscula | funciona | ok |

Además, si coinciden las dos listas gana confirmar, que es justo lo contrario de lo prudente.

**Arreglo:** normalizar (minúsculas, sin tildes, sin signos) y comparar **palabras completas**; si coinciden las dos intenciones o ninguna, no hacer nada. Mejor aún: usar plantillas con botones de respuesta rápida (ver `01-configuracion-twilio-whatsapp.md`, sección C) y leer `ButtonPayload`.

## B5 — No deduplica los reintentos de Twilio 🟡 medio

Twilio reintenta si el webhook tarda o falla. Cada reintento vuelve a ejecutar el cambio de estado y **vuelve a insertar** una fila en `notifications`. Como el update es idempotente en la práctica, el daño principal es ruido en `notifications` y estados que rebotan si llegan un "sí" y un "no" casi a la vez.

**Arreglo:** tabla de eventos procesados con `MessageSid` único (tarea 7.3, requiere migración en el repo Next.js).

## B6 — `notify-route.ts` no autentica al que llama 🟠 alto (a confirmar)

En la copia que tengo, `POST /api/notifications/notify` recibe `{ appointmentId }` y envía WhatsApp y emails sin comprobar quién llama. Si el middleware de Next.js no lo protege, cualquiera puede **mandar mensajes a los clientes** (coste y spam desde tu marca), y comprobar la existencia de citas por el 404/200.

**Acción:** confirmar en el repo Next.js si el middleware cubre esa ruta. Si no, protegerla ya.

## B7 — HTML de email sin escapar 🟡 medio

`notify-route.ts` interpola nombres, servicio y notas del cliente directamente en el HTML del email. Un nombre con `<img onerror=…>` o similar se cuela en el correo que reciben el cliente y el especialista.

**Arreglo:** escapar todo lo que venga de la base al construir el HTML.

## B8 — `campaigns-send-route.ts` es un stub 🟢 informativo

No envía nada. Decidir si se migra o se elimina (roadmap, Fase 7).

---

## Qué le pediría al repo Next.js ahora mismo

Propuesta para que el usuario decida (ninguna acción se ejecuta sin su "sí"):

1. **B1 + B2 + B3 juntos**, que son el agujero de seguridad real: validar firma, resolver el negocio y actuar solo sobre la cita notificada. Es un cambio acotado en `twilio-webhook-route.ts`.
2. **B6**: comprobar el middleware y proteger `notify` si hace falta.
3. B4, B5, B7 pueden esperar a que Go tome el relevo, salvo que el flujo esté en uso con clientes reales.

Si prefieres no tocar Next.js, la alternativa es **desconectar la URL del webhook en Twilio** hasta que el servicio en Go esté desplegado. Mientras la URL esté configurada y sin validación de firma, B1 sigue abierto.
