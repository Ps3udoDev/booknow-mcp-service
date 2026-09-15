# 🔒 Decisiones que bloquean la Fase 7

Responde aquí (o en el chat) antes de las tareas 7.4, 7.6 y 7.7. Cada una dice qué se bloquea y cuál es mi recomendación.

---

## D8.a — ¿Cómo se sabe a qué negocio pertenece un WhatsApp entrante?

Hoy se busca el teléfono del cliente en **todos** los negocios, lo que rompe el aislamiento (fallo B2).

| Opción | Cómo funciona | Requisitos |
|---|---|---|
| **A. Por número destino (`To`)** | Cada negocio tiene su propio número de WhatsApp; el `To` del mensaje identifica el negocio. | Un número (y un coste) por negocio, más una tabla que relacione número → negocio. |
| **B. Por la notificación enviada (recomendada)** | Se busca la última notificación de WhatsApp enviada a ese teléfono en una ventana (por ejemplo 48 h) y se usa su negocio y su cita. | Ninguno extra: ya se registra en `notifications` con `reference_id`. |
| **C. Mixta** | Si el `To` identifica un negocio, se usa; si no, se cae a la opción B. | Lo mejor de ambas cuando haya números por negocio. |

**Recomiendo B ahora y C cuando haya un número por negocio.** B resuelve además el fallo B3, porque enlaza la respuesta con la cita que se notificó.

**Tu respuesta:**

---

## D8.b — ¿Cuántos números de WhatsApp vais a tener?

Hoy: uno solo (o el sandbox). Si BookNow crece a varios negocios con un único número compartido, la opción A deja de servir y hay que forzar la B.

**Tu respuesta:**

---

## D9 — ¿Plantillas con botones o respuestas escritas?

Con botones de respuesta rápida, Twilio manda un payload estable y desaparece casi toda la ambigüedad del texto libre (fallo B4). Con texto libre hay que interpretar "casi no llego" y similares, y a veces no responder.

**Recomiendo botones**, manteniendo el parser de texto como respaldo.

**Tu respuesta:**

---

## D10 — ¿Quién envía las notificaciones salientes durante la transición?

| Opción | Efecto |
|---|---|
| **A. Se quedan en Next.js** de momento | La Fase 7 solo migra el webhook entrante. Menos riesgo, pero los fallos B6 y B7 siguen en Next.js. |
| **B. Pasan a Go en esta fase** (recomendada) | Un solo sitio que manda mensajes, con autenticación del llamador y HTML escapado. Next.js tendría que llamar al servicio Go. |

**Tu respuesta:**

---

## D11 — Si las notificaciones pasan a Go, ¿cómo se autentica quien las pide?

| Opción | Cuándo encaja |
|---|---|
| **A. Token de servicio compartido** en cabecera | Sencillo; hay que rotarlo a mano. |
| **B. OIDC de Google (Cloud Run)** (recomendada) | Vercel pide un token a Google y Cloud Run lo valida. Sin secretos compartidos, pero requiere configuración. |
| **C. JWT de Supabase del usuario** | Sirve si quien dispara la notificación es siempre una persona autenticada en el panel, no un proceso. |

**Tu respuesta:**

---

## D12 — ¿Registramos los callbacks de estado de Twilio (entregado, leído, fallido)?

Útil para saber si el recordatorio llegó; implica otra ruta, más filas en `notifications` y más tráfico.

**Recomiendo dejarlo para después de la Fase 9.**

**Tu respuesta:**

---

## D13 — ¿Qué hacemos ya en el repo Next.js?

Ver `02-bugs-produccion-twilio.md`. Opciones: arreglar ahora B1+B2+B3 (y comprobar B6), esperar a que Go lo reemplace, o desconectar el webhook en Twilio mientras tanto.

**Tu respuesta:**

---

## D14 — ¿Qué negocio y qué teléfono usamos para probar?

Elvis Studio y un teléfono tuyo dado de alta en el sandbox, o un negocio de pruebas aparte.

**Tu respuesta:**
