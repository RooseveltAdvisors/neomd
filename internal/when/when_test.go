package when

import (
	"testing"
	"time"
)

func TestParseNaturalLanguageTimes(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 7, 10, 30, 0, 0, loc) // Monday
	tests := []struct {
		input string
		want  time.Time
	}{
		{"in 20 minutes", time.Date(2026, 9, 7, 10, 50, 0, 0, loc)},
		{"in 3 hours", time.Date(2026, 9, 7, 13, 30, 0, 0, loc)},
		{"in 2 days", time.Date(2026, 9, 9, 10, 30, 0, 0, loc)},
		{"in a week", time.Date(2026, 9, 14, 10, 30, 0, 0, loc)},
		{"+2h", time.Date(2026, 9, 7, 12, 30, 0, 0, loc)},
		{"friday", time.Date(2026, 9, 11, 9, 0, 0, 0, loc)},
		{"next monday", time.Date(2026, 9, 14, 9, 0, 0, 0, loc)},
		{"sat 2pm", time.Date(2026, 9, 12, 14, 0, 0, 0, loc)},
		{"tonight", time.Date(2026, 9, 7, 18, 0, 0, 0, loc)},
		{"tomorrow morning", time.Date(2026, 9, 8, 9, 0, 0, 0, loc)},
		{"tomorrow afternoon", time.Date(2026, 9, 8, 14, 0, 0, 0, loc)},
		{"tomorrow evening", time.Date(2026, 9, 8, 18, 0, 0, 0, loc)},
		{"17:30", time.Date(2026, 9, 7, 17, 30, 0, 0, loc)},
		{"9am", time.Date(2026, 9, 8, 9, 0, 0, 0, loc)},
		{"sep 12", time.Date(2026, 9, 12, 0, 0, 0, 0, loc)},
		{"2026-09-12 09:00", time.Date(2026, 9, 12, 9, 0, 0, 0, loc)},
		{"later today", time.Date(2026, 9, 7, 14, 0, 0, 0, loc)},
		{"this weekend", time.Date(2026, 9, 12, 9, 0, 0, 0, loc)},
		{"next week", time.Date(2026, 9, 14, 9, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse(tt.input, now)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("Parse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseKeepsLegacySendLaterInputs(t *testing.T) {
	now := time.Date(2026, 8, 24, 14, 30, 0, 0, time.UTC)
	for _, input := range []string{"30m", "+1d", "+1d2h", "tomorrow", "tomorrow 17:30", "2026-08-25 08:15"} {
		if _, err := Parse(input, now); err != nil {
			t.Errorf("Parse(%q): %v", input, err)
		}
	}
}

func TestParsePastClockRollsToTomorrow(t *testing.T) {
	loc := time.FixedZone("EDT", -4*60*60)
	now := time.Date(2026, 9, 7, 18, 0, 0, 0, loc)
	got, err := Parse("17:30", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 8, 17, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("past clock = %v, want %v", got, want)
	}
}

func TestParseTomorrowClockDoesNotSkipAnExtraDay(t *testing.T) {
	loc := time.FixedZone("EDT", -4*60*60)
	now := time.Date(2026, 9, 7, 18, 0, 0, 0, loc)
	got, err := Parse("tomorrow 17:30", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 8, 17, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("tomorrow clock = %v, want %v", got, want)
	}
}

func TestParseTonightAfterSixRollsToNextEvening(t *testing.T) {
	loc := time.FixedZone("EDT", -4*60*60)
	now := time.Date(2026, 9, 7, 19, 0, 0, 0, loc)
	got, err := Parse("tonight", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 8, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("tonight = %v, want %v", got, want)
	}
}

func TestParseWeekdayAndWeekendRollForward(t *testing.T) {
	loc := time.FixedZone("EDT", -4*60*60)
	saturday := time.Date(2026, 9, 12, 10, 0, 0, 0, loc)
	got, err := Parse("this weekend", saturday)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 19, 9, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("weekend rollover = %v, want %v", got, want)
	}

	friday := time.Date(2026, 9, 11, 15, 0, 0, 0, loc)
	got, err = Parse("friday 2pm", friday)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 18, 14, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("past named-day time = %v, want %v", got, want)
	}
}

func TestParseDSTCalendarArithmetic(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 7, 10, 0, 0, 0, loc)
	got, err := Parse("in 2 days", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 3, 9, 10, 0, 0, 0, loc)
	if !got.Equal(want) || got.Hour() != 10 {
		t.Fatalf("DST result = %v, want local 10:00 at %v", got, want)
	}
}

func TestParseRejectsUnknownOrZeroTimes(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	for _, input := range []string{"", "yesterday", "25:99", "in zero days", "+0m", "banana"} {
		if _, err := Parse(input, now); err == nil {
			t.Errorf("Parse(%q): expected error", input)
		}
	}
}
