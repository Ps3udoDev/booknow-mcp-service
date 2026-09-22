\set ON_ERROR_STOP 1
begin;
\i :migration
\i :migration
\echo '=== 1. idempotencia: la migracion se aplico 2 veces sin error'

create temp table chk (n serial, seccion int, chk text, ok boolean, detalle text);
grant all on chk to public;
grant all on sequence chk_n_seq to public;

-- 2. Expected privileges, function settings and objects.
insert into chk (seccion, chk, ok) values
  (2, 'exec apply service', has_function_privilege('booknow_mcp_service','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  (2, 'apply security definer', (select prosecdef from pg_proc where oid = 'public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)'::regprocedure)),
  (2, 'apply search_path vacio', (select proconfig = array['search_path=""'] from pg_proc where oid = 'public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)'::regprocedure)),
  (2, 'purge security definer', (select prosecdef and proconfig = array['search_path=""'] from pg_proc where oid = 'public.purge_twilio_webhook_events()'::regprocedure)),
  (2, 'rls en twilio_webhook_events', (select relrowsecurity from pg_class where oid = 'public.twilio_webhook_events'::regclass)),
  (2, 'sin politicas en twilio_webhook_events', not exists (select 1 from pg_policies where tablename = 'twilio_webhook_events')),
  (2, 'cron purge-twilio-webhook-events', exists (select 1 from cron.job where jobname = 'purge-twilio-webhook-events' and schedule = '27 3 * * *')),
  (2, 'indice notifications whatsapp', to_regclass('public.idx_notifications_whatsapp_appointment_recent') is not null),
  (2, 'sel notifications reference_id', has_column_privilege('booknow_mcp_service','public.notifications','reference_id','SELECT')),
  (2, 'sel notifications notification_type', has_column_privilege('booknow_mcp_service','public.notifications','notification_type','SELECT')),
  (2, 'ins notifications error_message', has_column_privilege('booknow_mcp_service','public.notifications','error_message','INSERT')),
  (2, 'sel customers email', has_column_privilege('booknow_mcp_service','public.customers','email','SELECT')),
  (2, 'sel customers notify_whatsapp', has_column_privilege('booknow_mcp_service','public.customers','notify_whatsapp','SELECT')),
  (2, 'sel profiles email', has_column_privilege('booknow_mcp_service','public.profiles','email','SELECT')),
  (2, 'sel branches address', has_column_privilege('booknow_mcp_service','public.branches','address','SELECT')),
  (2, 'sel workstations name', has_column_privilege('booknow_mcp_service','public.workstations','name','SELECT'));

-- 3. Forbidden privileges (ok = the privilege is absent).
insert into chk (seccion, chk, ok) values
  (3, 'exec apply public', not has_function_privilege('public','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  (3, 'exec apply anon', not has_function_privilege('anon','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  (3, 'exec apply authenticated', not has_function_privilege('authenticated','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  (3, 'exec apply service_role', not has_function_privilege('service_role','public.apply_twilio_whatsapp_reply(text,text,text,text,uuid,uuid,uuid,interval)','EXECUTE')),
  (3, 'exec purge service', not has_function_privilege('booknow_mcp_service','public.purge_twilio_webhook_events()','EXECUTE')),
  (3, 'exec purge anon', not has_function_privilege('anon','public.purge_twilio_webhook_events()','EXECUTE')),
  (3, 'events: service sin select', not has_any_column_privilege('booknow_mcp_service','public.twilio_webhook_events','SELECT')),
  (3, 'events: service sin insert', not has_any_column_privilege('booknow_mcp_service','public.twilio_webhook_events','INSERT')),
  (3, 'events: service sin update', not has_any_column_privilege('booknow_mcp_service','public.twilio_webhook_events','UPDATE')),
  (3, 'events: service sin delete', not has_table_privilege('booknow_mcp_service','public.twilio_webhook_events','DELETE')),
  (3, 'events: anon sin select', not has_table_privilege('anon','public.twilio_webhook_events','SELECT')),
  (3, 'events: authenticated sin select', not has_table_privilege('authenticated','public.twilio_webhook_events','SELECT')),
  (3, 'sel notifications message', not has_column_privilege('booknow_mcp_service','public.notifications','message','SELECT')),
  (3, 'sel notifications title', not has_column_privilege('booknow_mcp_service','public.notifications','title','SELECT')),
  (3, 'upd notifications', not has_any_column_privilege('booknow_mcp_service','public.notifications','UPDATE')),
  (3, 'del notifications', not has_table_privilege('booknow_mcp_service','public.notifications','DELETE')),
  (3, 'ins notifications delivered_at', not has_column_privilege('booknow_mcp_service','public.notifications','delivered_at','INSERT')),
  (3, 'upd appointments', not has_any_column_privilege('booknow_mcp_service','public.appointments','UPDATE')),
  (3, 'sel appt internal_notes', not has_column_privilege('booknow_mcp_service','public.appointments','internal_notes','SELECT')),
  (3, 'sel appt cancellation_reason', not has_column_privilege('booknow_mcp_service','public.appointments','cancellation_reason','SELECT')),
  (3, 'sel customers address', not has_column_privilege('booknow_mcp_service','public.customers','address','SELECT')),
  (3, 'sel customers notes', not has_column_privilege('booknow_mcp_service','public.customers','notes','SELECT')),
  (3, 'sel customers document_number', not has_column_privilege('booknow_mcp_service','public.customers','document_number','SELECT')),
  (3, 'sel profiles phone', not has_column_privilege('booknow_mcp_service','public.profiles','phone','SELECT')),
  (3, 'sel profiles commission_percentage', not has_column_privilege('booknow_mcp_service','public.profiles','commission_percentage','SELECT')),
  (3, 'upd customers', not has_any_column_privilege('booknow_mcp_service','public.customers','UPDATE'));

-- 4. Fixtures (as postgres). Tenant A and tenant B share the same customer phone on purpose.
--    a1 pending, notified 1 h ago          a5 pending, notified 3 days ago (outside 48 h)
--    a2 confirmed, notified                 a6 tenant B, notified to customer B
--    a3 pending, never notified             a7 completed, notified
--    a4 pending in the past, notified       a8 pending, only an appointment_response row
insert into public.tenants (id, slug, name, status) values
  ('70000000-0000-4000-8000-00000000000a', 'phase7-a', 'phase7-a', 'active'),
  ('70000000-0000-4000-8000-00000000000b', 'phase7-b', 'phase7-b', 'active');
insert into public.branches (id, tenant_id, name, timezone, address, is_active) values
  ('70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-00000000000a', 'Sede A', 'America/Guayaquil', 'Calle 1', true),
  ('70000000-0000-4000-8000-0000000000c2', '70000000-0000-4000-8000-00000000000b', 'Sede B', 'America/Guayaquil', 'Calle 2', true);
insert into public.services (id, tenant_id, name, slug, duration_minutes, buffer_minutes, base_price, currency_code, is_active) values
  ('70000000-0000-4000-8000-0000000000d1', '70000000-0000-4000-8000-00000000000a', 'Corte', 'phase7-corte-a', 30, 0, 5, 'USD', true),
  ('70000000-0000-4000-8000-0000000000d2', '70000000-0000-4000-8000-00000000000b', 'Corte', 'phase7-corte-b', 30, 0, 5, 'USD', true);
insert into public.customers (id, tenant_id, first_name, last_name, full_name, phone_country_code, phone, email, address) values
  ('70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-00000000000a', 'Ana', 'A', 'Ana A', '+593', '991234567', 'ana@example.test', 'direccion privada'),
  ('70000000-0000-4000-8000-0000000000e2', '70000000-0000-4000-8000-00000000000b', 'Ana', 'B', 'Ana B', '+593', '991234567', 'anab@example.test', null);
insert into public.workstations (id, tenant_id, branch_id, name, code) values
  ('70000000-0000-4000-8000-0000000000f1', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', 'Puesto 1', 'P1');

insert into public.appointments (id, tenant_id, branch_id, customer_id, service_id, scheduled_at, duration_minutes, status)
select ('70000000-0000-4000-8000-0000000001a' || v.k)::uuid, v.t::uuid, v.b::uuid, v.c::uuid, v.s::uuid,
       now() + v.at, 30, v.st::public.appointment_status
from (values
  ('1', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-0000000000d1', interval '2 days', 'pending'),
  ('2', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-0000000000d1', interval '3 days', 'confirmed'),
  ('3', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-0000000000d1', interval '4 days', 'pending'),
  ('4', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-0000000000d1', interval '-1 hour', 'pending'),
  ('5', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-0000000000d1', interval '5 days', 'pending'),
  ('6', '70000000-0000-4000-8000-00000000000b', '70000000-0000-4000-8000-0000000000c2', '70000000-0000-4000-8000-0000000000e2', '70000000-0000-4000-8000-0000000000d2', interval '2 days', 'pending'),
  ('7', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-0000000000d1', interval '6 days', 'completed'),
  ('8', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000c1', '70000000-0000-4000-8000-0000000000e1', '70000000-0000-4000-8000-0000000000d1', interval '7 days', 'pending')
) v(k, t, b, c, s, at, st);

insert into public.notifications (tenant_id, recipient_type, recipient_id, notification_type, title, message, channel, status, reference_type, reference_id, created_at)
select v.t::uuid, 'customer', v.c::uuid, v.nt, 'titulo con datos', 'mensaje con datos', 'whatsapp', 'sent', 'appointment',
       ('70000000-0000-4000-8000-0000000001a' || v.k)::uuid, now() - v.ago
from (values
  ('1', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000e1', 'appointment_confirmation', interval '1 hour'),
  ('2', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000e1', 'appointment_confirmation', interval '2 hours'),
  ('4', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000e1', 'appointment_confirmation', interval '3 hours'),
  ('5', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000e1', 'appointment_confirmation', interval '3 days'),
  ('6', '70000000-0000-4000-8000-00000000000b', '70000000-0000-4000-8000-0000000000e2', 'appointment_confirmation', interval '30 minutes'),
  ('7', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000e1', 'appointment_confirmation', interval '4 hours'),
  ('8', '70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-0000000000e1', 'appointment_response', interval '1 hour')
) v(k, t, c, nt, ago);

grant booknow_mcp_service to postgres with set true;
set local role booknow_mcp_service;

-- Calls the function as the Go service will; returns the result or the error message.
create function pg_temp.reply(p_n int, p_intent text, p_tenant text, p_customer text, p_appt text,
                              p_window interval default interval '48 hours', p_masked text default '+593******567',
                              out result jsonb, out err text)
language plpgsql as $$
begin
  result := public.apply_twilio_whatsapp_reply(
    'SM' || lpad(p_n::text, 32, '0'), p_masked, '+14155238886', p_intent,
    p_tenant::uuid, p_customer::uuid, ('70000000-0000-4000-8000-0000000001a' || p_appt)::uuid, p_window);
exception when others then
  err := sqlerrm;
end $$;

do $$
declare
  ta constant text := '70000000-0000-4000-8000-00000000000a';
  tb constant text := '70000000-0000-4000-8000-00000000000b';
  ca constant text := '70000000-0000-4000-8000-0000000000e1';
  cb constant text := '70000000-0000-4000-8000-0000000000e2';
  r record;
  n integer;
begin
  -- Resolution query of decision D8.a, as the role, with only granted columns.
  select count(*) into n from (
    select n.tenant_id, n.reference_id, n.recipient_id
    from public.notifications n
    join public.customers c on c.id = n.recipient_id and c.tenant_id = n.tenant_id
    where n.channel = 'whatsapp' and n.recipient_type = 'customer' and n.reference_type = 'appointment'
      and n.notification_type <> 'appointment_response'
      and n.status in ('sent', 'delivered', 'read')
      and n.created_at > now() - interval '48 hours'
      and c.phone_country_code || regexp_replace(c.phone, '\D', '', 'g') = '+593991234567'
  ) q;
  insert into chk (seccion, chk, ok, detalle) values (4, 'rol: consulta de resolucion D8.a (5 en ventana, 2 tenants)', n = 5, n::text);

  r := pg_temp.reply(1, 'confirm', ta, ca, '1');
  insert into chk (seccion, chk, ok, detalle) values (4, 'confirmar a1: applied',
    r.err is null and r.result ->> 'result' = 'applied' and r.result ->> 'appointment_status' = 'confirmed'
      and (r.result ->> 'duplicate')::boolean = false, coalesce(r.err, r.result::text));

  r := pg_temp.reply(1, 'cancel', ta, ca, '1');
  insert into chk (seccion, chk, ok, detalle) values (4, 'mismo MessageSid: duplicate, no cancela',
    r.err is null and (r.result ->> 'duplicate')::boolean and r.result ->> 'result' = 'applied', coalesce(r.err, r.result::text));

  r := pg_temp.reply(2, 'confirm', ta, ca, '1');
  insert into chk (seccion, chk, ok, detalle) values (4, 'confirmar a1 otra vez: already_in_state',
    r.result ->> 'result' = 'already_in_state', coalesce(r.err, r.result::text));

  r := pg_temp.reply(3, 'cancel', ta, ca, '2');
  insert into chk (seccion, chk, ok, detalle) values (4, 'cancelar a2 (confirmed): applied',
    r.result ->> 'result' = 'applied' and r.result ->> 'appointment_status' = 'cancelled', coalesce(r.err, r.result::text));

  r := pg_temp.reply(4, 'cancel', ta, ca, '2');
  insert into chk (seccion, chk, ok, detalle) values (4, 'cancelar a2 otra vez: already_in_state',
    r.result ->> 'result' = 'already_in_state', coalesce(r.err, r.result::text));

  r := pg_temp.reply(5, 'confirm', ta, ca, '3');
  insert into chk (seccion, chk, ok, detalle) values (4, 'a3 sin notificar: not_notified',
    r.result ->> 'result' = 'not_applicable' and r.result ->> 'result_detail' = 'not_notified', coalesce(r.err, r.result::text));

  r := pg_temp.reply(6, 'confirm', ta, ca, '4');
  insert into chk (seccion, chk, ok, detalle) values (4, 'a4 pasada: appointment_past',
    r.result ->> 'result_detail' = 'appointment_past', coalesce(r.err, r.result::text));

  r := pg_temp.reply(7, 'confirm', ta, ca, '5');
  insert into chk (seccion, chk, ok, detalle) values (4, 'a5 notificada hace 3 dias con ventana 48 h: not_notified',
    r.result ->> 'result_detail' = 'not_notified', coalesce(r.err, r.result::text));

  r := pg_temp.reply(8, 'cancel', ta, ca, '6');
  insert into chk (seccion, chk, ok, detalle) values (4, 'cita de tenant B con tenant A: not_notified',
    r.result ->> 'result_detail' = 'not_notified', coalesce(r.err, r.result::text));

  r := pg_temp.reply(9, 'cancel', tb, ca, '6');
  insert into chk (seccion, chk, ok, detalle) values (4, 'tenant B con cliente de A: not_notified',
    r.result ->> 'result_detail' = 'not_notified', coalesce(r.err, r.result::text));

  r := pg_temp.reply(10, 'cancel', ta, ca, '7');
  insert into chk (seccion, chk, ok, detalle) values (4, 'a7 completed: status_completed',
    r.result ->> 'result_detail' = 'status_completed', coalesce(r.err, r.result::text));

  r := pg_temp.reply(11, 'confirm', ta, ca, '8');
  insert into chk (seccion, chk, ok, detalle) values (4, 'a8 solo con appointment_response: not_notified',
    r.result ->> 'result_detail' = 'not_notified', coalesce(r.err, r.result::text));

  r := pg_temp.reply(12, 'unknown', ta, ca, '3');
  insert into chk (seccion, chk, ok, detalle) values (4, 'intencion unknown: ignored',
    r.result ->> 'result' = 'ignored', coalesce(r.err, r.result::text));

  select * into r from pg_temp.reply(13, 'confirm', null, null, null) x;
  r := pg_temp.reply(13, 'confirm', null, null, null);
  insert into chk (seccion, chk, ok, detalle) values (4, 'sin resolver: unresolved (y el reintento es duplicate)',
    r.result ->> 'result' = 'unresolved' and (r.result ->> 'duplicate')::boolean, coalesce(r.err, r.result::text));

  r := pg_temp.reply(14, 'si', ta, ca, '3');
  insert into chk (seccion, chk, ok, detalle) values (4, 'intencion invalida: INVALID_INTENT', r.err like 'INVALID_INTENT:%', r.err);

  r := pg_temp.reply(15, 'confirm', ta, ca, '3', interval '30 days');
  insert into chk (seccion, chk, ok, detalle) values (4, 'ventana de 30 dias: INVALID_WINDOW', r.err like 'INVALID_WINDOW:%', r.err);

  r := pg_temp.reply(16, 'confirm', ta, null, '3');
  insert into chk (seccion, chk, ok, detalle) values (4, 'resolucion parcial: INVALID_RESOLUTION', r.err like 'INVALID_RESOLUTION:%', r.err);

  r := pg_temp.reply(17, 'confirm', ta, ca, '3', interval '48 hours', '+593991234567');
  insert into chk (seccion, chk, ok, detalle) values (4, 'telefono sin enmascarar: rechazado por el check', r.err like '%from_phone_masked%', r.err);

  begin
    perform public.apply_twilio_whatsapp_reply('SMnothex', null, null, 'confirm', null, null, null, interval '48 hours');
    insert into chk (seccion, chk, ok) values (4, 'MessageSid invalido: rechazado', false);
  exception when check_violation then
    insert into chk (seccion, chk, ok) values (4, 'MessageSid invalido: rechazado', true);
  end;

  -- Outgoing notification log as 7.7 will write it.
  insert into public.notifications (tenant_id, recipient_type, recipient_id, notification_type, title, message, channel, status, sent_at, reference_type, reference_id)
  values (ta::uuid, 'customer', ca::uuid, 'appointment_confirmation', 'Confirmacion de cita', 'WhatsApp enviado', 'whatsapp', 'sent', now(), 'appointment', '70000000-0000-4000-8000-0000000001a3');
  insert into chk (seccion, chk, ok) values (4, 'rol: insert en notifications (7.7)', true);

  select count(*) into n from (
    select c.email, c.notify_email, c.notify_whatsapp, c.preferred_language, p.email as specialist_email,
           b.address, b.city, b.phone, b.email as branch_email, w.name
    from public.customers c
    join public.branches b on b.tenant_id = c.tenant_id
    left join public.workstations w on w.branch_id = b.id
    left join public.profiles p on p.tenant_id = c.tenant_id
    where c.id = ca::uuid
  ) q;
  insert into chk (seccion, chk, ok, detalle) values (4, 'rol: lectura de datos para avisos (7.7/7.8)', n >= 1, n::text);
end $$;

reset role;

-- Final state checked as postgres (the role cannot read these tables or columns).
insert into chk (seccion, chk, ok, detalle)
select 4, 'estados finales a1..a8',
       string_agg(right(id::text, 1) || '=' || status, ',' order by id) = '1=confirmed,2=cancelled,3=pending,4=pending,5=pending,6=pending,7=completed,8=pending',
       string_agg(right(id::text, 1) || '=' || status, ',' order by id)
from public.appointments where tenant_id in ('70000000-0000-4000-8000-00000000000a', '70000000-0000-4000-8000-00000000000b');

insert into chk (seccion, chk, ok, detalle)
select 4, 'a1 confirmed_at y a2 cancelled_at + motivo',
       bool_and(case right(id::text, 1)
                  when '1' then confirmed_at is not null and cancelled_at is null
                  when '2' then cancelled_at is not null and cancellation_reason = 'Cancelada por el cliente por WhatsApp'
                end), null
from public.appointments where id in ('70000000-0000-4000-8000-0000000001a1', '70000000-0000-4000-8000-0000000001a2');

insert into chk (seccion, chk, ok, detalle)
select 4, 'exactamente 2 appointment_response nuevas y sin PII',
       count(*) = 2 and bool_and(message not like '%+593%' and message not like '%991234567%' and status = 'delivered'), count(*)::text
from public.notifications
where notification_type = 'appointment_response' and tenant_id = '70000000-0000-4000-8000-00000000000a' and created_at > now() - interval '1 minute';

insert into chk (seccion, chk, ok, detalle)
select 4, 'eventos: 13 filas, resultados esperados, sin telefono en claro',
       count(*) = 13
         and string_agg(right(message_sid, 2) || '=' || result || coalesce(':' || result_detail, ''), ',' order by message_sid) =
             '01=applied,02=already_in_state,03=applied,04=already_in_state,05=not_applicable:not_notified,06=not_applicable:appointment_past,07=not_applicable:not_notified,08=not_applicable:not_notified,09=not_applicable:not_notified,10=not_applicable:status_completed,11=not_applicable:not_notified,12=ignored,13=unresolved'
         and bool_and(from_phone_masked = '+593******567'),
       string_agg(right(message_sid, 2) || '=' || result || coalesce(':' || result_detail, ''), ',' order by message_sid)
from public.twilio_webhook_events;

-- 5. Denied operations (ok = insufficient_privilege).
create function pg_temp.denied(p_sql text) returns boolean
language plpgsql as $$
begin
  execute p_sql;
  return false;
exception when insufficient_privilege then
  return true;
end $$;

set local role booknow_mcp_service;
insert into chk (seccion, chk, ok) values
  (5, 'rol: update appointments', pg_temp.denied($q$update public.appointments set status = 'cancelled' where false$q$)),
  (5, 'rol: select message de notifications', pg_temp.denied($q$select message from public.notifications limit 1$q$)),
  (5, 'rol: update notifications', pg_temp.denied($q$update public.notifications set status = 'read' where false$q$)),
  (5, 'rol: delete notifications', pg_temp.denied($q$delete from public.notifications where false$q$)),
  (5, 'rol: select twilio_webhook_events', pg_temp.denied($q$select message_sid from public.twilio_webhook_events limit 1$q$)),
  (5, 'rol: insert twilio_webhook_events', pg_temp.denied($q$insert into public.twilio_webhook_events (message_sid, intent, result) select 'x', 'unknown', 'ignored' where false$q$)),
  (5, 'rol: delete twilio_webhook_events', pg_temp.denied($q$delete from public.twilio_webhook_events where false$q$)),
  (5, 'rol: ejecutar purge', pg_temp.denied($q$select public.purge_twilio_webhook_events()$q$)),
  (5, 'rol: select address de customers', pg_temp.denied($q$select address from public.customers limit 1$q$)),
  (5, 'rol: select phone de profiles', pg_temp.denied($q$select phone from public.profiles limit 1$q$));
reset role;

set local role anon;
insert into chk (seccion, chk, ok) values
  (5, 'anon: ejecutar apply', pg_temp.denied($q$select public.apply_twilio_whatsapp_reply('x', null, null, 'unknown', null, null, null, interval '1 day')$q$)),
  (5, 'anon: select twilio_webhook_events', pg_temp.denied($q$select 1 from public.twilio_webhook_events limit 1$q$));
reset role;
set local role authenticated;
insert into chk (seccion, chk, ok) values
  (5, 'authenticated: ejecutar apply', pg_temp.denied($q$select public.apply_twilio_whatsapp_reply('x', null, null, 'unknown', null, null, null, interval '1 day')$q$)),
  (5, 'authenticated: select twilio_webhook_events', pg_temp.denied($q$select 1 from public.twilio_webhook_events limit 1$q$));
reset role;

-- Purge (as postgres): only events older than 30 days go away.
insert into public.twilio_webhook_events (message_sid, intent, result, received_at)
values ('SM' || lpad('99', 32, '0'), 'unknown', 'ignored', now() - interval '31 days');
select public.purge_twilio_webhook_events() as purged \gset
insert into chk (seccion, chk, ok, detalle)
select 5, 'purge borra solo eventos de mas de 30 dias', :purged = 1 and count(*) = 13, :purged || ' borrados, quedan ' || count(*)
from public.twilio_webhook_events;

\echo '=== resultado por seccion (todas deben tener fallan = 0)'
select seccion, count(*) as checks, count(*) filter (where not coalesce(ok, false)) as fallan from chk group by seccion order by seccion;
\echo '=== checks que fallan (debe estar vacio)'
select seccion, chk, detalle from chk where not coalesce(ok, false) order by n;
rollback;
