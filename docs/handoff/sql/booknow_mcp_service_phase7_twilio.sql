-- booknow-mcp-service: WhatsApp replies and outgoing notifications (Go service, phase 7).
--
-- Least privilege, same approach as phase 6: the service never updates appointments directly.
-- A customer's WhatsApp reply is applied by one security definer function that, in a single
-- transaction, deduplicates by MessageSid, re-checks that the appointment was notified to that
-- customer by WhatsApp, changes its status and records the response.
--
-- Contents:
--   1. public.twilio_webhook_events: one row per inbound message (dedup + trace, masked phone only).
--   2. public.apply_twilio_whatsapp_reply(...): the only write path from a reply to an appointment.
--   3. Grants to read notifications (tenant resolution, decision D8.a) and to log outgoing ones (7.7).
--   4. Column grants to build outgoing WhatsApp and email notifications (7.7 and 7.8).
--   5. Index for the "WhatsApp notifications in the last hours" lookup.
--   6. Daily purge of old webhook events (30 days).

-- 1. Inbound webhook events ------------------------------------------------------------------------

create table if not exists public.twilio_webhook_events (
  message_sid       text primary key
                    check (message_sid ~ '^(SM|MM)[0-9a-f]{32}$'),
  tenant_id         uuid references public.tenants (id) on delete cascade,
  appointment_id    uuid references public.appointments (id) on delete set null,
  -- Only the masked sender as produced by pii.MaskPhone (e.g. +593******321), never the raw phone
  -- or the message text. At most 3 leading and 3 trailing digits can appear.
  from_phone_masked text
                    check (from_phone_masked is null or from_phone_masked ~ '^(\+[0-9]{3})?\*+([0-9]{3})?$'),
  -- Business number the message was sent to (ours, not personal data). Prepares decision D8.b.
  to_phone          text
                    check (to_phone is null or to_phone ~ '^\+[1-9][0-9]{7,14}$'),
  intent            text not null check (intent in ('confirm', 'cancel', 'unknown')),
  result            text not null
                    check (result in ('applied', 'already_in_state', 'not_applicable', 'ignored', 'unresolved')),
  -- Stable reason code for not_applicable (e.g. appointment_past), never free text.
  result_detail     text check (result_detail is null or result_detail ~ '^[a-z_]{1,64}$'),
  received_at       timestamptz not null default now(),
  processed_at      timestamptz not null default now()
);

comment on table public.twilio_webhook_events is
  'Inbound Twilio WhatsApp messages processed by booknow-mcp-service: deduplication by MessageSid and outcome. No message text, masked phone only.';

create index if not exists idx_twilio_webhook_events_tenant
  on public.twilio_webhook_events (tenant_id, received_at desc);

create index if not exists idx_twilio_webhook_events_received_at
  on public.twilio_webhook_events (received_at);

-- Not exposed through the Data API: RLS on and no policies; only the function below writes here.
alter table public.twilio_webhook_events enable row level security;
revoke all on table public.twilio_webhook_events from public, anon, authenticated;

-- 2. Apply a reply ---------------------------------------------------------------------------------
--
-- Go resolves tenant, customer and appointment from the notification sent to that phone (D8.a) and
-- parses the intent; this function trusts none of it blindly:
--   - it only acts if that appointment was notified by WhatsApp to that customer, in that tenant,
--     within p_window (the resolution rule, re-checked with a fresh snapshot);
--   - it locks the appointment row, so a "yes" and a "no" arriving together are applied in order;
--   - a MessageSid already seen returns the stored outcome with duplicate = true and does nothing.
--
-- Result: {"duplicate": bool, "result": ..., "result_detail": ..., "appointment_status": ...}
create or replace function public.apply_twilio_whatsapp_reply(
  p_message_sid       text,
  p_from_phone_masked text,
  p_to_phone          text,
  p_intent            text,
  p_tenant_id         uuid,
  p_customer_id       uuid,
  p_appointment_id    uuid,
  p_window            interval
)
returns jsonb
language plpgsql
security definer
set search_path = ''
as $$
declare
  v_existing public.twilio_webhook_events%rowtype;
  v_status   public.appointment_status;
  v_starts   timestamptz;
  v_result   text;
  v_detail   text;
  v_new      public.appointment_status;
begin
  if p_intent is null or p_intent not in ('confirm', 'cancel', 'unknown') then
    raise exception 'INVALID_INTENT: intent must be confirm, cancel or unknown.';
  end if;

  if p_window is null or p_window < interval '1 hour' or p_window > interval '7 days' then
    raise exception 'INVALID_WINDOW: window must be between 1 hour and 7 days.';
  end if;

  if (p_tenant_id is null) <> (p_customer_id is null) or (p_tenant_id is null) <> (p_appointment_id is null) then
    raise exception 'INVALID_RESOLUTION: tenant, customer and appointment go together.';
  end if;

  -- Deduplication. A concurrent insert of the same MessageSid waits here until the first commits.
  insert into public.twilio_webhook_events (message_sid, from_phone_masked, to_phone, intent, result)
  values (p_message_sid, p_from_phone_masked, p_to_phone, p_intent, 'unresolved')
  on conflict (message_sid) do nothing;

  if not found then
    select * into v_existing from public.twilio_webhook_events where message_sid = p_message_sid;

    return jsonb_build_object(
      'duplicate', true,
      'result', v_existing.result,
      'result_detail', v_existing.result_detail,
      'appointment_status', null
    );
  end if;

  if p_tenant_id is null then
    v_result := 'unresolved';
  elsif p_intent = 'unknown' then
    v_result := 'ignored';
  elsif not exists (
    select 1
    from public.notifications n
    where n.tenant_id = p_tenant_id
      and n.recipient_type = 'customer'
      and n.recipient_id = p_customer_id
      and n.reference_type = 'appointment'
      and n.reference_id = p_appointment_id
      and n.channel = 'whatsapp'
      and n.notification_type <> 'appointment_response'
      and n.status in ('sent', 'delivered', 'read')
      and n.created_at > now() - p_window
  ) then
    v_result := 'not_applicable';
    v_detail := 'not_notified';
  else
    select a.status, a.scheduled_at
    into v_status, v_starts
    from public.appointments a
    where a.id = p_appointment_id
      and a.tenant_id = p_tenant_id
      and a.customer_id = p_customer_id
    for update;

    if not found then
      v_result := 'not_applicable';
      v_detail := 'appointment_not_found';
    elsif v_starts <= now() then
      v_result := 'not_applicable';
      v_detail := 'appointment_past';
    elsif p_intent = 'confirm' and v_status = 'confirmed'
       or p_intent = 'cancel' and v_status = 'cancelled' then
      v_result := 'already_in_state';
    elsif v_status not in ('pending', 'confirmed') then
      v_result := 'not_applicable';
      v_detail := 'status_' || v_status::text;
    elsif p_intent = 'confirm' then
      update public.appointments
      set status = 'confirmed', confirmed_at = now()
      where id = p_appointment_id;

      v_new := 'confirmed';
      v_result := 'applied';
    else
      update public.appointments
      set status = 'cancelled', cancelled_at = now(), cancellation_reason = 'Cancelada por el cliente por WhatsApp'
      where id = p_appointment_id;

      v_new := 'cancelled';
      v_result := 'applied';
    end if;

    if v_result = 'applied' then
      insert into public.notifications (
        tenant_id, recipient_type, recipient_id, notification_type, title, message,
        channel, status, sent_at, reference_type, reference_id
      )
      values (
        p_tenant_id, 'customer', p_customer_id, 'appointment_response',
        'Respuesta por WhatsApp',
        case when v_new = 'confirmed' then 'El cliente confirmó la cita por WhatsApp.'
             else 'El cliente canceló la cita por WhatsApp.' end,
        'whatsapp', 'delivered', now(), 'appointment', p_appointment_id
      );
    end if;
  end if;

  update public.twilio_webhook_events
  set tenant_id = p_tenant_id,
      appointment_id = p_appointment_id,
      result = v_result,
      result_detail = v_detail,
      processed_at = now()
  where message_sid = p_message_sid;

  return jsonb_build_object(
    'duplicate', false,
    'result', v_result,
    'result_detail', v_detail,
    'appointment_status', coalesce(v_new, v_status)
  );
end;
$$;

comment on function public.apply_twilio_whatsapp_reply(text, text, text, text, uuid, uuid, uuid, interval) is
  'Applies a customer WhatsApp reply (confirm/cancel) to the appointment notified to them: deduplicates by MessageSid, re-checks the notification, locks the appointment and records the response.';

-- Security definer in public is exposed through the Data API: only the Go service may run it.
revoke all on function public.apply_twilio_whatsapp_reply(text, text, text, text, uuid, uuid, uuid, interval)
  from public, anon, authenticated, service_role;
grant execute on function public.apply_twilio_whatsapp_reply(text, text, text, text, uuid, uuid, uuid, interval)
  to booknow_mcp_service;

-- 3. Notifications -------------------------------------------------------------------------------

-- Tenant resolution (D8.a). No title or message: they may carry personal data.
grant select (
  id, tenant_id, recipient_type, recipient_id, notification_type, channel, status,
  reference_type, reference_id, sent_at, created_at
) on table public.notifications to booknow_mcp_service;

-- Log of outgoing notifications (7.7). delivered_at and read_at stay out (status callbacks, D12).
grant insert (
  tenant_id, recipient_type, recipient_id, notification_type, title, message, channel, status,
  sent_at, reference_type, reference_id, error_message
) on table public.notifications to booknow_mcp_service;

-- 4. Data to build outgoing notifications (7.7 WhatsApp, 7.8 email + .ics) ------------------------

-- Customer contact and preferences. Addresses, documents and private notes stay out.
grant select (email, notify_email, notify_whatsapp, preferred_language)
  on table public.customers to booknow_mcp_service;

-- Specialist email, to send them the appointment. Phone, address and commissions stay out.
grant select (email) on table public.profiles to booknow_mcp_service;

-- Branch contact shown in the email and the .ics location.
grant select (address, city, phone, email) on table public.branches to booknow_mcp_service;

-- Workstation name shown to the specialist.
grant select (id, tenant_id, branch_id, name, is_active) on table public.workstations to booknow_mcp_service;

-- 5. Index for the resolution lookup ----------------------------------------------------------------

create index if not exists idx_notifications_whatsapp_appointment_recent
  on public.notifications (created_at desc)
  where channel = 'whatsapp' and reference_type = 'appointment';

-- 6. Retention -------------------------------------------------------------------------------------

create or replace function public.purge_twilio_webhook_events()
returns integer
language plpgsql
security definer
set search_path = ''
as $$
declare
  v_deleted integer;
begin
  -- Twilio retries within minutes; 30 days is enough for deduplication and troubleshooting.
  delete from public.twilio_webhook_events
  where received_at < now() - interval '30 days';

  get diagnostics v_deleted = row_count;
  return v_deleted;
end;
$$;

comment on function public.purge_twilio_webhook_events() is
  'Deletes Twilio webhook events older than 30 days. Run daily by pg_cron.';

revoke all on function public.purge_twilio_webhook_events() from public, anon, authenticated, service_role;

-- Daily at 03:27 UTC, ten minutes after the MCP audit purge.
select cron.schedule('purge-twilio-webhook-events', '27 3 * * *', 'select public.purge_twilio_webhook_events()');
