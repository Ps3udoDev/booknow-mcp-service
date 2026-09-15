package mcpserver

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
)

type scheduleSummaryInput struct {
	StartDate    string `json:"startDate"              jsonschema:"fecha inicial en formato YYYY-MM-DD (zona horaria del negocio)"`
	EndDate      string `json:"endDate"                jsonschema:"fecha final inclusiva en formato YYYY-MM-DD; máximo 31 días de rango"`
	BranchID     string `json:"branchId,omitempty"     jsonschema:"UUID de la sucursal (opcional)"`
	SpecialistID string `json:"specialistId,omitempty" jsonschema:"UUID del especialista (opcional)"`
}

type availableSlotsInput struct {
	ServiceID    string `json:"serviceId"              jsonschema:"UUID del servicio a agendar"`
	BranchID     string `json:"branchId"               jsonschema:"UUID de la sucursal"`
	Date         string `json:"date"                   jsonschema:"fecha en formato YYYY-MM-DD, desde hoy hasta 90 días"`
	SpecialistID string `json:"specialistId,omitempty" jsonschema:"UUID del especialista (opcional)"`
}

type listAppointmentsInput struct {
	StartDate string `json:"startDate"        jsonschema:"fecha inicial YYYY-MM-DD o fecha y hora ISO 8601"`
	EndDate   string `json:"endDate"          jsonschema:"fecha final inclusiva YYYY-MM-DD o fecha y hora ISO 8601; máximo 31 días"`
	Status    string `json:"status,omitempty" jsonschema:"filtrar por estado: pending, confirmed, in_progress, completed, cancelled o no_show"`
	Page      int    `json:"page,omitempty"   jsonschema:"número de página (por defecto 1)"`
	Limit     int    `json:"limit,omitempty"  jsonschema:"resultados por página (por defecto 20, máximo 100)"`
}

type searchCustomersInput struct {
	Query string `json:"query"           jsonschema:"nombre, apellido o teléfono a buscar (3 a 100 caracteres)"`
	Limit int    `json:"limit,omitempty" jsonschema:"máximo de resultados (por defecto 20, máximo 100)"`
}

type customersOutput struct {
	Customers []business.Customer `json:"customers"`
}

func addBusinessTools(server *mcp.Server, env toolEnv) {
	svc := env.deps.Business
	tenantID := env.access.TenantID

	register(server, env, toolSpec[struct{}, business.Snapshot]{
		tool: &mcp.Tool{
			Name:        "get_business_snapshot",
			Title:       "Resumen del negocio",
			Description: "Obtiene KPIs agregados del negocio sin datos personales: sucursales, especialistas y servicios activos, citas de hoy (zona horaria del negocio), citas por estado y servicios más solicitados en los últimos 30 días.",
			Annotations: readOnly,
		},
		run: func(ctx context.Context, _ struct{}) (business.Snapshot, error) {
			return svc.Snapshot(ctx, tenantID)
		},
	})

	register(server, env, toolSpec[scheduleSummaryInput, business.ScheduleSummary]{
		tool: &mcp.Tool{
			Name:        "get_schedule_summary",
			Title:       "Resumen de agenda",
			Description: "Resume la ocupación en un rango de fechas (máximo 31 días): citas no canceladas por día y por especialista. Filtros opcionales por sucursal y especialista.",
			Annotations: readOnly,
		},
		summary: func(in scheduleSummaryInput) map[string]any {
			return compact(map[string]any{"startDate": in.StartDate, "endDate": in.EndDate, "branchId": in.BranchID, "specialistId": in.SpecialistID})
		},
		run: func(ctx context.Context, in scheduleSummaryInput) (business.ScheduleSummary, error) {
			return svc.ScheduleSummary(ctx, tenantID, business.ScheduleSummaryInput(in))
		},
	})

	register(server, env, toolSpec[availableSlotsInput, business.AvailableSlots]{
		tool: &mcp.Tool{
			Name:        "list_available_slots",
			Title:       "Horarios disponibles",
			Description: "Calcula los horarios libres para reservar un servicio en una sucursal y fecha, respetando horarios y descansos de los especialistas, horario de la sucursal, ausencias y citas existentes. Devuelve cada hora con los especialistas disponibles.",
			Annotations: readOnly,
		},
		summary: func(in availableSlotsInput) map[string]any {
			return compact(map[string]any{"serviceId": in.ServiceID, "branchId": in.BranchID, "date": in.Date, "specialistId": in.SpecialistID})
		},
		run: func(ctx context.Context, in availableSlotsInput) (business.AvailableSlots, error) {
			return svc.AvailableSlots(ctx, tenantID, business.AvailableSlotsInput(in))
		},
	})

	register(server, env, toolSpec[listAppointmentsInput, business.AppointmentList]{
		tool: &mcp.Tool{
			Name:        "list_appointments",
			Title:       "Listado de citas",
			Description: "Lista las citas de un rango de fechas (máximo 31 días) con paginación. Los teléfonos de los clientes se devuelven siempre enmascarados.",
			Annotations: readOnly,
		},
		summary: func(in listAppointmentsInput) map[string]any {
			return compact(map[string]any{"startDate": in.StartDate, "endDate": in.EndDate, "status": in.Status, "page": in.Page, "limit": in.Limit})
		},
		run: func(ctx context.Context, in listAppointmentsInput) (business.AppointmentList, error) {
			return svc.ListAppointments(ctx, tenantID, business.ListAppointmentsInput(in))
		},
	})

	register(server, env, toolSpec[searchCustomersInput, customersOutput]{
		tool: &mcp.Tool{
			Name:        "search_customers",
			Title:       "Buscar clientes",
			Description: "Busca clientes por nombre, apellido o teléfono (mínimo 3 caracteres). Devuelve teléfonos enmascarados y nunca notas, direcciones ni documentos.",
			Annotations: readOnly,
		},
		// The query may be a name or a phone number: audit only its size and kind.
		summary: func(in searchCustomersInput) map[string]any {
			return compact(map[string]any{
				"queryLength": utf8.RuneCountInString(strings.TrimSpace(in.Query)),
				"byPhone":     looksLikePhone(in.Query),
				"limit":       in.Limit,
			})
		},
		run: func(ctx context.Context, in searchCustomersInput) (customersOutput, error) {
			customers, err := svc.SearchCustomers(ctx, tenantID, business.SearchCustomersInput(in))

			return customersOutput{Customers: customers}, err
		},
	})
}

// compact drops empty values so audit summaries only hold what the caller sent.
func compact(m map[string]any) map[string]any {
	for k, v := range m {
		switch x := v.(type) {
		case string:
			if x == "" {
				delete(m, k)
			}
		case int:
			if x == 0 {
				delete(m, k)
			}
		case bool:
			if !x {
				delete(m, k)
			}
		}
	}

	return m
}

func looksLikePhone(q string) bool {
	digits := 0

	for _, r := range q {
		if unicode.IsDigit(r) {
			digits++
		}
	}

	return digits >= 3
}
