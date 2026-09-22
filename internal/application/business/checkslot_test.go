package business

import (
	"testing"
	"time"
)

func TestCheckSlot(t *testing.T) {
	t.Parallel()

	gye := time.FixedZone("GYE", -5*60*60)
	at := func(hhmm string) time.Time {
		c, _ := parseClock(hhmm)

		return time.Date(2026, 9, 16, 0, 0, 0, 0, gye).Add(c)
	}

	tests := []struct {
		name          string
		in            SlotCheckInput
		wantAvailable bool
		wantFree      []string
	}{
		{name: "start on a free slot", in: SlotCheckInput{Start: at("10:00")}, wantAvailable: true, wantFree: []string{"Ana"}},
		{name: "same instant in UTC", in: SlotCheckInput{Start: at("10:00").UTC()}, wantAvailable: true, wantFree: []string{"Ana"}},
		{name: "requested specialist free", in: SlotCheckInput{Start: at("09:00"), SpecialistID: specID}, wantAvailable: true, wantFree: []string{"Ana"}},
		{name: "variant makes it end after the shift", in: SlotCheckInput{Start: at("10:00"), ExtraMinutes: 30}},
		{name: "off the 30 minute grid", in: SlotCheckInput{Start: at("10:15")}},
		{name: "outside working hours", in: SlotCheckInput{Start: at("15:00")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := tt.in
			in.ServiceID, in.BranchID = serviceID, branchID

			got, err := newTestService(slotStore()).CheckSlot(t.Context(), tenantA, in)
			if err != nil {
				t.Fatalf("CheckSlot() error = %v", err)
			}

			var free []string
			for _, sp := range got.Specialists {
				free = append(free, sp.Name)
			}

			if got.Available != tt.wantAvailable || len(free) != len(tt.wantFree) || (len(free) > 0 && free[0] != tt.wantFree[0]) {
				t.Errorf("CheckSlot() available = %v free = %v, want %v %v", got.Available, free, tt.wantAvailable, tt.wantFree)
			}

			if got.Service.ID != serviceID || got.Branch.ID != branchID || got.Timezone != "America/Guayaquil" || got.DurationMinutes != 60+in.ExtraMinutes {
				t.Errorf("CheckSlot() = %+v", got)
			}
		})
	}
}

func TestCheckSlotErrors(t *testing.T) {
	t.Parallel()

	valid := SlotCheckInput{ServiceID: serviceID, BranchID: branchID, Start: time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)}

	tests := []struct {
		name   string
		mutate func(*SlotCheckInput, *fakeStore)
		want   error
	}{
		{name: "invalid branch id", mutate: func(in *SlotCheckInput, _ *fakeStore) { in.BranchID = "centro" }, want: ErrInvalidArgument},
		{name: "start in the past", mutate: func(in *SlotCheckInput, _ *fakeStore) { in.Start = fixedNow.Add(-time.Hour) }, want: ErrInvalidArgument},
		{name: "start too far ahead", mutate: func(in *SlotCheckInput, _ *fakeStore) { in.Start = fixedNow.AddDate(0, 0, 120) }, want: ErrInvalidArgument},
		{name: "duration not positive", mutate: func(in *SlotCheckInput, _ *fakeStore) { in.ExtraMinutes = -60 }, want: ErrInvalidArgument},
		{name: "service of another tenant", mutate: func(_ *SlotCheckInput, s *fakeStore) { s.serviceErr = ErrNotFound }, want: ErrNotFound},
		{name: "inactive branch", mutate: func(_ *SlotCheckInput, s *fakeStore) { s.branch.Active = false }, want: ErrNotFound},
		{
			name: "unknown specialist",
			mutate: func(in *SlotCheckInput, s *fakeStore) {
				in.SpecialistID = specID
				s.availability.SpecialistFound = false
			},
			want: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in, store := valid, slotStore()
			tt.mutate(&in, store)

			_, err := newTestService(store).CheckSlot(t.Context(), tenantA, in)
			requireKind(t, err, tt.want)
		})
	}
}
