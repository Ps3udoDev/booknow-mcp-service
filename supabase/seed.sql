-- Loaded by the Supabase CLI after the schema snapshot on `supabase db reset`. Local only.
--
-- The dump only emits `REVOKE ... FROM PUBLIC` for these functions, but when it is replayed locally the
-- Supabase default privileges grant EXECUTE to anon and authenticated again. Production has them revoked
-- (Next.js migrations 20260915170457 and 20260915212242); mirror that so local tests see the same ACLs.
revoke all on function public.confirm_mcp_appointment_draft(uuid, uuid) from public, anon, authenticated;
revoke all on function public.purge_mcp_tool_calls() from public, anon, authenticated, service_role;
