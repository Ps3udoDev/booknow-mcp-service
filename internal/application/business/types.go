package business

import (
	"context"
	"time"
)

// Store reads business data. Every method filters by tenant; implementations must never return
// rows of another tenant. It returns ErrNotFound for single-entity lookups that do not exist.
type Store interface {
	TenantProfile(ctx context.Context, tenantID string) (TenantProfile, error)
	SnapshotCounts(ctx context.Context, q SnapshotQuery) (SnapshotCounts, error)
	ScheduleSummary(ctx context.Context, q ScheduleQuery) (ScheduleCounts, error)
	ListAppointments(ctx context.Context, q AppointmentQuery) ([]AppointmentRow, int, error)
	SearchCustomers(ctx context.Context, q CustomerQuery) ([]CustomerRow, error)
	SlotService(ctx context.Context, tenantID, serviceID string) (SlotService, error)
	SlotBranch(ctx context.Context, tenantID, branchID string) (SlotBranch, error)
	Availability(ctx context.Context, q AvailabilityQuery) (Availability, error)
}

// Ref is a named entity reference.
type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TenantProfile is the tenant data needed to render results.
type TenantProfile struct {
	Name     string
	Slug     string
	Timezone string
}

// --- get_business_snapshot ---

// SnapshotQuery selects the aggregation windows for the snapshot.
type SnapshotQuery struct {
	TenantID string
	// Since bounds the status and top-service statistics.
	Since time.Time
	// DayStart and DayEnd delimit "today" in the tenant timezone.
	DayStart, DayEnd time.Time
}

// SnapshotCounts are the aggregates read from the store.
type SnapshotCounts struct {
	ActiveBranches    int
	ActiveSpecialists int
	ActiveServices    int
	TodayAppointments int
	ByStatus          map[string]int
	TopServices       []ServiceCount
}

// ServiceCount is the number of appointments of a service.
type ServiceCount struct {
	Name              string `json:"name"`
	AppointmentsCount int    `json:"appointmentsCount"`
}

// Snapshot is the aggregated, PII-free view of the business.
type Snapshot struct {
	TenantName  string          `json:"tenantName"`
	TenantSlug  string          `json:"tenantSlug"`
	Timezone    string          `json:"timezone"`
	Metrics     SnapshotMetrics `json:"metrics"`
	TopServices []ServiceCount  `json:"topServicesLast30Days"`
	GeneratedAt time.Time       `json:"generatedAt"`
}

// SnapshotMetrics are the business KPIs.
type SnapshotMetrics struct {
	ActiveBranchesCount    int            `json:"activeBranchesCount"`
	ActiveSpecialistsCount int            `json:"activeSpecialistsCount"`
	ActiveServicesCount    int            `json:"activeServicesCount"`
	TodayAppointmentsCount int            `json:"todayAppointmentsCount"`
	AppointmentsByStatus   map[string]int `json:"appointmentsByStatusLast30Days"`
}

// --- get_schedule_summary ---

// ScheduleSummaryInput are the tool arguments.
type ScheduleSummaryInput struct {
	StartDate    string
	EndDate      string
	BranchID     string
	SpecialistID string
}

// ScheduleQuery selects non-cancelled appointments in [From, To).
type ScheduleQuery struct {
	TenantID string
	// Timezone is a validated IANA name used to bucket appointments by local date.
	Timezone     string
	From, To     time.Time
	BranchID     string
	SpecialistID string
}

// ScheduleCounts are the grouped counts read from the store.
type ScheduleCounts struct {
	Days        []DayCount
	Specialists []SpecialistCount
}

// DayCount is the number of appointments on a local date.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// SpecialistCount is the number of appointments of a specialist; ID is nil for unassigned ones.
type SpecialistCount struct {
	ID    *string `json:"id"`
	Name  string  `json:"name"`
	Count int     `json:"count"`
}

// Period describes the requested range.
type Period struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Timezone  string `json:"timezone"`
}

// ScheduleSummary is the occupation summary for a range.
type ScheduleSummary struct {
	Period                 Period            `json:"period"`
	TotalAppointments      int               `json:"totalAppointments"`
	DailyAppointments      []DayCount        `json:"dailyAppointments"`
	SpecialistAppointments []SpecialistCount `json:"specialistAppointments"`
}

// --- list_appointments ---

// ListAppointmentsInput are the tool arguments.
type ListAppointmentsInput struct {
	StartDate string
	EndDate   string
	Status    string
	Page      int
	Limit     int
}

// AppointmentQuery selects a page of appointments in [From, To).
type AppointmentQuery struct {
	TenantID string
	From, To time.Time
	Status   string
	Limit    int
	Offset   int
}

// AppointmentRow is an appointment as read from the store, before masking.
type AppointmentRow struct {
	ID                string
	ScheduledAt       time.Time
	EndsAt            *time.Time
	DurationMinutes   int
	Status            string
	EstimatedPrice    *float64
	CurrencyCode      *string
	CustomerNotes     *string
	Source            *string
	CustomerID        *string
	CustomerFirstName *string
	CustomerLastName  *string
	CustomerPhone     *string
	SpecialistID      *string
	SpecialistName    *string
	ServiceID         *string
	ServiceName       *string
	BranchID          *string
	BranchName        *string
}

// AppointmentCustomer is the customer of an appointment with a masked phone.
type AppointmentCustomer struct {
	ID          string  `json:"id"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	PhoneMasked *string `json:"phone_masked"`
}

// Appointment is an appointment safe to return to the MCP client.
type Appointment struct {
	ID              string               `json:"id"`
	ScheduledAt     time.Time            `json:"scheduled_at"`
	EndsAt          *time.Time           `json:"ends_at"`
	DurationMinutes int                  `json:"duration_minutes"`
	Status          string               `json:"status"`
	EstimatedPrice  *float64             `json:"estimated_price"`
	CurrencyCode    *string              `json:"currency_code"`
	CustomerNotes   *string              `json:"customer_notes"`
	Source          *string              `json:"source"`
	Customer        *AppointmentCustomer `json:"customer"`
	Specialist      *Ref                 `json:"specialist"`
	Service         *Ref                 `json:"service"`
	Branch          *Ref                 `json:"branch"`
}

// Pagination describes a page of results.
type Pagination struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

// AppointmentList is a page of appointments.
type AppointmentList struct {
	Timezone   string        `json:"timezone"`
	Items      []Appointment `json:"items"`
	Pagination Pagination    `json:"pagination"`
}

// --- search_customers ---

// SearchCustomersInput are the tool arguments.
type SearchCustomersInput struct {
	Query string
	Limit int
}

// CustomerQuery searches customers by an escaped ILIKE pattern and, optionally, phone digits.
type CustomerQuery struct {
	TenantID string
	// Pattern is an ILIKE pattern with \ as escape character.
	Pattern string
	// Digits, when not empty, also matches customers whose phone digits contain it.
	Digits string
	Limit  int
}

// CustomerRow is a customer as read from the store, before masking.
type CustomerRow struct {
	ID        string
	FirstName string
	LastName  string
	Phone     *string
	CreatedAt *time.Time
}

// Customer is a customer safe to return to the MCP client.
type Customer struct {
	ID          string     `json:"id"`
	FirstName   string     `json:"first_name"`
	LastName    string     `json:"last_name"`
	PhoneMasked *string    `json:"phone_masked"`
	CreatedAt   *time.Time `json:"created_at"`
}

// --- list_available_slots ---

// AvailableSlotsInput are the tool arguments.
type AvailableSlotsInput struct {
	ServiceID    string
	BranchID     string
	Date         string
	SpecialistID string
}

// SlotService is the service being booked.
type SlotService struct {
	ID                 string
	Name               string
	DurationMinutes    int
	BufferMinutes      int
	RequiresSpecialist bool
	Active             bool
}

// SlotBranch is the branch where the service is booked.
type SlotBranch struct {
	ID       string
	Name     string
	Timezone string
	// OperatingHours is the raw branches.operating_hours JSON.
	OperatingHours []byte
	Active         bool
}

// AvailabilityQuery selects schedules, exceptions and busy intervals for one local day.
type AvailabilityQuery struct {
	TenantID     string
	BranchID     string
	SpecialistID string
	// Weekday is the day_of_week enum value (monday..sunday).
	Weekday string
	// Date is the local date (YYYY-MM-DD) used for schedule exceptions.
	Date             string
	DayStart, DayEnd time.Time
}

// Availability is the scheduling data for one day.
type Availability struct {
	// SpecialistFound reports whether the requested specialist is an active specialist of the tenant.
	SpecialistFound bool
	Specialists     []SpecialistSchedule
	Exceptions      []ScheduleException
	Busy            []BusyInterval
}

// SpecialistSchedule is a specialist with their shifts at the branch for the weekday.
type SpecialistSchedule struct {
	ID     string
	Name   string
	Shifts []Shift
}

// Shift is a working period in local clock time ("15:04:05").
type Shift struct {
	Start      string
	End        string
	BreakStart *string
	BreakEnd   *string
}

// ScheduleException is an absence or special schedule; SpecialistID nil means the whole branch.
type ScheduleException struct {
	SpecialistID *string
	Type         string
	Start        *string
	End          *string
	DayOff       bool
}

// BusyInterval is an existing appointment of a specialist.
type BusyInterval struct {
	SpecialistID string
	Start, End   time.Time
}

// SlotServiceRef describes the booked service in the result.
type SlotServiceRef struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	DurationMinutes int    `json:"duration_minutes"`
}

// Slot is a bookable start time with the specialists free at that time.
type Slot struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Specialists []Ref     `json:"specialists"`
}

// AvailableSlots is the result of list_available_slots.
type AvailableSlots struct {
	Date                string         `json:"date"`
	Timezone            string         `json:"timezone"`
	Service             SlotServiceRef `json:"service"`
	Branch              Ref            `json:"branch"`
	SpecialistID        *string        `json:"specialistId"`
	SlotIntervalMinutes int            `json:"slotIntervalMinutes"`
	// CapacityChecked is false for services that do not require a specialist: only branch hours apply.
	CapacityChecked bool   `json:"capacityChecked"`
	Slots           []Slot `json:"slots"`
}
