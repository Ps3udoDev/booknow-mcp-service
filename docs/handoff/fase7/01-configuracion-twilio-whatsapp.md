# 🔒 Qué tienes que configurar tú: Twilio, WhatsApp y Resend

Todo lo de este documento lo hace el usuario en consolas externas. El agente no puede hacerlo y no debe pedir que le pegues secretos en el chat.

Marca cada punto cuando esté hecho y anota el resultado en `reportes/7.0-configuracion.md` (usa la plantilla).

---

## A. Twilio: cuenta y credenciales

- [ ] **Cuenta de Twilio** (la que ya usa BookNow en producción sirve). Anota en qué cuenta estás trabajando: *trial* o *upgraded*. En trial solo puedes enviar mensajes a números verificados.
- [ ] **Account SID** (`TWILIO_ACCOUNT_SID`, empieza por `AC…`). No es secreto, pero tampoco lo publiques.
- [ ] **Auth Token** (`TWILIO_AUTH_TOKEN`). **Es secreto.** Con él se firman y se validan los webhooks.
  - Guárdalo en Google Secret Manager cuando llegue la Fase 9. Mientras tanto, en tu `.env` local (que está en `.gitignore`).
  - Si sospechas que se filtró, rótalo desde la consola. Twilio permite tener un token primario y uno secundario para rotar sin cortes.
- [ ] Decide si quieres usar **API Key + Secret** en lugar del Auth Token para *enviar* mensajes (recomendado a futuro: se puede revocar sin romper la validación de firma). La validación de la firma entrante **siempre** usa el Auth Token de la cuenta.

## B. WhatsApp: remitente

Dos caminos. Elige uno y anótalo.

### B.1 Sandbox (para desarrollo y pruebas)
- [ ] Twilio Console → Messaging → Try it out → **Send a WhatsApp message**.
- [ ] Número del sandbox (`whatsapp:+14155238886` normalmente) → será `TWILIO_WHATSAPP_FROM`.
- [ ] Cada teléfono que quiera recibir mensajes debe escribir antes `join <dos-palabras>` al sandbox. Anota qué números has dado de alta (los de prueba, no los de clientes reales).
- [ ] Limitación: en sandbox **no** hacen falta plantillas aprobadas, pero la sesión caduca a las 24 h y el remitente no es tu marca.

### B.2 Número de producción (WhatsApp Business Platform)
- [ ] Perfil de empresa verificado en **Meta Business Manager** (puede tardar días).
- [ ] **WhatsApp Sender** creado en Twilio (Messaging → Senders → WhatsApp senders) con un número que **no** esté ya usado en la app de WhatsApp normal.
- [ ] Display name aprobado por Meta.
- [ ] Anota el número en formato `whatsapp:+58XXXXXXXXXX` → `TWILIO_WHATSAPP_FROM`.
- [ ] (Opcional pero recomendado) **Messaging Service** que agrupe el remitente; permite cambiar de número sin tocar el código.

## C. Plantillas de mensaje (Content Templates)

WhatsApp exige plantilla aprobada para escribir a alguien fuera de la ventana de 24 h. El código actual usa una sola (`TWILIO_CONTENT_SID`) con dos variables: `{{1}}` fecha y `{{2}}` hora.

- [ ] Revisa en Twilio Console → Content Template Builder qué plantillas existen hoy y cuál está en producción. Anota **nombre, SID (`HX…`), idioma, categoría y el texto exacto con sus variables**.
- [ ] Decide qué plantillas quieres para la Fase 7. Propuesta mínima:
  | Uso | Variables sugeridas | Botones |
  |---|---|---|
  | Recordatorio / confirmación de cita | 1 nombre del cliente, 2 servicio, 3 fecha, 4 hora, 5 sucursal | Quick reply: "Confirmar" y "Cancelar" |
  | Aviso de cita cancelada | 1 nombre, 2 fecha, 3 hora | — |
  - **Recomendación fuerte:** usa plantillas con **botones de respuesta rápida** en lugar de pedir que el cliente escriba "sí"/"no". Twilio envía entonces un payload estable (`ButtonPayload` / `ButtonText`) y desaparece casi todo el problema de interpretar texto libre.
- [ ] Manda a aprobar las plantillas (Meta tarda de minutos a días) y anota el SID de cada una.
- [ ] Anota el **idioma** de cada plantilla (`es`, `es_ES`, …): el envío falla si no coincide.

## D. Webhook entrante

- [ ] La URL definitiva depende del despliegue en Cloud Run (Fase 9). Hasta entonces se prueba con la URL temporal de staging o con un túnel local.
- [ ] Ruta que implementará Go: **`POST /webhooks/twilio`** sobre el dominio del servicio (por ejemplo `https://mcp.booknow.app/webhooks/twilio`).
- [ ] Configúrala en Twilio: en el WhatsApp Sender (o en el Messaging Service → Integration) → "When a message comes in" → **HTTP POST** con esa URL.
- [ ] Deja también la **Status Callback URL** apuntando al mismo servicio si quieres registrar entregas y lecturas (decisión pendiente; ver `04-decisiones-abiertas.md`).
- [ ] **Importante para la firma:** la URL configurada debe coincidir **carácter a carácter** con la que el servicio usa para validar la firma (incluye `https://`, el host y la ruta; sin barra final de más). Anota la URL exacta que dejes puesta.
- [ ] Anota si hay algún proxy, redirección o dominio alternativo delante (cambia la URL que Twilio firma).

## E. Resend (emails con .ics)

- [ ] Cuenta de Resend y **dominio verificado** (SPF, DKIM y DMARC en el DNS).
- [ ] `RESEND_API_KEY` (**secreto**).
- [ ] `FROM_EMAIL` con el dominio verificado; hoy el código cae por defecto en `onboarding@resend.dev`, que no sirve para producción.
- [ ] Decide si los emails los sigue enviando Next.js o pasan a Go en esta fase (ver `04-decisiones-abiertas.md`).

## F. Números y datos de prueba

- [ ] Un teléfono de prueba tuyo dado de alta en el sandbox (o que puedas usar con el número de producción).
- [ ] Un cliente de prueba en Elvis Studio con ese teléfono y una cita futura, para probar confirmar y cancelar de punta a punta.
- [ ] Confirma si podemos usar el negocio **Elvis Studio** para las pruebas o prefieres un negocio aparte.

## G. Cómo entregar los datos (sin filtrar secretos)

1. **No pegues secretos en el chat ni en ningún `.md` del repo.**
2. Escribe los valores en tu `.env` local (ya está en `.gitignore`):
   ```
   TWILIO_ACCOUNT_SID=AC...
   TWILIO_AUTH_TOKEN=...
   TWILIO_WHATSAPP_FROM=whatsapp:+14155238886
   TWILIO_CONTENT_SID=HX...
   TWILIO_WEBHOOK_URL=https://<host>/webhooks/twilio
   RESEND_API_KEY=re_...
   FROM_EMAIL=Elvis Studio <citas@tudominio.com>
   ```
3. En el chat basta con que digas: "listo, las variables están en `.env`", más los datos **no secretos**: número remitente, SIDs de plantillas, idioma, URL del webhook y si es sandbox o producción.
4. El agente añadirá esas variables a `.env.example` **sin valores** y las usará desde el entorno.

## H. Checklist de cierre

- [ ] Sé si estoy en sandbox o en producción.
- [ ] Tengo remitente, plantillas aprobadas con sus SIDs e idioma.
- [ ] La URL del webhook está anotada y coincide con la que validará el servicio.
- [ ] Los secretos están solo en `.env` (y en Secret Manager cuando llegue la Fase 9).
- [ ] Tengo un teléfono y un cliente de prueba listos.
- [ ] Respondí `04-decisiones-abiertas.md`.
