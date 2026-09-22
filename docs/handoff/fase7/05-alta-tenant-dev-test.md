# 🔒 Alta del tenant de pruebas `dev-test`

Lo ejecuta **el usuario** en el SQL Editor de Supabase Studio, proyecto `book-now-hub`
(`rrnysepngbycvuciodoj`). Cierra la decisión **D14** de `04-decisiones-abiertas.md`.

No es una migración: **no crea ni altera tablas**, solo inserta datos. Por eso no pasa por el repo
Next.js (regla 5 del `README.md`, que aplica a esquema y permisos).

| Archivo | Qué hace |
|---|---|
| `docs/handoff/sql/dev_test_tenant.sql` | Crea el tenant, sucursal, módulos, owner, especialista, servicios, horarios, puestos y cliente. Idempotente. |
| `docs/handoff/sql/verify_dev_test_tenant.sql` | 16 comprobaciones de solo lectura. El teléfono sale enmascarado. |

## Qué queda montado

```
tenant  dev-test  "Dev Test"   EC / America/Guayaquil / USD / es-EC / active
└── sucursal  "Loja Centro"  (principal, L-V 08:00-18:00, S 09:00-14:00)
    ├── puestos      Puesto 1, Puesto 2
    ├── servicios    Corte de cabello (30 min, 5 USD) · Ballage (30+5 min, 25 USD)
    ├── owner        javiercalva@teams4soft.com   -> tenant_users, rol owner
    ├── especialista "Miguel" (auth user NUEVO)   -> profiles, is_specialist
    │                 los 2 servicios, L-V 09:00-18:00 con pausa 13:00-14:00
    └── cliente      v.pseudo.11@gmail.com        -> customers, notify_whatsapp = true
```

17 módulos habilitados, los mismos que Elvis Studio. Los que importan aquí son **`business-mcp`**
(sin él el MCP de Go deniega todo) y **`notifications`** (flujo de WhatsApp).

## Pasos

### 1. Crear el usuario de auth del especialista

Studio → **Authentication → Users → Add user → Create new user**

- email: `miguel.dev@teams4soft.com` (o el que prefieras)
- marca **Auto Confirm User**

Copia su UUID. Es el único usuario nuevo que hace falta: `javiercalva@teams4soft.com` y
`v.pseudo.11@gmail.com` **ya existen** en `auth.users`.

> **Por qué no se reutiliza al Miguel de Elvis Studio:** `public.profiles` tiene `PRIMARY KEY (id)` y
> `FOREIGN KEY (id) REFERENCES auth.users(id)`, con un solo `tenant_id` por fila. Un usuario de auth
> solo puede tener un perfil, en un tenant. El script lo comprueba y aborta con un mensaje explícito
> si le pasas un UUID que ya tiene perfil en otro sitio.
>
> `javiercalva` sí se reutiliza porque es `admin` en `elvis-studio` **solo vía `tenant_users`**, que
> admite el mismo usuario en varios tenants (`unique (auth_user_id, tenant_id)`), y no tiene ninguna
> fila en `profiles`. Y `tenant_users` es justo lo que consulta
> `internal/repository/postgres/mcp_access.go`, así que basta para que el MCP funcione.

### 2. Editar los dos valores del script

En `dev_test_tenant.sql`, las dos líneas marcadas `<<< EDITA`:

```sql
v_specialist_auth_id uuid := '<UUID del paso 1>';
v_test_phone_e164    text := '+593XXXXXXXXX';   -- tu teléfono de pruebas, E.164
```

El teléfono tiene que estar dado de alta en el **sandbox de Twilio** (`join <dos-palabras>`, ver
`01-configuracion-twilio-whatsapp.md` sección B.1).

⚠️ **No guardes el archivo editado en el repo ni pegues el teléfono en el chat** (regla 7 del
`README.md`). Edítalo en el propio SQL Editor.

### 3. Ensayo con `rollback`

En el SQL Editor, pega:

```sql
begin;
-- el bloque do $$ ... $$; entero
rollback;
```

Tienen que salir los `NOTICE` con los ids y ninguna excepción. Si algo falla, falla aquí sin escribir.

### 4. Ejecución real

Lo mismo con `commit;` en vez de `rollback;`. Anota el `tenant_id` que sale en el resumen.

### 5. Verificar

Ejecuta `verify_dev_test_tenant.sql` entero. **Las 16 filas tienen que decir `ok`.**

### 6. Crear una cita futura

No la crea el script porque depende de la fecha en que pruebes. Hazla desde la app, o con el `insert`
comentado al final de `verify_dev_test_tenant.sql`.

Comprueba la hora en la zona del tenant, no en la de tu sesión:

```sql
select scheduled_at at time zone 'America/Guayaquil' from public.appointments where ...;
```

Es la misma trampa que causó el bug de zona horaria de Táriba en la Fase 6.

### 7. Conectar el MCP

Para que el owner pueda usar las tools contra `dev-test` hace falta una fila en `mcp_connections`
con `(auth_user_id, oauth_client_id, tenant_id, status = 'active')`. **No la crea este script**: la
crea el flujo de consentimiento OAuth cuando `javiercalva` autorice el cliente MCP eligiendo
`dev-test`. Si ya tiene una conexión apuntando a `elvis-studio`, tendrá que consentir de nuevo para
este tenant (`D3` del roadmap: gana la de `updated_at` más reciente).

## Validación ya hecha

Los dos scripts se probaron contra el esquema real en la base local de Supabase
(`supabase_db_MCP`), dentro de `begin … rollback`:

- El script completo corre de principio a fin sin errores.
- **Idempotencia**: ejecutado dos veces seguidas deja 1 tenant, 1 sucursal, 2 servicios, 2
  `specialist_services`, 5 horarios, 2 puestos, 1 cliente, 1 `tenant_user`, 1 perfil, 17 módulos.
- Las 16 comprobaciones de `verify_dev_test_tenant.sql` dan `ok`.
- La guarda del especialista aborta correctamente si el UUID ya tiene perfil en otro tenant.

Dos errores que salieron en esa validación y ya están corregidos: `tenants.created_by` y
`tenant_modules.enabled_by` referencian `public.global_users`, **no** `auth.users` (los dos tenants
existentes los tienen a `null`, y aquí se dejan igual).

Lo que **no** se pudo validar en local: `public.modules` y `public.currencies` están vacías en el
snapshot (no trae datos), así que se sembraron a mano para la prueba. En producción existen las 17
filas de módulos y `USD`. El script avisa con un `warning` si algún slug de módulo no existe.
