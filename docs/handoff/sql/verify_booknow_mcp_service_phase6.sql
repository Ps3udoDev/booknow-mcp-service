\set ON_ERROR_STOP 1
begin;
\i :migration
\i :migration
\echo '=== 1. idempotencia: la migracion se aplico 2 veces sin error'

create temp table chk (n serial, seccion int, chk text, ok boolean, detalle text);
grant all on chk to public;
grant all on sequence chk_n_seq to public;

-- 2. Expected privileges and function settings.
insert into chk (seccion, chk, ok) values
  (2, 'sel variants price_modifier', has_column_privilege('booknow_mcp_service','public.service_variants','price_modifier','SELECT')),
  (2, 'sel drafts status', has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','status','SELECT')),
  (2, 'sel drafts confirmed_appointment_id', has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','confirmed_appointment_id','SELECT')),
  (2, 'ins drafts idempotency_key', has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','idempotency_key','INSERT')),
  (2, 'exec confirm service', has_function_privilege('booknow_mcp_service','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  (2, 'exec confirm service_role', has_function_privilege('service_role','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  (2, 'security definer', (select prosecdef from pg_proc where oid = 'public.confirm_mcp_appointment_draft(uuid,uuid)'::regprocedure)),
  (2, 'search_path vacio', (select proconfig = array['search_path=""'] from pg_proc where oid = 'public.confirm_mcp_appointment_draft(uuid,uuid)'::regprocedure)),
  (2, 'check source admite mcp y valores previos', (select convalidated and pg_get_constraintdef(oid) = $c$CHECK ((source = ANY (ARRAY['web'::text, 'app'::text, 'phone'::text, 'walk_in'::text, 'whatsapp'::text, 'instagram'::text, 'mcp'::text])))$c$
     from pg_constraint where conrelid = 'public.appointments'::regclass and conname = 'appointments_source_check'));

-- 3. Forbidden privileges (ok = the privilege is absent).
insert into chk (seccion, chk, ok) values
  (3, 'exec confirm public', not has_function_privilege('public','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  (3, 'exec confirm anon', not has_function_privilege('anon','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  (3, 'exec confirm authenticated', not has_function_privilege('authenticated','public.confirm_mcp_appointment_draft(uuid,uuid)','EXECUTE')),
  (3, 'upd drafts (cualquier columna)', not has_any_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','UPDATE')),
  (3, 'del drafts', not has_table_privilege('booknow_mcp_service','public.mcp_appointment_drafts','DELETE')),
  (3, 'ins drafts id', not has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','id','INSERT')),
  (3, 'ins drafts status', not has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','status','INSERT')),
  (3, 'ins drafts confirmed_appointment_id', not has_column_privilege('booknow_mcp_service','public.mcp_appointment_drafts','confirmed_appointment_id','INSERT')),
  (3, 'sel variants description', not has_column_privilege('booknow_mcp_service','public.service_variants','description','SELECT')),
  (3, 'upd variants', not has_any_column_privilege('booknow_mcp_service','public.service_variants','UPDATE')),
  (3, 'ins appointments', not has_any_column_privilege('booknow_mcp_service','public.appointments','INSERT')),
  (3, 'upd appointments', not has_any_column_privilege('booknow_mcp_service','public.appointments','UPDATE')),
  (3, 'sel appt internal_notes', not has_column_privilege('booknow_mcp_service','public.appointments','internal_notes','SELECT')),
  (3, 'ins appointment_services', not has_any_column_privilege('booknow_mcp_service','public.appointment_services','INSERT')),
  (3, 'upd connections status', not has_column_privilege('booknow_mcp_service','public.mcp_connections','status','UPDATE'));

-- 4. Fixtures (as postgres): tenant A (admin, manager, employee, inactive admin, specialist) and tenant B (admin).
insert into auth.users (id) values
  ('60000000-0000-4000-8000-0000000000a1'), ('60000000-0000-4000-8000-0000000000a2'),
  ('60000000-0000-4000-8000-0000000000a3'), ('60000000-0000-4000-8000-0000000000a4'),
  ('60000000-0000-4000-8000-0000000000a5'), ('60000000-0000-4000-8000-0000000000b1');
insert into public.tenants (id, slug, name, status) values
  ('60000000-0000-4000-8000-00000000000a', 'phase6-a', 'phase6-a', 'active'),
  ('60000000-0000-4000-8000-00000000000b', 'phase6-b', 'phase6-b', 'active');
insert into public.tenant_users (tenant_id, auth_user_id, email, full_name, role, is_active) values
  ('60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000a1', 'a1@example.test', 'A1', 'admin', true),
  ('60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000a2', 'a2@example.test', 'A2', 'manager', true),
  ('60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000a3', 'a3@example.test', 'A3', 'employee', true),
  ('60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000a4', 'a4@example.test', 'A4', 'admin', false),
  ('60000000-0000-4000-8000-00000000000b', '60000000-0000-4000-8000-0000000000b1', 'b1@example.test', 'B1', 'admin', true);
insert into public.profiles (id, tenant_id, full_name, email, is_specialist, is_active) values
  ('60000000-0000-4000-8000-0000000000a5', '60000000-0000-4000-8000-00000000000a', 'Especialista A', 'a5@example.test', true, true);
insert into public.branches (id, tenant_id, name, timezone, is_active) values
  ('60000000-0000-4000-8000-0000000000c1', '60000000-0000-4000-8000-00000000000a', 'Sede A', 'America/Guayaquil', true);
insert into public.services (id, tenant_id, name, slug, duration_minutes, buffer_minutes, base_price, currency_code, requires_specialist, is_active) values
  ('60000000-0000-4000-8000-0000000000d1', '60000000-0000-4000-8000-00000000000a', 'Corte', 'phase6-corte', 60, 0, 20, 'USD', true, true);
insert into public.service_variants (id, tenant_id, service_id, name, description, duration_modifier, price_modifier) values
  ('60000000-0000-4000-8000-0000000000d2', '60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000d1', 'Largo', 'descripcion interna', 15, 5);
insert into public.customers (id, tenant_id, first_name, last_name, full_name, phone) values
  ('60000000-0000-4000-8000-0000000000e1', '60000000-0000-4000-8000-00000000000a', 'Ana', 'Test', 'Ana Test', '+593991234567');
insert into public.mcp_connections (id, tenant_id, auth_user_id, oauth_client_id, status) values
  ('60000000-0000-4000-8000-0000000000f1', '60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000a1', 'phase6-client', 'active');

grant booknow_mcp_service to postgres with set true;
set local role booknow_mcp_service;

-- Drafts created exactly as the Go service will: explicit columns, ON CONFLICT DO NOTHING, RETURNING.
-- d1 10:00-11:00, d2 10:30 (overlaps d1), d3 11:00-12:00 (touches d1), d4 expired, d5 12:00.
insert into public.mcp_appointment_drafts (
  tenant_id, connection_id, actor_auth_user_id, customer_id, service_id, service_variant_id, branch_id, specialist_id,
  scheduled_at, ends_at, duration_minutes, estimated_price, currency_code, customer_notes, idempotency_key, expires_at
)
select '60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000f1', '60000000-0000-4000-8000-0000000000a1',
       '60000000-0000-4000-8000-0000000000e1', '60000000-0000-4000-8000-0000000000d1', '60000000-0000-4000-8000-0000000000d2',
       '60000000-0000-4000-8000-0000000000c1', '60000000-0000-4000-8000-0000000000a5',
       date_trunc('day', now()) + interval '1 day' + v.h, date_trunc('day', now()) + interval '1 day' + v.h + interval '60 minutes',
       60, 25, 'USD', 'nota del cliente', v.k, now() + v.ttl
from (values
  ('d1', interval '10 hours', interval '10 minutes'),
  ('d2', interval '10 hours 30 minutes', interval '10 minutes'),
  ('d3', interval '11 hours', interval '10 minutes'),
  ('d4', interval '14 hours', interval '-1 minute'),
  ('d5', interval '12 hours', interval '10 minutes')
) v(k, h, ttl)
on conflict (connection_id, idempotency_key) do nothing
returning id;

do $$
declare
  inserted integer;
  total integer;
begin
  -- Retry with the same keys: no new rows.
  insert into public.mcp_appointment_drafts (
    tenant_id, connection_id, actor_auth_user_id, customer_id, service_id, branch_id,
    scheduled_at, ends_at, duration_minutes, idempotency_key, expires_at
  )
  values ('60000000-0000-4000-8000-00000000000a', '60000000-0000-4000-8000-0000000000f1', '60000000-0000-4000-8000-0000000000a1',
          '60000000-0000-4000-8000-0000000000e1', '60000000-0000-4000-8000-0000000000d1', '60000000-0000-4000-8000-0000000000c1',
          now() + interval '1 day', now() + interval '1 day 1 hour', 60, 'd1', now() + interval '10 minutes')
  on conflict (connection_id, idempotency_key) do nothing;
  get diagnostics inserted = row_count;
  select count(*) into total from public.mcp_appointment_drafts
  where connection_id = '60000000-0000-4000-8000-0000000000f1';
  insert into chk (seccion, chk, ok, detalle) values (4, 'reintento con la misma key no duplica',
    inserted = 0 and total = 5, format('insertadas=%s total=%s', inserted, total));
end $$;

-- Confirmation helper for the checks below: returns the error message or the result.
create function pg_temp.try_confirm(p_key text, p_actor uuid, out result jsonb, out err text)
language plpgsql as $$
declare
  v_id uuid;
begin
  select id into v_id from public.mcp_appointment_drafts
  where connection_id = '60000000-0000-4000-8000-0000000000f1' and idempotency_key = p_key;
  begin
    result := public.confirm_mcp_appointment_draft(coalesce(v_id, gen_random_uuid()), p_actor);
  exception when others then
    err := sqlerrm;
  end;
end $$;

do $$
declare
  admin_a constant uuid := '60000000-0000-4000-8000-0000000000a1';
  r1 record;
  r2 record;
  r3 record;
  r record;
begin
  r1 := pg_temp.try_confirm('d1', admin_a);
  insert into chk (seccion, chk, ok, detalle) values (4, 'confirmar d1: cita pending source=mcp, no idempotente',
    r1.err is null and (r1.result ->> 'idempotent')::boolean = false
      and r1.result #>> '{appointment,status}' = 'pending' and r1.result #>> '{appointment,source}' = 'mcp',
    coalesce(r1.err, r1.result::text));
  insert into chk (seccion, chk, ok, detalle) values (4, 'resultado sin campos sensibles (15 claves, sin internal_notes)',
    (select count(*) from jsonb_object_keys(r1.result -> 'appointment')) = 15
      and not (r1.result -> 'appointment') ?| array['internal_notes', 'customer_notes', 'advance_amount', 'cancellation_reason'],
    (select string_agg(k, ',') from jsonb_object_keys(r1.result -> 'appointment') k));

  r2 := pg_temp.try_confirm('d1', admin_a);
  insert into chk (seccion, chk, ok, detalle) values (4, 'doble confirmacion de d1: idempotente, misma cita',
    r2.err is null and (r2.result ->> 'idempotent')::boolean and r2.result #>> '{appointment,id}' = r1.result #>> '{appointment,id}',
    coalesce(r2.err, r2.result::text));

  r3 := pg_temp.try_confirm('d1', '60000000-0000-4000-8000-0000000000a2');
  insert into chk (seccion, chk, ok, detalle) values (4, 'manager del tenant repite d1: idempotente',
    r3.err is null and (r3.result ->> 'idempotent')::boolean, coalesce(r3.err, r3.result::text));

  r := pg_temp.try_confirm('d2', admin_a);
  insert into chk (seccion, chk, ok, detalle) values (4, 'd2 solapado con d1: SPECIALIST_UNAVAILABLE', r.err like 'SPECIALIST_UNAVAILABLE:%', r.err);

  r := pg_temp.try_confirm('d3', admin_a);
  insert into chk (seccion, chk, ok, detalle) values (4, 'd3 contiguo a d1: se confirma', r.err is null, r.err);

  r := pg_temp.try_confirm('d4', admin_a);
  insert into chk (seccion, chk, ok, detalle) values (4, 'd4 expirado: DRAFT_EXPIRED', r.err like 'DRAFT_EXPIRED:%', r.err);

  r := pg_temp.try_confirm('d5', '60000000-0000-4000-8000-0000000000a3');
  insert into chk (seccion, chk, ok, detalle) values (4, 'employee: UNAUTHORIZED', r.err like 'UNAUTHORIZED:%', r.err);

  r := pg_temp.try_confirm('d5', '60000000-0000-4000-8000-0000000000a4');
  insert into chk (seccion, chk, ok, detalle) values (4, 'admin inactivo: UNAUTHORIZED', r.err like 'UNAUTHORIZED:%', r.err);

  r := pg_temp.try_confirm('d5', '60000000-0000-4000-8000-0000000000b1');
  insert into chk (seccion, chk, ok, detalle) values (4, 'admin de otro tenant: UNAUTHORIZED', r.err like 'UNAUTHORIZED:%', r.err);

  r := pg_temp.try_confirm('d1', '60000000-0000-4000-8000-0000000000b1');
  insert into chk (seccion, chk, ok, detalle) values (4, 'admin de otro tenant repite d1: UNAUTHORIZED', r.err like 'UNAUTHORIZED:%', r.err);

  r := pg_temp.try_confirm('no-existe', admin_a);
  insert into chk (seccion, chk, ok, detalle) values (4, 'draft inexistente: DRAFT_NOT_FOUND', r.err like 'DRAFT_NOT_FOUND:%', r.err);

  insert into chk (seccion, chk, ok, detalle)
  select 4, 'estados finales d1..d5', string_agg(idempotency_key || '=' || status, ',' order by idempotency_key) = 'd1=confirmed,d2=draft,d3=confirmed,d4=draft,d5=draft',
         string_agg(idempotency_key || '=' || status, ',' order by idempotency_key)
  from public.mcp_appointment_drafts where connection_id = '60000000-0000-4000-8000-0000000000f1';
end $$;

-- 5. Denied operations (ok = insufficient_privilege).
create function pg_temp.denied(p_sql text) returns boolean
language plpgsql as $$
begin
  execute p_sql;
  return false;
exception when insufficient_privilege then
  return true;
end $$;

insert into chk (seccion, chk, ok) values
  (5, 'rol: update status de drafts', pg_temp.denied($q$update public.mcp_appointment_drafts set status = 'confirmed' where false$q$)),
  (5, 'rol: delete drafts', pg_temp.denied($q$delete from public.mcp_appointment_drafts where false$q$)),
  (5, 'rol: insert draft con status', pg_temp.denied($q$insert into public.mcp_appointment_drafts (status) select 'confirmed' where false$q$)),
  (5, 'rol: insert appointments', pg_temp.denied($q$insert into public.appointments (tenant_id) select null where false$q$)),
  (5, 'rol: select description de variantes', pg_temp.denied($q$select description from public.service_variants limit 1$q$)),
  (5, 'rol: select updated_at de drafts', pg_temp.denied($q$select updated_at from public.mcp_appointment_drafts limit 1$q$));

reset role;
select count(*) = 2 as ok, count(*) as citas_mcp
from public.appointments where tenant_id = '60000000-0000-4000-8000-00000000000a' and source = 'mcp' \gset
insert into chk (seccion, chk, ok, detalle) values (5, 'exactamente 2 citas mcp y 2 appointment_services', :'ok'::boolean
  and (select count(*) from public.appointment_services s join public.appointments a on a.id = s.appointment_id
       where a.tenant_id = '60000000-0000-4000-8000-00000000000a') = 2, :'citas_mcp');

set local role anon;
insert into chk (seccion, chk, ok) values
  (5, 'anon: ejecutar confirm', pg_temp.denied($q$select public.confirm_mcp_appointment_draft(gen_random_uuid(), gen_random_uuid())$q$));
reset role;
set local role authenticated;
insert into chk (seccion, chk, ok) values
  (5, 'authenticated: ejecutar confirm', pg_temp.denied($q$select public.confirm_mcp_appointment_draft(gen_random_uuid(), gen_random_uuid())$q$));
reset role;

\echo '=== resultado por seccion (todas deben tener fallan = 0)'
select seccion, count(*) as checks, count(*) filter (where not coalesce(ok, false)) as fallan from chk group by seccion order by seccion;
\echo '=== checks que fallan (debe estar vacio)'
select seccion, chk, detalle from chk where not coalesce(ok, false) order by n;
rollback;
