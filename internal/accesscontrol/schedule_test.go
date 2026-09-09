package accesscontrol

import (
	"reflect"
	"testing"
)

func TestNormalizeScheduleDefaultsToAlways(t *testing.T) {
	schedule, err := NormalizeSchedule(AccessSchedule{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schedule, AlwaysSchedule()) {
		t.Fatalf("zero schedule = %#v, want %#v", schedule, AlwaysSchedule())
	}
}

func TestNormalizeScheduleCanonicalizesWeeklyWindows(t *testing.T) {
	schedule, err := NormalizeSchedule(AccessSchedule{
		Mode: ScheduleModeWeekly,
		Windows: []AccessTimeWindow{{
			Days:  []string{"FRI", "mon", "fri"},
			Start: " 20:00 ",
			End:   "22:30",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := AccessSchedule{Mode: ScheduleModeWeekly, Windows: []AccessTimeWindow{{Days: []string{"mon", "fri"}, Start: "20:00", End: "22:30"}}}
	if !reflect.DeepEqual(schedule, want) {
		t.Fatalf("normalized schedule = %#v, want %#v", schedule, want)
	}
}

func TestNormalizeScheduleRejectsInvalidWindows(t *testing.T) {
	tests := []struct {
		name     string
		schedule AccessSchedule
	}{
		{name: "weekly empty", schedule: AccessSchedule{Mode: ScheduleModeWeekly}},
		{name: "unknown mode", schedule: AccessSchedule{Mode: "daily"}},
		{name: "invalid day", schedule: AccessSchedule{Mode: ScheduleModeWeekly, Windows: []AccessTimeWindow{{Days: []string{"monday"}, Start: "20:00", End: "21:00"}}}},
		{name: "invalid time", schedule: AccessSchedule{Mode: ScheduleModeWeekly, Windows: []AccessTimeWindow{{Days: []string{"mon"}, Start: "8:00", End: "21:00"}}}},
		{name: "equal endpoints", schedule: AccessSchedule{Mode: ScheduleModeWeekly, Windows: []AccessTimeWindow{{Days: []string{"mon"}, Start: "20:00", End: "20:00"}}}},
		{name: "always with windows", schedule: AccessSchedule{Mode: ScheduleModeAlways, Windows: []AccessTimeWindow{{Days: []string{"mon"}, Start: "20:00", End: "21:00"}}}},
		{name: "overlap", schedule: AccessSchedule{Mode: ScheduleModeWeekly, Windows: []AccessTimeWindow{
			{Days: []string{"mon"}, Start: "20:00", End: "22:00"},
			{Days: []string{"mon"}, Start: "21:00", End: "23:00"},
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateSchedule(test.schedule); err == nil {
				t.Fatal("expected schedule validation error")
			}
		})
	}
}

func TestCompileScheduleGroupsDaysAndSplitsMidnight(t *testing.T) {
	compiled, err := CompileSchedule(AccessSchedule{
		Mode: ScheduleModeWeekly,
		Windows: []AccessTimeWindow{
			{Days: []string{"mon", "tue"}, Start: "20:00", End: "22:00"},
			{Days: []string{"fri"}, Start: "22:00", End: "07:00"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"00:00:00-06:59:59,sat",
		"20:00:00-21:59:59,mon,tue",
		"22:00:00-23:59:59,fri",
	}
	if !reflect.DeepEqual(compiled, want) {
		t.Fatalf("compiled schedule = %#v, want %#v", compiled, want)
	}
}

func TestCompileScheduleAlwaysHasNoMatcher(t *testing.T) {
	compiled, err := CompileSchedule(AlwaysSchedule())
	if err != nil {
		t.Fatal(err)
	}
	if compiled != nil {
		t.Fatalf("always schedule compiled to %#v, want nil", compiled)
	}
}

func TestNormalizeScheduleRejectsCrossMidnightOverlapOnNextDay(t *testing.T) {
	_, err := NormalizeSchedule(AccessSchedule{
		Mode: ScheduleModeWeekly,
		Windows: []AccessTimeWindow{
			{Days: []string{"mon"}, Start: "22:00", End: "02:00"},
			{Days: []string{"tue"}, Start: "01:00", End: "03:00"},
		},
	})
	if err == nil {
		t.Fatal("expected overlap after cross-midnight expansion")
	}
}
