-- Cluster roles the remote schema snapshot references but `supabase db dump` does not include.
-- Loaded by the Supabase CLI before migrations on `supabase start` / `supabase db reset`.
-- Local only: production roles are managed by migrations in the Next.js repo; no password here.
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'booknow_mcp_service') then
    create role booknow_mcp_service with login bypassrls noinherit;
  end if;
end
$$;

-- Local only: lets integration tests run store queries as the service role (SET LOCAL ROLE) to verify
-- that production grants are sufficient. Never granted in production.
grant booknow_mcp_service to postgres with set true;
