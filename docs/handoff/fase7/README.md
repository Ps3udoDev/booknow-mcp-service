# Fase 7 — Twilio WhatsApp: índice y reglas de trabajo

Carpeta de traspaso para continuar la Fase 7 en otra sesión o con otro agente. Fecha de creación: 2026-09-15.

Estado del proyecto: Fases 1–6 hechas (MCP completo, tools de lectura y escrituras en dos pasos validadas en producción). `docs/roadmap.md` manda sobre el estado; esta carpeta es el detalle de la Fase 7.

## Documentos

| Archivo | Para quién | Contenido |
|---|---|---|
| `01-configuracion-twilio-whatsapp.md` | 🔒 el usuario | Qué hay que crear y configurar en Twilio, Meta/WhatsApp y Resend, y qué datos entregar (sin secretos en el repo). |
| `02-bugs-produccion-twilio.md` | 🔒 el usuario + agente del repo Next.js | Los 5 fallos del webhook actual en producción y cómo mitigarlos ya, antes de que Go lo reemplace. |
| `03-plan-fase7.md` | el agente que implemente | Desglose en tareas 7.1–7.9 con entregables, criterios de aceptación y tests. |
| `04-decisiones-abiertas.md` | todos | ✅ **CERRADO el 2026-09-15.** Lo decidido en D8–D14 y las reglas que se derivan. Contrato de las tareas 7.4, 7.6 y 7.7. |
| `05-alta-tenant-dev-test.md` | 🔒 el usuario | Cómo dar de alta el tenant de pruebas `dev-test` (decisión D14). |
| `plantilla-reporte-task.md` | el agente que implemente | Plantilla del informe que deja cada tarea. |
| `reportes/` | todos | Un `.md` por tarea terminada. |

## Reglas de trabajo (obligatorias)

1. **Un informe por tarea.** Al terminar cada tarea (7.1, 7.2, …), crea `docs/handoff/fase7/reportes/7.X-<slug>.md` siguiendo `plantilla-reporte-task.md`, en el mismo commit que el código. Sin informe, la tarea no está terminada.
2. **Actualiza `docs/roadmap.md`** en ese mismo commit: marca `[x]` la línea correspondiente de la Fase 7.
3. **TDD.** Test primero, verlo fallar, luego el código. Al terminar, muta a mano 3–5 reglas críticas y comprueba que los tests las detectan; anótalo en el informe.
4. **Antes de dar una tarea por terminada:** `golangci-lint run` y `go test ./...` deben pasar. Los tests de integración necesitan `TEST_DATABASE_URL` (ver `CLAUDE.md`).
5. **Migraciones:** este repo nunca las aplica. Si una tarea necesita cambios de esquema o permisos, prepara el SQL en `docs/handoff/sql/`, escribe un documento de instrucciones como los de las Fases 5 y 6 (`docs/handoff/nextjs-migracion-fase*.md`) y **para**: lo aplica el agente del repo Next.js con la aprobación del usuario.
6. **Secretos:** nunca en el repo, en logs ni en informes. `TWILIO_AUTH_TOKEN` y `RESEND_API_KEY` son secretos; en los informes se nombran, nunca se copian.
7. **PII:** los mensajes de WhatsApp traen teléfonos y texto del cliente. No se registran en logs; en la auditoría o en la base solo lo estrictamente necesario, con el teléfono enmascarado (`internal/platform/pii.MaskPhone`).
8. **Nada de cambios en el repo Next.js desde aquí.** Solo instrucciones.

## Orden recomendado

`7.1 → 7.2 → 7.5 → 7.3 (🔒 migración) → 7.4 → 7.6 → 7.7 → 7.8 → 7.9`

Las decisiones ya están cerradas (`04-decisiones-abiertas.md`), así que el único bloqueo que queda es
la migración de 7.3.

- **7.1** y **7.2** ✅ hechas. **7.3**: SQL listo y validado, pendiente de que lo aplique Next.js
  (`docs/handoff/nextjs-migracion-fase7-twilio.md`). D13 en `docs/handoff/nextjs-fase7-arreglo-webhook-twilio.md`.
- **7.5** no depende de nada externo: se puede hacer ya. 7.5 sube de posición porque D9
  fijó los payloads de los botones (`CONFIRM` / `CANCEL`) y ya no necesita nada más.
- **7.3** produce el SQL, pero **lo aplica el repo Next.js**. Todo lo que hay de 7.4 en adelante
  espera a que esté aplicado.
- **7.7** y **7.8** se implementan y prueban en esta fase, pero el corte real de Next.js a Go no
  ocurre hasta la Fase 9 (ver D10).

## Contexto técnico que ya existe y conviene reutilizar

- `internal/httpapi/` — router chi, middleware, límites de body, logger sin headers ni cuerpos.
- `internal/config/` — carga y validación de variables; sigue su patrón para las de Twilio.
- `internal/platform/pii` — enmascarado de teléfonos.
- `internal/repository/postgres/` — `DBTX` (pool o transacción), stores con SQL parametrizado y tests que corren como el rol `booknow_mcp_service`.
- `internal/application/drafts` — ejemplo reciente de caso de uso + store + errores seguros.
- `migracion/twilio-whatsapp/*.ts` — implementación TypeScript actual (solo lectura, es el comportamiento a portar).
