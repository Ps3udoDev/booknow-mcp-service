-- Elvis Studio: la sucursal Tariba (Táchira, Venezuela) tenía zona America/Guayaquil (UTC-5) y los
-- horarios se ofrecían con una hora de desfase. Venezuela usa America/Caracas (UTC-4), como el negocio.
-- Ejecutar una vez en el SQL Editor de Supabase (producción). Idempotente: si ya está corregida, no cambia nada.
-- Verificado antes (2026-09-15, solo lectura): 6 horarios activos y 0 citas futuras en la sucursal.

begin;

update public.branches
set timezone = 'America/Caracas'
where id = '2b279985-5ba8-4223-9b38-7959e706a956'
  and tenant_id = (select id from public.tenants where slug = 'elvis-studio')
  and timezone = 'America/Guayaquil';

commit;

-- Esperado: Tariba y San Cristobal con America/Caracas.
select b.name, b.timezone
from public.branches b
join public.tenants t on t.id = b.tenant_id
where t.slug = 'elvis-studio'
order by b.name;
