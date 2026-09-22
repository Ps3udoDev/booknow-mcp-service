# 🔒 Decisiones que bloqueaban la Fase 7 — CERRADAS

Respondidas por el usuario el **2026-09-15**. Este documento pasa a ser la referencia de lo decidido;
si algo cambia, se edita aquí y se anota en `docs/roadmap.md`.

Resumen: **D8.a → B**, **D8.b → un número ahora, uno por tenant después**, **D9 → botones**,
**D10 → migrar a Go ahora**, **D11 → B (OIDC)**, **D12 → aplazada a Fase 9**, **D13 → arreglar aquí**,
**D14 → tenant nuevo `dev-test`**.

---

## D8.a — ¿Cómo se sabe a qué negocio pertenece un WhatsApp entrante?

**✅ Decidido: opción B — por la notificación enviada.**

Se busca la última notificación de WhatsApp enviada a ese teléfono dentro de una ventana de 48 h y se
usan su `tenant_id` y su `reference_id` (la cita). Esto cierra a la vez el fallo **B2** (búsqueda global
de clientes) y el **B3** (se toca "la cita más reciente" en vez de la notificada).

### Cómo queda la consulta (contrato para la tarea 7.4)

`notifications` **no guarda el teléfono destino**: guarda `recipient_id` → `customers.id`. Así que la
resolución es un join, no una búsqueda libre por teléfono:

```sql
select n.tenant_id, n.reference_id as appointment_id, n.recipient_id as customer_id
from public.notifications n
join public.customers c
  on c.id = n.recipient_id and c.tenant_id = n.tenant_id
where n.channel = 'whatsapp'
  and n.recipient_type = 'customer'
  and n.reference_type = 'appointment'
  and n.notification_type <> 'appointment_response' -- las respuestas registradas no son envíos
  and n.status in ('sent', 'delivered', 'read')
  and n.created_at > now() - ($2::interval)          -- ventana, 48 h
  and c.phone_country_code || regexp_replace(c.phone, '\D', '', 'g') = $1  -- E.164 normalizado
order by n.created_at desc
limit 1;
```

Sigue habiendo un `where` sobre el teléfono, pero acotado a *"a este teléfono le mandamos una
notificación en las últimas 48 h"*, que es un conjunto mucho más pequeño que `customers` entera,
y devuelve la cita exacta.

### Reglas que se derivan y son obligatorias en 7.4

1. **Ventana**: 48 h, en una constante configurable (`TWILIO_INBOUND_WINDOW`, default `48h`).
   Coincide con la ventana de sesión de WhatsApp (24 h) con margen.
2. **Ambigüedad = no actuar.** Si en la ventana hay notificaciones de **más de un tenant** para ese
   teléfono, el resultado es `unresolved`: se registra el evento y no se toca ninguna cita. Nunca
   "gana la más reciente" cuando hay dos negocios en juego — eso reabriría B2 por la puerta de atrás.
   Se implementa con un `count(distinct n.tenant_id)` sobre la misma ventana antes del `limit 1`.
3. **Normalización del teléfono**: `customers` guarda `phone_country_code` (`+58`, `+593`…) y `phone`
   por separado; Twilio manda `From=whatsapp:+593XXXXXXXXX`. Hay que normalizar los dos lados a E.164
   sin espacios ni guiones antes de comparar. Si el dato en base está sucio, el mensaje queda
   `unresolved` — nunca se cae a comparar "los últimos 10 dígitos" (eso es el fallo B2).
4. **Permisos**: el rol `booknow_mcp_service` **no tiene hoy ningún grant sobre `notifications`**.
   La tarea 7.3 tiene que pedir `select (id, tenant_id, recipient_type, recipient_id,
   notification_type, channel, status, reference_type, reference_id, created_at)`.
   Sin `message` ni `title`, que pueden llevar texto con PII.

### Cuándo pasamos a la opción C (mixta)

Cuando haya un número de WhatsApp por tenant (ver D8.b): si el `To` identifica un tenant, manda el `To`;
si no, se cae a B. El código de 7.4 se escribe con esa forma desde el principio (un resolutor con dos
estrategias en cadena), aunque la primera devuelva siempre "no sé" mientras haya un solo número.

---

## D8.b — ¿Cuántos números de WhatsApp vais a tener?

**✅ Decidido: uno solo por ahora; un número por tenant más adelante.**

Consecuencias:

- La opción A queda descartada para la Fase 7 y B es la única vía. Confirmado arriba.
- Cuando llegue el número por tenant hará falta **una tabla que relacione número → tenant**
  (`tenant_whatsapp_senders` o una columna en `tenants`). **No se crea ahora**: se anota como
  pendiente de la fase en que aparezca el segundo número, para no meter esquema muerto.
- El resolutor de 7.4 sí se diseña con el hueco para esa estrategia (ver D8.a, "opción C").

---

## D9 — ¿Plantillas con botones o respuestas escritas?

**✅ Decidido: botones de respuesta rápida, con el parser de texto como respaldo.**

- Las plantillas de `01-configuracion-twilio-whatsapp.md` sección C se crean con quick reply
  **"Confirmar"** y **"Cancelar"**.
- Precedencia en el parser (tarea 7.5): `ButtonPayload` → `ButtonText` → `Body`. Si llega un
  `ButtonPayload` reconocido, el texto libre ni se mira.
- Los payloads de los botones se fijan a valores estables y explícitos: **`CONFIRM`** y **`CANCEL`**.
  Van en la definición de la plantilla en Twilio, no se generan. Cualquier otro payload → `unknown`.
- El parser de texto libre **se implementa igual** (7.5 completa), porque el cliente siempre puede
  escribir en vez de pulsar, y porque las conversaciones ya abiertas no llevan botones.

---

## D10 — ¿Quién envía las notificaciones salientes durante la transición?

**✅ Decidido: opción B — pasan a Go en esta fase.**

Motivo del usuario: migrarlo ahora evita repetir el trabajo más adelante, y cierra **B6**
(`notify-route.ts` sin autenticar) y **B7** (HTML sin escapar) en un solo sitio en vez de dos.

Consecuencias que hay que asumir en 7.7:

- Go pasa a ser el **único emisor** de WhatsApp y de email. Next.js deja de llamar a Twilio y a Resend
  y llama al servicio Go.
- El rol `booknow_mcp_service` necesita **`insert` en `notifications`** (hoy no lo tiene). Va en la
  migración de 7.3 junto con el resto.
- ⚠️ **Dependencia de orden**: para que Next.js pueda llamar a Go, Go tiene que estar desplegado y
  accesible, y eso es la **Fase 9**. Ver "Qué falta para continuar", punto 3: la tarea 7.7 se
  implementa y se prueba en la Fase 7 (con servidor HTTP falso), pero el **corte real** de Next.js a Go
  no ocurre hasta que haya URL de Cloud Run. Hasta entonces Next.js sigue enviando.
- Resend y el `.ics` (tarea 7.8) entran en el mismo paquete por la misma razón.

---

## D11 — ¿Cómo se autentica quien pide una notificación?

**✅ Decidido: opción B — OIDC de Google (Cloud Run).**

Vercel pide un token de identidad a Google y Cloud Run lo valida antes de que la request llegue al
código. Sin secretos compartidos que rotar.

Consecuencias:

- Configuración en la Fase 9: cuenta de servicio para Vercel, `roles/run.invoker` sobre el servicio, y
  el `audience` fijado a la URL del servicio.
- En la Fase 7 el endpoint se implementa con la **verificación del token en el código** (issuer
  `https://accounts.google.com`, `aud` = URL del servicio, firma por JWKS de Google), no solo confiando
  en el IAM de Cloud Run. Defensa en profundidad y, además, así es testeable en local.
- En local y en los tests se usa un emisor falso; **nunca** un modo "sin auth" activable por variable.

---

## D12 — ¿Registramos los callbacks de estado de Twilio?

**✅ Decidido: no en la Fase 7. Se deja para después de la Fase 9.**

- No se implementa la ruta de status callback ni se guardan `delivered_at` / `read_at`.
- En `01-configuracion-twilio-whatsapp.md` sección D, la **Status Callback URL se deja vacía**.
- `notifications.status` se queda en `sent` o `failed`, que es lo que 7.7 escribe.
- Se anota en el roadmap como pendiente de v2, no como deuda de la Fase 7.

---

## D13 — ¿Qué hacemos ya en el repo Next.js?

**✅ Decidido: arreglar B1 + B2 + B3 ahora en Next.js.**

Razonamiento del usuario, y **es correcto**: el webhook está implementado pero no se usa, así que el
arreglo no cambia ningún comportamiento en producción; y dejándolo arreglado, cuando Go tome el relevo
se puede borrar el código de Next.js y quedarse con todo centralizado, sin arrastrar un agujero de
seguridad en el intervalo.

⚠️ **Hay que verificar la premisa antes de tocar nada**, porque de ella depende que el cambio sea
inocuo. Lo comprueba el agente del repo Next.js:

1. ¿Está la URL del webhook configurada hoy en la consola de Twilio? (si no lo está, B1 ya está cerrado
   de facto y el arreglo es puramente preventivo).
2. ¿Hay filas en `notifications` con `channel = 'whatsapp'` de los últimos 90 días? Si las hay, el flujo
   **sí** se está usando y el cambio deja de ser inocuo.
3. ¿El middleware de Next.js cubre `/api/notifications/notify`? Esto es **B6** y es independiente: si no
   lo cubre, hay que protegerlo ya, no esperar a la Fase 7.

Si (1) y (2) salen negativos, el arreglo entra sin riesgo. Si alguno sale positivo, se avisa antes de
tocar. Mientras tanto, la alternativa de coste cero sigue disponible: **quitar la URL del webhook en
Twilio**.

Nota: B4, B5 y B7 **no** se arreglan en Next.js — los cubre Go en las tareas 7.5, 7.3 y 7.8.

---

## D14 — ¿Qué negocio y qué teléfono usamos para probar?

**✅ Decidido: un tenant nuevo `dev-test`, copia reducida de Elvis Studio, en Ecuador / Loja.**

| Actor | Quién | Dónde vive |
|---|---|---|
| Cliente | `v.pseudo.11@gmail.com` | `customers` |
| Owner (staff que usa el MCP) | `javiercalva@teams4soft.com` | `tenant_users` con rol `owner` |
| Especialista | "Miguel", **usuario de auth nuevo** | `profiles` con `is_specialist = true` |

Datos del tenant: `slug = dev-test`, `country_code = EC`, `timezone = America/Guayaquil`,
`currency_code = USD`, `locale = es-EC`, `status = active`. Sucursal única **"Loja Centro"**.
Servicios clonados de Elvis: **Corte de cabello** (30 min, 5 USD) y **Ballage** (30 min + 5 buffer,
25 USD), que son justo los dos que tiene asignados Miguel en Elvis.

### ⚠️ Por qué Miguel **no** se puede reutilizar

`profiles` tiene `PRIMARY KEY (id)` y `FOREIGN KEY (id) REFERENCES auth.users(id)`, con un único
`tenant_id` por fila. Es decir: **un usuario de auth = un perfil = un tenant**. La fila de Miguel ya
apunta a `elvis-studio`; no puede tener otra en `dev-test`. Por eso el especialista de `dev-test` es un
**usuario de auth nuevo** que clona sus datos (nombre `Miguel`, `specialties = {hair_cutting,
hair_styling}`, los mismos dos servicios).

`javiercalva@teams4soft.com`, en cambio, sí se reutiliza tal cual: es `admin` en `elvis-studio` pero
**solo vía `tenant_users`**, que sí admite un usuario en varios tenants
(`unique (auth_user_id, tenant_id)`), y **no tiene ninguna fila en `profiles`** en ningún tenant.
Y `tenant_users` es exactamente lo que consulta la autorización del MCP
(`internal/repository/postgres/mcp_access.go`), así que con eso basta para que el MCP funcione.

### Ejecución

Script en `docs/handoff/sql/dev_test_tenant.sql`, verificación en
`docs/handoff/sql/verify_dev_test_tenant.sql`. **Se ejecuta en producción** (proyecto `book-now-hub`)
desde el SQL Editor de Studio, siguiendo `docs/handoff/nextjs-migracion-fase7-dev-test-tenant.md`.

El **teléfono de pruebas no se escribe en el repo**: el script lo toma de una variable que se rellena
en Studio al ejecutarlo (`:test_phone`). Regla 7 del `README.md`.
