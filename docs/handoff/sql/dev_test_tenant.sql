-- booknow-mcp-service: tenant de pruebas "dev-test" para la Fase 7 (Twilio WhatsApp).
-- Decision D14 de docs/handoff/fase7/04-decisiones-abiertas.md.
--
-- Copia reducida de Elvis Studio, ubicada en Loja (Ecuador), con tres actores:
--   cliente      v.pseudo.11@gmail.com        -> public.customers
--   owner (MCP)  javiercalva@teams4soft.com   -> public.tenant_users (rol owner)
--   especialista "Miguel" (auth user NUEVO)   -> public.profiles (is_specialist = true)
--
-- Por que el especialista necesita un usuario de auth nuevo:
--   public.profiles tiene PRIMARY KEY (id) y FOREIGN KEY (id) -> auth.users(id), con un unico
--   tenant_id por fila. Un usuario de auth solo puede tener UN perfil, en UN tenant. El Miguel de
--   Elvis Studio ya esta atado a ese tenant y no se puede reutilizar aqui.
--   javiercalva si se reutiliza: es admin en elvis-studio solo via tenant_users, que admite el mismo
--   usuario en varios tenants (unique (auth_user_id, tenant_id)), y no tiene fila en profiles.
--
-- El script es IDEMPOTENTE: se puede reejecutar sin duplicar nada.
-- No crea usuarios de auth (eso no se hace por SQL) ni escribe el telefono de pruebas en el repo.
--
-- ============================================================================
-- ANTES DE EJECUTAR
-- ============================================================================
--
-- 1. Crea el usuario de auth del especialista en Studio:
--       Authentication -> Users -> Add user -> "Create new user"
--       email: miguel.dev@teams4soft.com   (o el que prefieras)
--       marca "Auto Confirm User"
--    Copia su UUID.
--
-- 2. Rellena los DOS valores marcados con "<<< EDITA" abajo.
--
-- 3. Ensayo (obligatorio): ejecuta en el SQL Editor envuelto en una transaccion que se deshace,
--    para ver los NOTICE sin escribir nada:
--
--       begin;
--       <pega aqui todo el bloque do $$ ... $$;>
--       rollback;
--
--    Revisa la salida. Si no hay excepciones, vuelve a ejecutarlo con "commit;" en vez de "rollback;".
--
-- 4. Verifica con docs/handoff/sql/verify_dev_test_tenant.sql.
--
-- ============================================================================

do $$
declare
  -- <<< EDITA: UUID del usuario de auth creado en el paso 1.
  v_specialist_auth_id uuid := '00000000-0000-0000-0000-000000000000';

  -- <<< EDITA: telefono de pruebas en E.164, dado de alta en el sandbox de Twilio (ej: +593987654321).
  --            NO lo dejes guardado en el repo ni lo pegues en el chat.
  v_test_phone_e164 text := '+593000000000';

  -- Constantes del tenant. No hace falta tocarlas.
  c_owner_email      constant text := 'javiercalva@teams4soft.com';
  c_customer_email   constant text := 'v.pseudo.11@gmail.com';
  c_tenant_slug      constant text := 'dev-test';
  c_branch_code      constant text := 'LOJA-CENTRO';
  c_timezone         constant text := 'America/Guayaquil';

  -- Modulos habilitados: los mismos que Elvis Studio. 'business-mcp' es el que exige el MCP de Go
  -- (internal/tenant), y 'notifications' el que usa el flujo de WhatsApp.
  c_module_slugs constant text[] := array[
    'ai-assistant', 'appointments', 'business-mcp', 'cafeteria', 'campaigns', 'customers',
    'dashboard', 'ecommerce', 'inventory', 'multi-currency', 'notifications', 'pos',
    'reports', 'services', 'specialists', 'staff', 'workstations'
  ];

  v_tenant_id       uuid;
  v_branch_id       uuid;
  v_owner_auth_id   uuid;
  v_customer_auth_id uuid;
  v_customer_id     uuid;
  v_svc_corte_id    uuid;
  v_svc_ballage_id  uuid;
  v_phone_cc        text;
  v_phone_local     text;
  v_missing         text;
  v_count           integer;
begin
  -- ------------------------------------------------------------------------
  -- 0. Guardas. Todo lo que pueda estar mal, falla aqui y no a medias.
  -- ------------------------------------------------------------------------
  if v_specialist_auth_id = '00000000-0000-0000-0000-000000000000' then
    raise exception 'Falta editar v_specialist_auth_id con el UUID creado en Studio (paso 1).';
  end if;

  if v_test_phone_e164 = '+593000000000' or v_test_phone_e164 like '%000000%' then
    raise exception 'Falta editar v_test_phone_e164 con el telefono real de pruebas.';
  end if;

  if v_test_phone_e164 !~ '^\+[1-9][0-9]{7,14}$' then
    raise exception 'v_test_phone_e164 no esta en formato E.164 (ej: +593987654321).';
  end if;

  -- El especialista tiene que existir en auth y NO tener perfil en ningun tenant.
  if not exists (select 1 from auth.users where id = v_specialist_auth_id) then
    raise exception 'El usuario de auth % no existe. Crealo primero en Studio (paso 1).',
      v_specialist_auth_id;
  end if;

  select t.slug into v_missing
  from public.profiles p
  join public.tenants t on t.id = p.tenant_id
  where p.id = v_specialist_auth_id and t.slug <> c_tenant_slug;

  if v_missing is not null then
    raise exception
      'El usuario % ya tiene perfil en el tenant "%". profiles.id es PK: un usuario de auth solo '
      'puede pertenecer a un tenant. Usa un usuario de auth nuevo para el especialista.',
      v_specialist_auth_id, v_missing;
  end if;

  select id into v_owner_auth_id from auth.users where lower(email) = c_owner_email;
  if v_owner_auth_id is null then
    raise exception 'No existe el usuario de auth "%" (owner del tenant).', c_owner_email;
  end if;

  -- El cliente puede no tener cuenta: customers.user_id es opcional.
  select id into v_customer_auth_id from auth.users where lower(email) = c_customer_email;

  -- Twilio manda el telefono junto ("+593987654321") y customers lo guarda partido.
  v_phone_cc    := substring(v_test_phone_e164 from 1 for 4);   -- '+593'
  v_phone_local := substring(v_test_phone_e164 from 5);

  -- ------------------------------------------------------------------------
  -- 1. Tenant
  -- ------------------------------------------------------------------------
  -- created_by referencia public.global_users (admins de la plataforma), NO auth.users.
  -- elvis-studio y denty-med lo tienen a null; aqui igual, para no inventar un global_user.
  insert into public.tenants (
    slug, name, legal_name, country_code, timezone, currency_code, locale,
    status, subscription_plan, max_users, client_app_enabled
  )
  values (
    c_tenant_slug, 'Dev Test', 'Dev Test (tenant de pruebas)', 'EC', c_timezone, 'USD', 'es-EC',
    'active', 'starter', 5, true
  )
  on conflict (slug) do update
    set name         = excluded.name,
        country_code = excluded.country_code,
        timezone     = excluded.timezone,
        status       = excluded.status,
        updated_at   = now()
  returning id into v_tenant_id;

  raise notice 'tenant dev-test: %', v_tenant_id;

  -- ------------------------------------------------------------------------
  -- 2. Sucursal unica
  -- ------------------------------------------------------------------------
  insert into public.branches (
    tenant_id, name, code, city, state, country, timezone, is_main, is_active, operating_hours
  )
  values (
    v_tenant_id, 'Loja Centro', c_branch_code, 'Loja', 'Loja', 'EC', c_timezone, true, true,
    '{"monday":    {"open": "08:00", "close": "18:00"},
      "tuesday":   {"open": "08:00", "close": "18:00"},
      "wednesday": {"open": "08:00", "close": "18:00"},
      "thursday":  {"open": "08:00", "close": "18:00"},
      "friday":    {"open": "08:00", "close": "18:00"},
      "saturday":  {"open": "09:00", "close": "14:00"},
      "sunday":    null}'::jsonb
  )
  on conflict (tenant_id, code) do update
    set name       = excluded.name,
        timezone   = excluded.timezone,
        is_main    = excluded.is_main,
        is_active  = true,
        updated_at = now()
  returning id into v_branch_id;

  raise notice 'sucursal Loja Centro: %', v_branch_id;

  -- ------------------------------------------------------------------------
  -- 3. Modulos (incluye business-mcp: sin el, el MCP de Go deniega el acceso)
  -- ------------------------------------------------------------------------
  -- enabled_by tambien referencia global_users; se deja null, como en los tenants existentes.
  insert into public.tenant_modules (tenant_id, module_id, is_enabled)
  select v_tenant_id, m.id, true
  from public.modules m
  where m.slug = any(c_module_slugs)
  on conflict (tenant_id, module_id) do update
    set is_enabled = true,
        updated_at = now();

  select count(*) into v_count from public.tenant_modules where tenant_id = v_tenant_id;
  raise notice 'modulos habilitados: % de %', v_count, array_length(c_module_slugs, 1);

  -- Si algun slug de la lista no existe en public.modules, lo decimos en vez de habilitar de menos
  -- en silencio.
  select string_agg(s, ', ') into v_missing
  from unnest(c_module_slugs) as s
  where not exists (select 1 from public.modules m where m.slug = s);

  if v_missing is not null then
    raise warning 'Estos modulos no existen en public.modules y no se habilitaron: %', v_missing;
  end if;

  if not exists (
    select 1 from public.tenant_modules tm
    join public.modules m on m.id = tm.module_id
    where tm.tenant_id = v_tenant_id and m.slug = 'business-mcp' and tm.is_enabled
  ) then
    raise exception 'El modulo business-mcp no quedo habilitado: el MCP denegaria el acceso.';
  end if;

  -- ------------------------------------------------------------------------
  -- 4. Owner. Es lo unico que necesita el MCP: internal/repository/postgres/mcp_access.go
  --    resuelve el tenant por mcp_connections + tenant_users, no por profiles.
  -- ------------------------------------------------------------------------
  insert into public.tenant_users (
    auth_user_id, tenant_id, email, full_name, role, is_active, position, city
  )
  values (
    v_owner_auth_id, v_tenant_id, c_owner_email, 'Javier Calva', 'owner', true,
    'Owner (pruebas)', 'Loja'
  )
  on conflict (auth_user_id, tenant_id) do update
    set role       = 'owner',
        is_active  = true,
        updated_at = now();

  raise notice 'owner % dado de alta', c_owner_email;

  -- ------------------------------------------------------------------------
  -- 5. Especialista: clon de los datos del Miguel de Elvis sobre el auth user nuevo
  -- ------------------------------------------------------------------------
  insert into public.profiles (
    id, tenant_id, branch_id, full_name, email, role, is_specialist, specialties,
    is_active, city, commission_type, commission_percentage
  )
  select
    v_specialist_auth_id, v_tenant_id, v_branch_id, 'Miguel', u.email, 'employee', true,
    array['hair_cutting', 'hair_styling'], true, 'Loja', 'percentage', 30
  from auth.users u
  where u.id = v_specialist_auth_id
  on conflict (id) do update
    set tenant_id     = excluded.tenant_id,
        branch_id     = excluded.branch_id,
        full_name     = excluded.full_name,
        is_specialist = true,
        specialties   = excluded.specialties,
        is_active     = true,
        updated_at    = now();

  raise notice 'especialista Miguel: %', v_specialist_auth_id;

  -- ------------------------------------------------------------------------
  -- 6. Servicios: los dos que Miguel tiene asignados en Elvis Studio
  -- ------------------------------------------------------------------------
  insert into public.services (
    tenant_id, name, slug, description, category, duration_minutes, buffer_minutes,
    base_price, currency_code, requires_specialist, requires_station, is_active, sort_order
  )
  values (
    v_tenant_id, 'Corte de cabello', 'corte-de-cabello', 'Corte de cabello (tenant de pruebas)',
    'hair', 30, 0, 5.00, 'USD', true, true, true, 1
  )
  on conflict (tenant_id, slug) do update
    set duration_minutes = excluded.duration_minutes,
        base_price       = excluded.base_price,
        is_active        = true,
        updated_at       = now()
  returning id into v_svc_corte_id;

  insert into public.services (
    tenant_id, name, slug, description, category, duration_minutes, buffer_minutes,
    base_price, currency_code, requires_specialist, requires_station, is_active, sort_order
  )
  values (
    v_tenant_id, 'Ballage', 'ballage', 'Ballage (tenant de pruebas)',
    'hair', 30, 5, 25.00, 'USD', true, true, true, 2
  )
  on conflict (tenant_id, slug) do update
    set duration_minutes = excluded.duration_minutes,
        buffer_minutes   = excluded.buffer_minutes,
        base_price       = excluded.base_price,
        is_active        = true,
        updated_at       = now()
  returning id into v_svc_ballage_id;

  raise notice 'servicios: corte=% ballage=%', v_svc_corte_id, v_svc_ballage_id;

  insert into public.specialist_services (tenant_id, specialist_id, service_id, skill_level, is_active)
  values
    (v_tenant_id, v_specialist_auth_id, v_svc_corte_id,   4, true),
    (v_tenant_id, v_specialist_auth_id, v_svc_ballage_id, 4, true)
  on conflict (specialist_id, service_id) do update
    set is_active = true;

  -- ------------------------------------------------------------------------
  -- 7. Horario del especialista: lunes a viernes 09:00-18:00 con pausa 13:00-14:00.
  --    Hace falta para que list_available_slots y CheckSlot devuelvan huecos.
  -- ------------------------------------------------------------------------
  insert into public.specialist_schedules (
    tenant_id, specialist_id, branch_id, day_of_week, start_time, end_time,
    break_start, break_end, is_active
  )
  select
    v_tenant_id, v_specialist_auth_id, v_branch_id, d::public.day_of_week,
    '09:00'::time, '18:00'::time, '13:00'::time, '14:00'::time, true
  from unnest(array['monday', 'tuesday', 'wednesday', 'thursday', 'friday']) as d
  on conflict (specialist_id, branch_id, day_of_week) do update
    set start_time  = excluded.start_time,
        end_time    = excluded.end_time,
        break_start = excluded.break_start,
        break_end   = excluded.break_end,
        is_active   = true;

  -- ------------------------------------------------------------------------
  -- 8. Puestos de trabajo (los servicios tienen requires_station = true)
  -- ------------------------------------------------------------------------
  insert into public.workstations (tenant_id, branch_id, name, code, station_type, is_active)
  values
    (v_tenant_id, v_branch_id, 'Puesto 1', 'LOJA-01', 'general', true),
    (v_tenant_id, v_branch_id, 'Puesto 2', 'LOJA-02', 'general', true)
  on conflict (tenant_id, branch_id, code) do update
    set name       = excluded.name,
        is_active  = true,
        updated_at = now();

  -- ------------------------------------------------------------------------
  -- 9. Cliente unico, con WhatsApp activado (lo exige el flujo de la Fase 7)
  -- ------------------------------------------------------------------------
  insert into public.customers (
    tenant_id, first_name, last_name, full_name, email, phone_country_code, phone,
    preferred_branch_id, preferred_specialist_id, user_id,
    notify_email, notify_whatsapp, notify_sms, accepts_marketing,
    preferred_language, preferred_currency, city, is_active
  )
  values (
    v_tenant_id, 'Venerable', 'Pseudo', 'Venerable Pseudo', c_customer_email,
    v_phone_cc, v_phone_local, v_branch_id, v_specialist_auth_id, v_customer_auth_id,
    true, true, false, false,
    'es', 'USD', 'Loja', true
  )
  on conflict (tenant_id, email) do update
    set phone_country_code = excluded.phone_country_code,
        phone              = excluded.phone,
        notify_whatsapp    = true,
        user_id            = excluded.user_id,
        is_active          = true,
        updated_at         = now()
  returning id into v_customer_id;

  raise notice 'cliente: % (whatsapp activado)', v_customer_id;

  -- ------------------------------------------------------------------------
  -- 10. Resumen
  -- ------------------------------------------------------------------------
  raise notice '--------------------------------------------------';
  raise notice 'tenant_id    %', v_tenant_id;
  raise notice 'branch_id    %', v_branch_id;
  raise notice 'owner        % (%)', c_owner_email, v_owner_auth_id;
  raise notice 'especialista Miguel (%)', v_specialist_auth_id;
  raise notice 'cliente      % (%)', c_customer_email, v_customer_id;
  raise notice 'Siguiente: crea una cita futura y ejecuta verify_dev_test_tenant.sql';
  raise notice '--------------------------------------------------';
end
$$;
