-- booknow-mcp-service: permissions for the two-step appointment writes (Go MCP phase 6)
-- and hardening of public.confirm_mcp_appointment_draft.
--
-- Least privilege: the service can create drafts and read them back, but cannot update or delete
-- drafts, set their status, or write appointments directly. Appointments are only created by the
-- confirmation function, which runs as its owner (security definer).

-- 0. Allow source = 'mcp'. The confirmation function has always inserted it, but the check constraint
--    never listed it, so no MCP draft could ever be confirmed. Same values plus 'mcp'.
alter table public.appointments drop constraint if exists appointments_source_check;
alter table public.appointments add constraint appointments_source_check
  check (source = any (array['web', 'app', 'phone', 'walk_in', 'whatsapp', 'instagram', 'mcp'])) not valid;
alter table public.appointments validate constraint appointments_source_check;

-- 1. Service variants: pricing and duration modifiers only (no description).
grant select (
  id, tenant_id, service_id, name, duration_modifier, price_modifier, is_active
) on table public.service_variants to booknow_mcp_service;

-- 2. Drafts: read back (idempotent retries, tenant check before confirming) and insert.
--    status defaults to 'draft'; confirmation columns are only written by the function.
grant select (
  id, tenant_id, connection_id, actor_auth_user_id, customer_id, service_id, service_variant_id,
  branch_id, specialist_id, scheduled_at, ends_at, duration_minutes, estimated_price, currency_code,
  customer_notes, status, idempotency_key, expires_at, confirmed_appointment_id, confirmed_at, created_at
) on table public.mcp_appointment_drafts to booknow_mcp_service;

grant insert (
  tenant_id, connection_id, actor_auth_user_id, customer_id, service_id, service_variant_id,
  branch_id, specialist_id, scheduled_at, ends_at, duration_minutes, estimated_price, currency_code,
  customer_notes, idempotency_key, expires_at
) on table public.mcp_appointment_drafts to booknow_mcp_service;

-- 3. Confirmation function. Same signature, result shape and error prefixes as before, so the
--    Next.js MCP (service_role) keeps working until the cutover. Changes:
--    - search_path = '' (every object is schema-qualified).
--    - The actor is always checked against the draft's tenant, also on idempotent replays.
--    - Confirmations for the same specialist are serialized with a transaction advisory lock, so two
--      different overlapping drafts cannot both become appointments.
--    - The appointment is returned with an explicit list of fields (no internal notes, payments or
--      cancellation data).
--    - The expired status update was rolled back by the exception that followed it; it is removed
--      (expires_at alone defines expiry).
create or replace function public.confirm_mcp_appointment_draft(p_draft_id uuid, p_actor_auth_user_id uuid)
returns jsonb
language plpgsql
security definer
set search_path = ''
as $$
declare
  v_draft public.mcp_appointment_drafts%rowtype;
  v_appointment_id uuid;
  v_idempotent boolean := false;
begin
  -- 1. Lock the draft: concurrent confirmations of the same draft run one after another.
  select *
  into v_draft
  from public.mcp_appointment_drafts
  where id = p_draft_id
  for update;

  if not found then
    raise exception 'DRAFT_NOT_FOUND: El borrador de cita no existe.';
  end if;

  -- 2. The actor must be an active owner, admin or manager of the draft's tenant.
  if not exists (
    select 1
    from public.tenant_users tu
    where tu.tenant_id = v_draft.tenant_id
      and tu.auth_user_id = p_actor_auth_user_id
      and coalesce(tu.is_active, true) = true
      and tu.role in ('owner', 'admin', 'manager')
  ) then
    raise exception 'UNAUTHORIZED: No tienes permisos para confirmar este borrador.';
  end if;

  if v_draft.status = 'confirmed' and v_draft.confirmed_appointment_id is not null then
    -- 3. Idempotency: return the appointment created by the first confirmation.
    v_appointment_id := v_draft.confirmed_appointment_id;
    v_idempotent := true;
  else
    if v_draft.status <> 'draft' then
      raise exception 'INVALID_DRAFT_STATUS: El borrador no está en estado draft (estado actual: %).', v_draft.status;
    end if;

    if v_draft.expires_at < now() then
      raise exception 'DRAFT_EXPIRED: El borrador de cita ha expirado.';
    end if;

    -- 4. Serialize confirmations per specialist, then re-check overlaps with a fresh snapshot.
    if v_draft.specialist_id is not null then
      perform pg_advisory_xact_lock(
        hashtextextended('booknow:mcp-confirm:specialist:' || v_draft.specialist_id::text, 0)
      );

      if exists (
        select 1
        from public.appointments a
        where a.tenant_id = v_draft.tenant_id
          and a.specialist_id = v_draft.specialist_id
          and a.status in ('pending', 'confirmed', 'in_progress')
          and tstzrange(a.scheduled_at, a.ends_at, '[)') && tstzrange(v_draft.scheduled_at, v_draft.ends_at, '[)')
      ) then
        raise exception 'SPECIALIST_UNAVAILABLE: El especialista ya no está disponible en este horario.';
      end if;
    end if;

    -- 5. Create the appointment as pending with source = 'mcp'.
    insert into public.appointments (
      tenant_id, branch_id, customer_id, specialist_id, service_id, service_variant_id,
      scheduled_at, ends_at, duration_minutes, status, customer_notes, estimated_price, currency_code, source
    )
    values (
      v_draft.tenant_id, v_draft.branch_id, v_draft.customer_id, v_draft.specialist_id, v_draft.service_id,
      v_draft.service_variant_id, v_draft.scheduled_at, v_draft.ends_at, v_draft.duration_minutes,
      'pending'::public.appointment_status, v_draft.customer_notes, v_draft.estimated_price,
      v_draft.currency_code, 'mcp'
    )
    returning id into v_appointment_id;

    insert into public.appointment_services (
      appointment_id, service_id, service_variant_id, specialist_id, duration_minutes, price
    )
    values (
      v_appointment_id, v_draft.service_id, v_draft.service_variant_id, v_draft.specialist_id,
      v_draft.duration_minutes, v_draft.estimated_price
    );

    update public.mcp_appointment_drafts
    set status = 'confirmed',
        confirmed_appointment_id = v_appointment_id,
        confirmed_at = now(),
        updated_at = now()
    where id = p_draft_id;
  end if;

  return jsonb_build_object(
    'success', true,
    'idempotent', v_idempotent,
    'appointment', (
      select jsonb_build_object(
        'id', a.id,
        'tenant_id', a.tenant_id,
        'branch_id', a.branch_id,
        'customer_id', a.customer_id,
        'specialist_id', a.specialist_id,
        'service_id', a.service_id,
        'service_variant_id', a.service_variant_id,
        'scheduled_at', a.scheduled_at,
        'ends_at', a.ends_at,
        'duration_minutes', a.duration_minutes,
        'status', a.status,
        'estimated_price', a.estimated_price,
        'currency_code', a.currency_code,
        'source', a.source,
        'created_at', a.created_at
      )
      from public.appointments a
      where a.id = v_appointment_id
    )
  );
end;
$$;

comment on function public.confirm_mcp_appointment_draft(uuid, uuid) is
  'Confirma atómicamente un borrador de cita MCP: valida actor y expiración, serializa por especialista, crea la cita pending con source=mcp y es idempotente.';

-- Security definer in public is exposed through the Data API: only the MCP backends may run it.
-- service_role: Next.js MCP until the cutover. booknow_mcp_service: Go MCP.
revoke all on function public.confirm_mcp_appointment_draft(uuid, uuid) from public, anon, authenticated;
grant execute on function public.confirm_mcp_appointment_draft(uuid, uuid) to service_role, booknow_mcp_service;
