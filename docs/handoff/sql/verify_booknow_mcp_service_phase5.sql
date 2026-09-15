\set ON_ERROR_STOP 1
begin;
\i :migration
\i :migration
\echo '=== 1. idempotencia (2 ejecuciones OK). Jobs con ese nombre:'
select count(*) as jobs, max(schedule) as schedule, max(command) as command from cron.job where jobname = 'purge-mcp-tool-calls';

\echo '=== 2. permisos esperados (todos_true debe ser t)'
select bool_and(ok) as todos_true, string_agg(case when not ok then chk end, ', ') as fallan from (values
  ('upd last_used_at', has_column_privilege('booknow_mcp_service','public.mcp_connections','last_used_at','UPDATE')),
  ('ins tool_calls', has_column_privilege('booknow_mcp_service','public.mcp_tool_calls','safe_summary','INSERT')),
  ('sel appt customer_notes', has_column_privilege('booknow_mcp_service','public.appointments','customer_notes','SELECT')),
  ('sel customers phone', has_column_privilege('booknow_mcp_service','public.customers','phone','SELECT')),
  ('sel profiles full_name', has_column_privilege('booknow_mcp_service','public.profiles','full_name','SELECT')),
  ('sel branches hours', has_column_privilege('booknow_mcp_service','public.branches','operating_hours','SELECT')),
  ('sel services price', has_column_privilege('booknow_mcp_service','public.services','base_price','SELECT')),
  ('sel schedules', has_column_privilege('booknow_mcp_service','public.specialist_schedules','start_time','SELECT')),
  ('sel exceptions', has_column_privilege('booknow_mcp_service','public.schedule_exceptions','is_day_off','SELECT'))
) v(chk, ok);

\echo '=== 3. permisos prohibidos (alguno_true debe ser f)'
select bool_or(ok) as alguno_true, string_agg(case when ok then chk end, ', ') as sobran from (values
  ('upd connections status', has_column_privilege('booknow_mcp_service','public.mcp_connections','status','UPDATE')),
  ('sel tool_calls', has_table_privilege('booknow_mcp_service','public.mcp_tool_calls','SELECT')),
  ('upd tool_calls', has_table_privilege('booknow_mcp_service','public.mcp_tool_calls','UPDATE')),
  ('del tool_calls', has_table_privilege('booknow_mcp_service','public.mcp_tool_calls','DELETE')),
  ('sel appt internal_notes', has_column_privilege('booknow_mcp_service','public.appointments','internal_notes','SELECT')),
  ('sel appt advance_amount', has_column_privilege('booknow_mcp_service','public.appointments','advance_amount','SELECT')),
  ('ins appointments', has_table_privilege('booknow_mcp_service','public.appointments','INSERT')),
  ('sel customers notes', has_column_privilege('booknow_mcp_service','public.customers','notes','SELECT')),
  ('sel customers address', has_column_privilege('booknow_mcp_service','public.customers','address','SELECT')),
  ('sel customers document', has_column_privilege('booknow_mcp_service','public.customers','document_number','SELECT')),
  ('sel customers email', has_column_privilege('booknow_mcp_service','public.customers','email','SELECT')),
  ('sel profiles email', has_column_privilege('booknow_mcp_service','public.profiles','email','SELECT')),
  ('sel profiles commission', has_column_privilege('booknow_mcp_service','public.profiles','commission_percentage','SELECT')),
  ('sel branches phone', has_column_privilege('booknow_mcp_service','public.branches','phone','SELECT')),
  ('sel exceptions reason', has_column_privilege('booknow_mcp_service','public.schedule_exceptions','reason','SELECT')),
  ('exec purge service', has_function_privilege('booknow_mcp_service','public.purge_mcp_tool_calls()','EXECUTE')),
  ('exec purge anon', has_function_privilege('anon','public.purge_mcp_tool_calls()','EXECUTE')),
  ('exec purge authenticated', has_function_privilege('authenticated','public.purge_mcp_tool_calls()','EXECUTE')),
  ('exec purge service_role', has_function_privilege('service_role','public.purge_mcp_tool_calls()','EXECUTE'))
) v(chk, ok);

\echo '=== 4. purga con retenciones distintas'
insert into auth.users (id) values ('aaaaaaaa-0000-4000-8000-000000000001');
insert into public.modules (slug, name) values ('business-mcp', 'x') on conflict (slug) do nothing;
insert into public.tenants (id, slug, name, status) values
  ('10000000-0000-4000-8000-000000000007', 'r7',   'r7',   'active'),
  ('10000000-0000-4000-8000-000000000090', 'rdef', 'rdef', 'active'),
  ('10000000-0000-4000-8000-000000000999', 'rbad', 'rbad', 'active'),
  ('10000000-0000-4000-8000-000000000500', 'rbig', 'rbig', 'active'),
  ('10000000-0000-4000-8000-000000000003', 'rlow', 'rlow', 'active');
insert into public.tenant_modules (tenant_id, module_id, config)
select v.t::uuid, m.id, v.c::jsonb from public.modules m, (values
  ('10000000-0000-4000-8000-000000000007', '{"audit_retention_days": 7}'),
  ('10000000-0000-4000-8000-000000000999', '{"audit_retention_days": "abc"}'),
  ('10000000-0000-4000-8000-000000000500', '{"audit_retention_days": 500}'),
  ('10000000-0000-4000-8000-000000000003', '{"audit_retention_days": 3}')
) v(t, c) where m.slug = 'business-mcp';
insert into public.mcp_tool_calls (tenant_id, actor_auth_user_id, oauth_client_id, request_id, tool_name, status, created_at)
select t.id, 'aaaaaaaa-0000-4000-8000-000000000001', 'c', 'req-' || a.d, 'health', 'succeeded', now() - make_interval(days => a.d)
from public.tenants t cross join (values (2), (8), (50), (100)) a(d)
where t.slug in ('r7', 'rdef', 'rbad', 'rbig', 'rlow');

select public.purge_mcp_tool_calls() as filas_borradas;
\echo 'esperado: r7 [2] | rdef, rbad, rbig [2,8,50] | rlow [2] (minimo 7 dias); filas_borradas = 9'
select t.slug, array_agg(extract(day from now() - c.created_at)::int order by c.created_at desc) as dias_restantes
from public.mcp_tool_calls c join public.tenants t on t.id = c.tenant_id
where t.slug in ('r7', 'rdef', 'rbad', 'rbig', 'rlow') group by t.slug order by t.slug;

\echo '=== 5. consultas reales como booknow_mcp_service'
grant booknow_mcp_service to postgres with set true;
set local role booknow_mcp_service;
select count(*) as appts from public.appointments where tenant_id = '10000000-0000-4000-8000-000000000007' and scheduled_at > now() - interval '1 day';
select count(*) as customers from public.customers where tenant_id = '10000000-0000-4000-8000-000000000007' and (first_name ilike '%ana%' or phone ilike '%123%');
select count(*) as slots_data from public.specialist_schedules s join public.branches b on b.id = s.branch_id where b.tenant_id = '10000000-0000-4000-8000-000000000007' and s.is_active;
insert into public.mcp_tool_calls (tenant_id, actor_auth_user_id, oauth_client_id, request_id, tool_name, status)
values ('10000000-0000-4000-8000-000000000007', 'aaaaaaaa-0000-4000-8000-000000000001', 'c', 'req-role', 'health', 'succeeded');
\echo 'insert de auditoria como el rol: OK'
\echo '--- los siguientes 5 deben fallar con permission denied'
\set ON_ERROR_STOP 0
savepoint s1;
select notes from public.customers limit 1;
rollback to savepoint s1;
savepoint s2;
select * from public.customers limit 1;
rollback to savepoint s2;
savepoint s3;
select id from public.mcp_tool_calls limit 1;
rollback to savepoint s3;
savepoint s4;
update public.mcp_connections set status = 'revoked' where false;
rollback to savepoint s4;
savepoint s5;
select public.purge_mcp_tool_calls();
rollback to savepoint s5;
rollback;
