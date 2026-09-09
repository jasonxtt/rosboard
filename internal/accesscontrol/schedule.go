package accesscontrol

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	ScheduleModeAlways = "always"
	ScheduleModeWeekly = "weekly"

	MaxAccessScheduleWindows = 16
)

var ErrInvalidSchedule = errors.New("invalid access schedule")

var accessScheduleDays = [...]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var accessScheduleDayIndex = map[string]int{
	"mon": 0,
	"tue": 1,
	"wed": 2,
	"thu": 3,
	"fri": 4,
	"sat": 5,
	"sun": 6,
}

// AccessSchedule describes when an enabled access-control deny rule blocks
// matching traffic. RouterOS evaluates the canonical windows using its own
// local wall clock.
type AccessSchedule struct {
	Mode    string             `json:"mode"`
	Windows []AccessTimeWindow `json:"windows"`
}

// AccessTimeWindow uses minute precision. The end is exclusive; when end is
// earlier than start, the window continues into the following weekday.
type AccessTimeWindow struct {
	Days  []string `json:"days"`
	Start string   `json:"start"`
	End   string   `json:"end"`
}

type accessScheduleSegment struct {
	day      int
	startMin int
	endMin   int
}

func AlwaysSchedule() AccessSchedule {
	return AccessSchedule{Mode: ScheduleModeAlways, Windows: []AccessTimeWindow{}}
}

// NormalizeSchedule validates and returns a deterministic schedule suitable
// for persistence, API responses, and desired-state hashing.
func NormalizeSchedule(schedule AccessSchedule) (AccessSchedule, error) {
	mode := strings.ToLower(strings.TrimSpace(schedule.Mode))
	if mode == "" {
		mode = ScheduleModeAlways
	}
	if mode != ScheduleModeAlways && mode != ScheduleModeWeekly {
		return AccessSchedule{}, fmt.Errorf("%w: mode must be always or weekly", ErrInvalidSchedule)
	}
	if mode == ScheduleModeAlways {
		if len(schedule.Windows) != 0 {
			return AccessSchedule{}, fmt.Errorf("%w: always mode must not define windows", ErrInvalidSchedule)
		}
		return AlwaysSchedule(), nil
	}
	if len(schedule.Windows) == 0 {
		return AccessSchedule{}, fmt.Errorf("%w: weekly mode requires at least one window", ErrInvalidSchedule)
	}
	if len(schedule.Windows) > MaxAccessScheduleWindows {
		return AccessSchedule{}, fmt.Errorf("%w: at most %d windows are allowed", ErrInvalidSchedule, MaxAccessScheduleWindows)
	}

	windows := make([]AccessTimeWindow, 0, len(schedule.Windows))
	segments := make([]accessScheduleSegment, 0, len(schedule.Windows)*2)
	for _, window := range schedule.Windows {
		normalized, segmentParts, err := normalizeAccessTimeWindow(window)
		if err != nil {
			return AccessSchedule{}, err
		}
		windows = append(windows, normalized)
		segments = append(segments, segmentParts...)
	}
	if err := validateScheduleSegments(segments); err != nil {
		return AccessSchedule{}, err
	}
	sort.Slice(windows, func(i, j int) bool {
		leftDays := strings.Join(windows[i].Days, ",")
		rightDays := strings.Join(windows[j].Days, ",")
		if leftDays != rightDays {
			return leftDays < rightDays
		}
		if windows[i].Start != windows[j].Start {
			return windows[i].Start < windows[j].Start
		}
		return windows[i].End < windows[j].End
	})
	return AccessSchedule{Mode: ScheduleModeWeekly, Windows: windows}, nil
}

func ValidateSchedule(schedule AccessSchedule) error {
	_, err := NormalizeSchedule(schedule)
	return err
}

func normalizeAccessTimeWindow(window AccessTimeWindow) (AccessTimeWindow, []accessScheduleSegment, error) {
	start, err := parseScheduleClock(window.Start)
	if err != nil {
		return AccessTimeWindow{}, nil, fmt.Errorf("%w: invalid start time %q", ErrInvalidSchedule, window.Start)
	}
	end, err := parseScheduleClock(window.End)
	if err != nil {
		return AccessTimeWindow{}, nil, fmt.Errorf("%w: invalid end time %q", ErrInvalidSchedule, window.End)
	}
	if start == end {
		return AccessTimeWindow{}, nil, fmt.Errorf("%w: start and end time must differ", ErrInvalidSchedule)
	}

	daySet := make(map[int]bool, len(window.Days))
	for _, rawDay := range window.Days {
		day, ok := accessScheduleDayIndex[strings.ToLower(strings.TrimSpace(rawDay))]
		if !ok {
			return AccessTimeWindow{}, nil, fmt.Errorf("%w: invalid weekday %q", ErrInvalidSchedule, rawDay)
		}
		daySet[day] = true
	}
	if len(daySet) == 0 {
		return AccessTimeWindow{}, nil, fmt.Errorf("%w: a window requires at least one weekday", ErrInvalidSchedule)
	}
	days := make([]string, 0, len(daySet))
	dayNumbers := make([]int, 0, len(daySet))
	for day := range daySet {
		dayNumbers = append(dayNumbers, day)
	}
	sort.Ints(dayNumbers)
	for _, day := range dayNumbers {
		days = append(days, accessScheduleDays[day])
	}

	segments := make([]accessScheduleSegment, 0, len(dayNumbers)*2)
	for _, day := range dayNumbers {
		if start < end {
			segments = append(segments, accessScheduleSegment{day: day, startMin: start, endMin: end})
			continue
		}
		segments = append(segments, accessScheduleSegment{day: day, startMin: start, endMin: 24 * 60})
		nextDay := (day + 1) % len(accessScheduleDays)
		if end > 0 {
			segments = append(segments, accessScheduleSegment{day: nextDay, startMin: 0, endMin: end})
		}
	}
	return AccessTimeWindow{Days: days, Start: formatScheduleClock(start), End: formatScheduleClock(end)}, segments, nil
}

func parseScheduleClock(value string) (int, error) {
	value = strings.TrimSpace(value)
	if len(value) != 5 || value[2] != ':' || value[0] < '0' || value[0] > '9' || value[1] < '0' || value[1] > '9' || value[3] < '0' || value[3] > '9' || value[4] < '0' || value[4] > '9' {
		return 0, errors.New("time must use HH:MM")
	}
	hour := int(value[0]-'0')*10 + int(value[1]-'0')
	minute := int(value[3]-'0')*10 + int(value[4]-'0')
	if hour > 23 || minute > 59 {
		return 0, errors.New("time must be within 00:00 and 23:59")
	}
	return hour*60 + minute, nil
}

func formatScheduleClock(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func validateScheduleSegments(segments []accessScheduleSegment) error {
	sort.Slice(segments, func(i, j int) bool {
		if segments[i].day != segments[j].day {
			return segments[i].day < segments[j].day
		}
		if segments[i].startMin != segments[j].startMin {
			return segments[i].startMin < segments[j].startMin
		}
		return segments[i].endMin < segments[j].endMin
	})
	for index := 1; index < len(segments); index++ {
		previous, current := segments[index-1], segments[index]
		if previous.day == current.day && current.startMin < previous.endMin {
			return fmt.Errorf("%w: overlapping windows on %s", ErrInvalidSchedule, accessScheduleDays[current.day])
		}
	}
	return nil
}

// CompileSchedule converts a weekly schedule to RouterOS time matcher values.
// Each returned value has the form "HH:MM:SS-HH:MM:SS,mon,tue". Always mode
// returns no matcher, meaning the existing all-day deny objects remain active.
func CompileSchedule(schedule AccessSchedule) ([]string, error) {
	normalized, err := NormalizeSchedule(schedule)
	if err != nil {
		return nil, err
	}
	if normalized.Mode == ScheduleModeAlways {
		return nil, nil
	}
	byRange := make(map[string]map[int]bool)
	for _, window := range normalized.Windows {
		start, _ := parseScheduleClock(window.Start)
		end, _ := parseScheduleClock(window.End)
		for _, rawDay := range window.Days {
			day := accessScheduleDayIndex[rawDay]
			if start < end {
				addCompiledScheduleSegment(byRange, day, start, end)
				continue
			}
			addCompiledScheduleSegment(byRange, day, start, 24*60)
			nextDay := (day + 1) % len(accessScheduleDays)
			if end > 0 {
				addCompiledScheduleSegment(byRange, nextDay, 0, end)
			}
		}
	}
	type compiledRange struct {
		key      string
		startMin int
		endMin   int
		days     []int
	}
	ranges := make([]compiledRange, 0, len(byRange))
	for key, daySet := range byRange {
		var startMin, endMin int
		if _, err := fmt.Sscanf(key, "%d-%d", &startMin, &endMin); err != nil {
			return nil, fmt.Errorf("%w: invalid compiled time range", ErrInvalidSchedule)
		}
		days := make([]int, 0, len(daySet))
		for day := range daySet {
			days = append(days, day)
		}
		sort.Ints(days)
		ranges = append(ranges, compiledRange{key: key, startMin: startMin, endMin: endMin, days: days})
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].startMin != ranges[j].startMin {
			return ranges[i].startMin < ranges[j].startMin
		}
		return ranges[i].endMin < ranges[j].endMin
	})
	result := make([]string, 0, len(ranges))
	for _, item := range ranges {
		dayNames := make([]string, 0, len(item.days))
		for _, day := range item.days {
			dayNames = append(dayNames, accessScheduleDays[day])
		}
		result = append(result, formatRouterOSTimeRange(item.startMin, item.endMin)+","+strings.Join(dayNames, ","))
	}
	return result, nil
}

func addCompiledScheduleSegment(byRange map[string]map[int]bool, day, startMin, endMin int) {
	if startMin >= endMin {
		return
	}
	key := fmt.Sprintf("%d-%d", startMin, endMin)
	if byRange[key] == nil {
		byRange[key] = make(map[int]bool)
	}
	byRange[key][day] = true
}

func formatRouterOSTimeRange(startMin, endMin int) string {
	endSecond := endMin*60 - 1
	if endMin == 24*60 {
		endSecond = 23*60*60 + 59*60 + 59
	}
	return fmt.Sprintf("%02d:%02d:00-%02d:%02d:%02d", startMin/60, startMin%60, endSecond/3600, (endSecond/60)%60, endSecond%60)
}
