// Package when parses the natural-language times used by reminders and
// send-later. It is deliberately pure: the caller supplies the reference
// time, including its local timezone, so it is easy to test and DST-safe.
package when

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	relativeRe = regexp.MustCompile(`^(?:\+?)(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?$`)
	inRe       = regexp.MustCompile(`^in\s+(.+)$`)
	clockRe    = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
	isoDateRe  = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})(?:\s+(.+))?$`)
	monthDayRe = regexp.MustCompile(`^(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+(\d{1,2})(?:\s+(.+))?$`)
)

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

var months = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "sept": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// Parse resolves an expression relative to now. Relative day/week phrases
// use calendar arithmetic, while the legacy +1d/+2h forms remain duration
// arithmetic for backwards compatibility.
func Parse(input string, now time.Time) (time.Time, error) {
	s := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(input)), " "))
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if at, ok, err := parseRelative(s, now); ok {
		return at, err
	}
	if at, ok, err := parseNamedPhrase(s, now); ok {
		return at, err
	}
	if at, ok, err := parseWeekday(s, now); ok {
		return at, err
	}
	if at, ok, err := parseDate(s, now); ok {
		return at, err
	}
	if at, ok, err := parseClock(s, now); ok {
		return at, err
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q — try in 20 minutes, tomorrow 09:00, friday 2pm, or 2026-09-12 09:00", input)
}

func parseRelative(s string, now time.Time) (time.Time, bool, error) {
	legacy := strings.TrimPrefix(s, "+")
	if m := relativeRe.FindStringSubmatch(legacy); m != nil && legacy != "" && (strings.ContainsAny(legacy, "dhm")) {
		var d time.Duration
		for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute} {
			if m[i+1] == "" {
				continue
			}
			n, _ := strconv.Atoi(m[i+1])
			d += time.Duration(n) * unit
		}
		if d <= 0 {
			return time.Time{}, true, fmt.Errorf("time must be in the future")
		}
		return now.Add(d), true, nil
	}
	if m := inRe.FindStringSubmatch(s); m != nil {
		phrase := strings.TrimSpace(m[1])
		if phrase == "a week" || phrase == "one week" {
			return now.AddDate(0, 0, 7), true, nil
		}
		parts := strings.Fields(phrase)
		if len(parts) == 2 {
			n, err := strconv.Atoi(parts[0])
			if err == nil && n > 0 {
				switch parts[1] {
				case "minute", "minutes", "min", "mins":
					return now.Add(time.Duration(n) * time.Minute), true, nil
				case "hour", "hours", "hr", "hrs":
					return now.Add(time.Duration(n) * time.Hour), true, nil
				case "day", "days":
					return now.AddDate(0, 0, n), true, nil
				case "week", "weeks":
					return now.AddDate(0, 0, 7*n), true, nil
				}
			}
		}
		return time.Time{}, true, fmt.Errorf("unrecognized relative time %q", phrase)
	}
	return time.Time{}, false, nil
}

func parseNamedPhrase(s string, now time.Time) (time.Time, bool, error) {
	date := now
	switch s {
	case "later today":
		return ceilHour(now.Add(3 * time.Hour)), true, nil
	case "this weekend":
		return nextWeekdayAt(now, time.Saturday, 9, 0, false), true, nil
	case "next week":
		return nextWeekdayAt(now, time.Monday, 9, 0, true), true, nil
	case "tonight":
		at := time.Date(date.Year(), date.Month(), date.Day(), 18, 0, 0, 0, date.Location())
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at, true, nil
	case "tomorrow":
		return dateAt(now.AddDate(0, 0, 1), 9, 0), true, nil
	case "tomorrow morning":
		return dateAt(now.AddDate(0, 0, 1), 9, 0), true, nil
	case "tomorrow afternoon":
		return dateAt(now.AddDate(0, 0, 1), 14, 0), true, nil
	case "tomorrow evening":
		return dateAt(now.AddDate(0, 0, 1), 18, 0), true, nil
	}
	for _, part := range []struct {
		prefix string
		hour   int
	}{
		{"tomorrow morning", 9}, {"tomorrow afternoon", 14}, {"tomorrow evening", 18},
	} {
		if strings.HasPrefix(s, part.prefix+" ") {
			clock, ok, err := parseClock(strings.TrimSpace(strings.TrimPrefix(s, part.prefix)), now)
			if !ok || err != nil {
				return time.Time{}, true, fmt.Errorf("expected a clock time after %q", part.prefix)
			}
			return clockOnDate(now.AddDate(0, 0, 1), clock.Hour(), clock.Minute()), true, nil
		}
	}
	if strings.HasPrefix(s, "tomorrow ") {
		hour, minute, ok, err := parseClockParts(strings.TrimSpace(strings.TrimPrefix(s, "tomorrow ")))
		if !ok || err != nil {
			return time.Time{}, true, fmt.Errorf("expected a clock time after tomorrow")
		}
		return clockOnDate(now.AddDate(0, 0, 1), hour, minute), true, nil
	}
	return time.Time{}, false, nil
}

func parseWeekday(s string, now time.Time) (time.Time, bool, error) {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return time.Time{}, false, nil
	}
	word := parts[0]
	strict := false
	if word == "next" {
		if len(parts) < 2 {
			return time.Time{}, true, fmt.Errorf("expected a weekday after next")
		}
		word, strict = parts[1], true
		parts = parts[1:]
	}
	weekday, ok := weekdays[word]
	if !ok {
		return time.Time{}, false, nil
	}
	hour, minute := 9, 0
	if len(parts) > 1 {
		clock, parsed, err := parseClock(strings.Join(parts[1:], " "), now)
		if !parsed || err != nil {
			return time.Time{}, true, fmt.Errorf("expected a clock time after %s", word)
		}
		hour, minute = clock.Hour(), clock.Minute()
	}
	days := (int(weekday) - int(now.Weekday()) + 7) % 7
	if strict || days == 0 {
		days += 7
	}
	return clockOnDate(now.AddDate(0, 0, days), hour, minute), true, nil
}

func parseDate(s string, now time.Time) (time.Time, bool, error) {
	if m := isoDateRe.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		hour, minute, err := parseDateClock(m[4])
		if err != nil {
			return time.Time{}, true, err
		}
		at := time.Date(y, time.Month(mo), d, hour, minute, 0, 0, now.Location())
		if !at.After(now) {
			return time.Time{}, true, fmt.Errorf("%s is in the past", at.Format("2006-01-02 15:04"))
		}
		return at, true, nil
	}
	if m := monthDayRe.FindStringSubmatch(s); m != nil {
		mo := months[monthKey(m[1])]
		d, _ := strconv.Atoi(m[2])
		hour, minute, err := parseDateClock(m[3])
		if err != nil {
			return time.Time{}, true, err
		}
		y := now.Year()
		at := time.Date(y, mo, d, hour, minute, 0, 0, now.Location())
		if !at.After(now) {
			at = at.AddDate(1, 0, 0)
		}
		return at, true, nil
	}
	return time.Time{}, false, nil
}

func parseClock(s string, now time.Time) (time.Time, bool, error) {
	hour, minute, ok, err := parseClockParts(s)
	if !ok || err != nil {
		return time.Time{}, ok, err
	}
	y, mo, d := now.Date()
	at := time.Date(y, mo, d, hour, minute, 0, 0, now.Location())
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at, true, nil
}

func parseClockParts(s string) (int, int, bool, error) {
	m := clockRe.FindStringSubmatch(strings.TrimSpace(strings.ToLower(s)))
	if m == nil {
		return 0, 0, false, nil
	}
	hour, _ := strconv.Atoi(m[1])
	minute := 0
	if m[2] != "" {
		minute, _ = strconv.Atoi(m[2])
	}
	if minute > 59 {
		return 0, 0, true, fmt.Errorf("invalid minute %q", m[2])
	}
	if m[3] != "" {
		if hour < 1 || hour > 12 {
			return 0, 0, true, fmt.Errorf("invalid 12-hour clock %q", s)
		}
		if m[3] == "pm" && hour != 12 {
			hour += 12
		}
		if m[3] == "am" && hour == 12 {
			hour = 0
		}
	} else if hour > 23 {
		return 0, 0, true, fmt.Errorf("invalid clock time %q", s)
	}
	return hour, minute, true, nil
}

func parseDateClock(s string) (int, int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, 0, nil
	}
	hour, minute, ok, err := parseClockParts(strings.TrimSpace(s))
	if err != nil || !ok {
		return 0, 0, fmt.Errorf("invalid date clock %q", s)
	}
	return hour, minute, nil
}

func dateAt(day time.Time, hour, minute int) time.Time {
	return clockOnDate(day, hour, minute)
}

func clockOnDate(day time.Time, hour, minute int) time.Time {
	y, mo, d := day.Date()
	return time.Date(y, mo, d, hour, minute, 0, 0, day.Location())
}

func nextWeekdayAt(now time.Time, weekday time.Weekday, hour, minute int, strict bool) time.Time {
	days := (int(weekday) - int(now.Weekday()) + 7) % 7
	if strict || days == 0 {
		days += 7
	}
	return clockOnDate(now.AddDate(0, 0, days), hour, minute)
}

func ceilHour(t time.Time) time.Time {
	if t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return t
	}
	return t.Truncate(time.Hour).Add(time.Hour)
}

func monthKey(s string) string {
	s = strings.ToLower(s)
	if len(s) > 3 {
		return s
	}
	return s
}
