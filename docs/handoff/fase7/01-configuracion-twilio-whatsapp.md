# 🔒 Qué tienes que configurar tú: Twilio, WhatsApp y Resend

Todo lo de este documento lo hace el usuario en consolas externas. El agente no puede hacerlo y no debe pedir que le pegues secretos en el chat.

Marca cada punto cuando esté hecho y anota el resultado en `reportes/7.0-configuracion.md` (usa la plantilla).

---

## A. Twilio: cuenta y credenciales

- [x] **Cuenta de Twilio** (la que ya usa BookNow en producción sirve). Cuenta *trial*.
- [x] **Account SID** (`TWILIO_ACCOUNT_SID`: en `.env` local).
- [x] **Auth Token** (`TWILIO_AUTH_TOKEN`). Guardado en `.env` local.
- [x] Decide si quieres usar **API Key + Secret** en lugar del Auth Token para *enviar* mensajes. Para la validación de webhooks entrantes se utiliza el `TWILIO_AUTH_TOKEN`.

## B. WhatsApp: remitente

Dos caminos. Elige uno y anótalo.

### B.1 Sandbox (para desarrollo y pruebas)
- [x] Twilio Console → Messaging → Try it out → **Send a WhatsApp message**.
- [x] Número del sandbox (`whatsapp:+14155238886`) → `TWILIO_WHATSAPP_FROM`.
- [x] Teléfono de prueba unido mediante `join <palabras>` al sandbox (`+59******418`, valor real solo en `.env`).
- [x] Limitación conocida: en sandbox no hacen falta plantillas aprobadas por Meta, sesión caduca a las 24 h.

### B.2 Número de producción (WhatsApp Business Platform)
- [ ] Perfil de empresa verificado en **Meta Business Manager** (aplazado a fase posterior).
- [ ] **WhatsApp Sender** creado en Twilio.
- [ ] Display name aprobado por Meta.
- [ ] Anota el número en formato `whatsapp:+58XXXXXXXXXX` → `TWILIO_WHATSAPP_FROM`.
- [ ] (Opcional pero recomendado) **Messaging Service** que agrupe el remitente.

## C. Plantillas de mensaje (Content Templates)

- [x] Revisa en Twilio Console → Content Template Builder qué plantillas existen. Identificada `HX79aa0b881e10346eb8e34ccf507d5b23` (Quick Reply) y `HXb5b62575e6e4ff6129ad7c8efe1f983e`.
- [x] Decide qué plantillas quieres para la Fase 7. Propuesta con botones Quick Reply ("Confirmar" / "Cancelar", payloads `CONFIRM` / `CANCEL`).
- [x] SIDs anotados en `reportes/7.0-configuracion.md`.
- [ ] Asegurar que el **idioma** de la plantilla coincida con el idioma solicitado en el envío (`es` vs `en`).

## D. Webhook entrante

- [x] Ruta que implementará Go: **`POST /webhooks/twilio`**.
- [ ] Configurada en Twilio con URL pública de prueba (ngrok / Cloud Run temporal cuando esté lista la tarea 7.2).
- [x] Status Callback URL: se deja vacía (decisión D12 / D17).

## E. Resend (emails con .ics)

- [x] Cuenta de Resend y **dominio verificado**.
- [x] `RESEND_API_KEY` en local `.env`.
- [x] `FROM_EMAIL` con el dominio verificado configurado.
- [x] Envíos migran a Go en esta fase (decisión D10 / D16).

## F. Números y datos de prueba

- [x] Teléfono de prueba en el sandbox (`+59******418`, valor real solo en `.env`).
- [x] Cliente de prueba `v.pseudo.11@gmail.com` en tenant dedicado `dev-test` (`docs/handoff/sql/dev_test_tenant.sql`).
- [x] Decisión tomada: no tocar Elvis Studio en pruebas, usar `dev-test`.

## G. Cómo entregar los datos (sin filtrar secretos)

1. **No pegues secretos en el chat ni en ningún `.md` del repo.**
2. Escribe los valores en tu `.env` local (ya está en `.gitignore`):
   ```
   TWILIO_ACCOUNT_SID=AC...
   TWILIO_AUTH_TOKEN=...
   TWILIO_WHATSAPP_FROM=whatsapp:+14155238886
   TWILIO_CONTENT_SID=HX79aa0b881e10346eb8e34ccf507d5b23
   TWILIO_WEBHOOK_URL=https://<host>/webhooks/twilio
   RESEND_API_KEY=re_...
   FROM_EMAIL=Elvis Studio <citas@tudominio.com>
   ```
3. En el chat basta con que digas: "listo, las variables están en `.env`", más los datos **no secretos**: número remitente, SIDs de plantillas, idioma, URL del webhook y si es sandbox o producción.
4. El agente añadirá esas variables a `.env.example` **sin valores** y las usará desde el entorno.

## H. Checklist de cierre

- [x] Sé si estoy en sandbox o en producción (Sandbox).
- [x] Tengo remitente, plantillas con sus SIDs e idioma anotados.
- [ ] La URL del webhook está anotada y coincide con la que validará el servicio (pendiente de túnel/despliegue en 7.2).
- [x] Los secretos están solo en `.env`.
- [x] Tengo un teléfono y un cliente de prueba listos.
- [x] Respondí `04-decisiones-abiertas.md`.
