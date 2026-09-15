package mcpserver

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/drafts"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/audit"
)

type createDraftInput struct {
	CustomerID       string `json:"customerId"                 jsonschema:"UUID del cliente (usa search_customers)"`
	ServiceID        string `json:"serviceId"                  jsonschema:"UUID del servicio"`
	ServiceVariantID string `json:"serviceVariantId,omitempty" jsonschema:"UUID de la variante del servicio (opcional)"`
	BranchID         string `json:"branchId"                   jsonschema:"UUID de la sucursal"`
	SpecialistID     string `json:"specialistId,omitempty"     jsonschema:"UUID del especialista; obligatorio si el servicio lo requiere (usa list_available_slots)"`
	ScheduledAt      string `json:"scheduledAt"                jsonschema:"inicio de la cita en ISO 8601 con zona horaria, p. ej. 2026-09-16T10:00:00-05:00; debe coincidir con un horario de list_available_slots"`
	CustomerNotes    string `json:"customerNotes,omitempty"    jsonschema:"notas del cliente para la cita (opcional, máximo 500 caracteres)"`
	IdempotencyKey   string `json:"idempotencyKey"             jsonschema:"clave única de esta reserva (8 a 100 caracteres: letras, números, . _ : -); reutilízala solo al reintentar la misma reserva"`
}

type confirmDraftInput struct {
	DraftID        string `json:"draftId"        jsonschema:"UUID del borrador devuelto por create_appointment_draft"`
	IdempotencyKey string `json:"idempotencyKey" jsonschema:"la misma idempotencyKey usada al crear el borrador"`
}

var writeIdempotent = &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false)}

func addDraftTools(server *mcp.Server, env toolEnv) {
	svc := env.deps.Drafts
	actor := drafts.Actor{TenantID: env.access.TenantID, ConnectionID: env.access.ConnectionID, UserID: env.access.UserID}

	register(server, env, toolSpec[createDraftInput, drafts.DraftView]{
		tool: &mcp.Tool{
			Name:  "create_appointment_draft",
			Title: "Crear borrador de cita",
			Description: "Paso 1 de 2 para reservar. Valida cliente, servicio, variante, sucursal, especialista y que el horario esté libre, " +
				"y guarda un borrador que expira en pocos minutos sin crear la cita. Devuelve humanSummary: muéstralo al usuario y " +
				"pide su aprobación explícita antes de llamar a confirm_appointment_draft.",
			Annotations: writeIdempotent,
		},
		risk: audit.RiskWrite,
		// Notes are free text written by the user and the key is arbitrary: audit only identifiers and flags.
		summary: func(in createDraftInput) map[string]any {
			return compact(map[string]any{
				"customerId": in.CustomerID, "serviceId": in.ServiceID, "serviceVariantId": in.ServiceVariantID,
				"branchId": in.BranchID, "specialistId": in.SpecialistID, "scheduledAt": in.ScheduledAt,
				"hasNotes": strings.TrimSpace(in.CustomerNotes) != "",
			})
		},
		run: func(ctx context.Context, in createDraftInput) (drafts.DraftView, error) {
			return svc.Create(ctx, actor, drafts.CreateInput(in))
		},
	})

	register(server, env, toolSpec[confirmDraftInput, drafts.ConfirmResult]{
		tool: &mcp.Tool{
			Name:  "confirm_appointment_draft",
			Title: "Confirmar borrador de cita",
			Description: "Paso 2 de 2. Úsalo solo después de que el usuario apruebe explícitamente el humanSummary del borrador. " +
				"Crea la cita en estado pendiente de forma atómica, comprobando que el especialista sigue libre. " +
				"Repetir la llamada con el mismo borrador devuelve la misma cita.",
			Annotations: writeIdempotent,
		},
		risk: audit.RiskWrite,
		summary: func(in confirmDraftInput) map[string]any {
			return compact(map[string]any{"draftId": in.DraftID})
		},
		run: func(ctx context.Context, in confirmDraftInput) (drafts.ConfirmResult, error) {
			return svc.Confirm(ctx, actor, drafts.ConfirmInput(in))
		},
	})
}
