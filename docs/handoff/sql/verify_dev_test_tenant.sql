-- Verificacion del tenant de pruebas "dev-test" (docs/handoff/sql/dev_test_tenant.sql).
-- Solo lectura: no escribe nada. Ejecutalo despues del script de alta.
--
-- Cada fila devuelve check / esperado / obtenido / ok. Todas las filas tienen que dar ok = true.
-- El telefono del cliente se devuelve SIEMPRE enmascarado (regla de PII del proyecto).

with tenant as (
  select * from public.tenants where slug = 'dev-test'
),
branch as (
  select b.* from public.branches b join tenant t on t.id = b.tenant_id
),
specialist as (
  select p.* from public.profiles p join tenant t on t.id = p.tenant_id where p.is_specialist
),
checks as (

  -- 1. El tenant existe, esta activo y en la zona horaria de Ecuador.
  select 1 as n, 'tenant activo' as prueba,
         'active / America/Guayaquil / EC' as esperado,
         coalesce((select status::text || ' / ' || timezone || ' / ' || country_code from tenant),
                  'NO EXISTE') as obtenido,
         (select status = 'active' and timezone = 'America/Guayaquil' and country_code = 'EC'
            from tenant) as ok

  -- 2. Modulo business-mcp habilitado. Sin el, internal/tenant deniega toda llamada MCP.
  union all
  select 2, 'modulo business-mcp habilitado', 'true',
         coalesce((select tm.is_enabled::text
                     from public.tenant_modules tm
                     join public.modules m on m.id = tm.module_id
                     join tenant t on t.id = tm.tenant_id
                    where m.slug = 'business-mcp'), 'SIN FILA'),
         coalesce((select tm.is_enabled
                     from public.tenant_modules tm
                     join public.modules m on m.id = tm.module_id
                     join tenant t on t.id = tm.tenant_id
                    where m.slug = 'business-mcp'), false)

  -- 3. Modulo notifications habilitado (flujo de WhatsApp de la fase 7).
  union all
  select 3, 'modulo notifications habilitado', 'true',
         coalesce((select tm.is_enabled::text
                     from public.tenant_modules tm
                     join public.modules m on m.id = tm.module_id
                     join tenant t on t.id = tm.tenant_id
                    where m.slug = 'notifications'), 'SIN FILA'),
         coalesce((select tm.is_enabled
                     from public.tenant_modules tm
                     join public.modules m on m.id = tm.module_id
                     join tenant t on t.id = tm.tenant_id
                    where m.slug = 'notifications'), false)

  -- 4. Owner con un rol que el MCP acepta (owner | admin | manager) y activo.
  union all
  select 4, 'owner con rol valido para el MCP', 'owner / activo',
         coalesce((select tu.role::text || ' / ' || (case when tu.is_active then 'activo'
                                                         else 'INACTIVO' end)
                     from public.tenant_users tu join tenant t on t.id = tu.tenant_id
                    where tu.email = 'javiercalva@teams4soft.com'), 'SIN FILA'),
         coalesce((select tu.role in ('owner', 'admin', 'manager') and tu.is_active
                     from public.tenant_users tu join tenant t on t.id = tu.tenant_id
                    where tu.email = 'javiercalva@teams4soft.com'), false)

  -- 5. El owner esta enlazado a un usuario de auth: sin auth_user_id el MCP no lo encuentra
  --    (mcp_access.go hace join por tu.auth_user_id = c.auth_user_id).
  union all
  select 5, 'owner enlazado a auth.users', 'true',
         coalesce((select (tu.auth_user_id is not null)::text
                     from public.tenant_users tu join tenant t on t.id = tu.tenant_id
                    where tu.email = 'javiercalva@teams4soft.com'), 'SIN FILA'),
         coalesce((select tu.auth_user_id is not null
                     from public.tenant_users tu join tenant t on t.id = tu.tenant_id
                    where tu.email = 'javiercalva@teams4soft.com'), false)

  -- 6. Un unico especialista, activo.
  union all
  select 6, 'un especialista activo', '1',
         (select count(*)::text from specialist where is_active),
         (select count(*) = 1 from specialist where is_active)

  -- 7. El especialista NO pertenece tambien a otro tenant (imposible por el PK, pero lo dejamos
  --    explicito porque es el error que bloqueo el diseno: profiles.id es PK y FK a auth.users).
  union all
  select 7, 'el especialista solo esta en dev-test', '1',
         coalesce((select count(*)::text from public.profiles p
                    where p.id in (select id from specialist)), '0'),
         coalesce((select count(*) = 1 from public.profiles p
                    where p.id in (select id from specialist)), false)

  -- 8. El especialista tiene los dos servicios asignados.
  union all
  select 8, 'servicios del especialista', '2',
         (select count(*)::text from public.specialist_services ss
           where ss.specialist_id in (select id from specialist) and ss.is_active),
         (select count(*) = 2 from public.specialist_services ss
           where ss.specialist_id in (select id from specialist) and ss.is_active)

  -- 9. Horario de lunes a viernes: sin el, list_available_slots no devuelve huecos.
  union all
  select 9, 'horario del especialista (L-V)', '5',
         (select count(*)::text from public.specialist_schedules ss
           where ss.specialist_id in (select id from specialist) and ss.is_active),
         (select count(*) = 5 from public.specialist_schedules ss
           where ss.specialist_id in (select id from specialist) and ss.is_active)

  -- 10. Sucursal unica y principal.
  union all
  select 10, 'sucursal principal activa', '1',
         (select count(*)::text from branch where is_main and is_active),
         (select count(*) = 1 from branch where is_main and is_active)

  -- 11. Puestos de trabajo: los servicios tienen requires_station = true.
  union all
  select 11, 'puestos de trabajo activos', '2',
         (select count(*)::text from public.workstations w join tenant t on t.id = w.tenant_id
           where w.is_active),
         (select count(*) = 2 from public.workstations w join tenant t on t.id = w.tenant_id
           where w.is_active)

  -- 12. Un unico cliente.
  union all
  select 12, 'un cliente activo', '1',
         (select count(*)::text from public.customers c join tenant t on t.id = c.tenant_id
           where c.is_active),
         (select count(*) = 1 from public.customers c join tenant t on t.id = c.tenant_id
           where c.is_active)

  -- 13. El cliente acepta WhatsApp. Sin esto el flujo de la fase 7 no le manda nada.
  union all
  select 13, 'cliente con notify_whatsapp', 'true',
         coalesce((select c.notify_whatsapp::text from public.customers c
                     join tenant t on t.id = c.tenant_id
                    where c.email = 'v.pseudo.11@gmail.com'), 'SIN FILA'),
         coalesce((select c.notify_whatsapp from public.customers c
                     join tenant t on t.id = c.tenant_id
                    where c.email = 'v.pseudo.11@gmail.com'), false)

  -- 14. Telefono en E.164 valido una vez unido country_code + phone. Es como llega el "From" de
  --     Twilio y como lo comparara la resolucion de tenant de la tarea 7.4 (decision D8.a).
  --     Se muestra enmascarado.
  union all
  select 14, 'telefono del cliente en E.164',
         'coincide con ^\+[1-9][0-9]{7,14}$',
         coalesce((select left(c.phone_country_code || c.phone, 3) || '******' ||
                          right(c.phone_country_code || c.phone, 3)
                     from public.customers c join tenant t on t.id = c.tenant_id
                    where c.email = 'v.pseudo.11@gmail.com'), 'SIN FILA'),
         coalesce((select (c.phone_country_code || c.phone) ~ '^\+[1-9][0-9]{7,14}$'
                     from public.customers c join tenant t on t.id = c.tenant_id
                    where c.email = 'v.pseudo.11@gmail.com'), false)

  -- 15. Aislamiento: ese telefono no puede estar tambien en otro tenant, o la resolucion por
  --     notificacion (D8.a) quedaria ambigua y el mensaje se descartaria como "unresolved".
  union all
  select 15, 'el telefono no se repite en otro tenant', '0',
         coalesce((select count(*)::text from public.customers c2
                    where c2.tenant_id <> (select id from tenant)
                      and c2.phone_country_code || c2.phone =
                          (select c.phone_country_code || c.phone from public.customers c
                             join tenant t on t.id = c.tenant_id
                            where c.email = 'v.pseudo.11@gmail.com')), '0'),
         coalesce((select count(*) = 0 from public.customers c2
                    where c2.tenant_id <> (select id from tenant)
                      and c2.phone_country_code || c2.phone =
                          (select c.phone_country_code || c.phone from public.customers c
                             join tenant t on t.id = c.tenant_id
                            where c.email = 'v.pseudo.11@gmail.com')), true)

  -- 16. Dos servicios activos.
  union all
  select 16, 'servicios activos del tenant', '2',
         (select count(*)::text from public.services s join tenant t on t.id = s.tenant_id
           where s.is_active),
         (select count(*) = 2 from public.services s join tenant t on t.id = s.tenant_id
           where s.is_active)
)
select n, prueba, esperado, obtenido,
       case when ok then 'ok' else '*** FALLA ***' end as resultado
from checks
order by n;

-- ---------------------------------------------------------------------------
-- Pendiente manual tras ejecutar el alta: una cita futura para probar confirmar/cancelar.
-- No se crea por script porque depende de la fecha en que pruebes. En la app, o:
--
--   insert into public.appointments (
--     tenant_id, branch_id, customer_id, specialist_id, service_id,
--     scheduled_at, ends_at, duration_minutes, status, estimated_price, currency_code, source
--   )
--   select t.id, b.id, c.id, p.id, s.id,
--          (current_date + 2) + time '10:00', (current_date + 2) + time '10:30', 30,
--          'pending', s.base_price, 'USD', 'web'
--   from public.tenants t
--   join public.branches b  on b.tenant_id = t.id
--   join public.customers c on c.tenant_id = t.id and c.email = 'v.pseudo.11@gmail.com'
--   join public.profiles p  on p.tenant_id = t.id and p.is_specialist
--   join public.services s  on s.tenant_id = t.id and s.slug = 'corte-de-cabello'
--   where t.slug = 'dev-test';
--
-- Ojo: (current_date + 2) + time '10:00' se interpreta en la zona horaria de la sesion, no en la
-- del tenant. Comprueba el resultado con:
--   select scheduled_at at time zone 'America/Guayaquil' from public.appointments ...
-- ---------------------------------------------------------------------------
