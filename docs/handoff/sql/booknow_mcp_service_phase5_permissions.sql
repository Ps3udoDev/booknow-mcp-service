-- booknow-mcp-service: permissions for the read tools, audit and connection usage (Go MCP phase 5),
-- plus daily purge of MCP audit rows according to each tenant's retention (7-90 days, default 90).
--
-- Least privilege: column-level SELECT on tables that hold personal data, so the service cannot read
-- private notes, addresses, documents, contact data of staff or billing fields even by mistake.

-- 1. Track connection usage (throttled by the service). Only this column is writable.
grant update (last_used_at) on table public.mcp_connections to booknow_mcp_service;

-- 2. Audit: append-only. No SELECT, UPDATE or DELETE on audit rows.
grant insert (
  tenant_id, connection_id, actor_auth_user_id, oauth_client_id, request_id, tool_name,
  risk_level, arguments_hash, safe_summary, status, error_code, duration_ms
) on table public.mcp_tool_calls to booknow_mcp_service;

-- 3. Read tools (get_business_snapshot, get_schedule_summary, list_available_slots,
--    list_appointments, search_customers). internal_notes, cancellation data and payment fields excluded.
grant select (
  id, tenant_id, branch_id, customer_id, specialist_id, workstation_id, service_id, service_variant_id,
  scheduled_at, ends_at, duration_minutes, status, estimated_price, currency_code, customer_notes,
  source, created_at
) on table public.appointments to booknow_mcp_service;

-- Customers: identity and phone only (phone is always masked before reaching the LLM).
grant select (
  id, tenant_id, first_name, last_name, full_name, phone, phone_country_code, is_active, created_at
) on table public.customers to booknow_mcp_service;

-- Staff: public-facing name and specialist flags only (no email, phone, address or commissions).
grant select (
  id, tenant_id, branch_id, full_name, is_specialist, is_active
) on table public.profiles to booknow_mcp_service;

grant select (
  id, tenant_id, name, timezone, operating_hours, is_main, is_active
) on table public.branches to booknow_mcp_service;

grant select (
  id, tenant_id, name, category, duration_minutes, buffer_minutes, base_price, currency_code,
  requires_specialist, is_active
) on table public.services to booknow_mcp_service;

grant select (
  id, tenant_id, specialist_id, branch_id, day_of_week, start_time, end_time, break_start, break_end, is_active
) on table public.specialist_schedules to booknow_mcp_service;

-- schedule_exceptions has no tenant_id: the service scopes it through specialist/branch. "reason" excluded.
grant select (
  id, specialist_id, branch_id, exception_date, exception_type, start_time, end_time, is_day_off
) on table public.schedule_exceptions to booknow_mcp_service;

-- 4. Audit retention purge.
-- Supabase grants postgres access to the cron schema when the extension is created; the extra
-- GRANTs from the Supabase install guide are redundant here and make re-runs fail, so they are omitted.
create extension if not exists pg_cron with schema pg_catalog;

create or replace function public.purge_mcp_tool_calls()
returns integer
language plpgsql
security definer
set search_path = ''
as $$
declare
  v_deleted integer;
begin
  -- Retention per tenant from tenant_modules.config.audit_retention_days (business-mcp),
  -- clamped to 7..90; missing or invalid values fall back to 90.
  delete from public.mcp_tool_calls c
  using (
    select t.id as tenant_id,
           coalesce((
             select least(greatest((tm.config ->> 'audit_retention_days')::integer, 7), 90)
             from public.tenant_modules tm
             join public.modules m on m.id = tm.module_id
             where tm.tenant_id = t.id
               and m.slug = 'business-mcp'
               and (tm.config ->> 'audit_retention_days') ~ '^[0-9]{1,4}$'
           ), 90) as retention_days
    from public.tenants t
  ) r
  where c.tenant_id = r.tenant_id
    and c.created_at < now() - make_interval(days => r.retention_days);

  get diagnostics v_deleted = row_count;
  return v_deleted;
end;
$$;

comment on function public.purge_mcp_tool_calls() is
  'Deletes MCP audit rows older than each tenant''s audit_retention_days (7-90, default 90). Run daily by pg_cron.';

-- Functions in public are exposed through the Data API: only the scheduler (postgres) may run this.
revoke all on function public.purge_mcp_tool_calls() from public, anon, authenticated, service_role;

-- Daily at 03:17 UTC. cron.schedule updates the existing job when the name already exists.
select cron.schedule('purge-mcp-tool-calls', '17 3 * * *', 'select public.purge_mcp_tool_calls()');
