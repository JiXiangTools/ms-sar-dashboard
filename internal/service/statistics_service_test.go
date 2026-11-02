package service

import (
	"context"
	"testing"
	"time"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
)

type fakeStatisticsRepository struct {
	days     []domain.StatisticsDay
	snapshot *domain.StatisticsSnapshot
}

func (f *fakeStatisticsRepository) PutStatistics(context.Context, domain.StatisticsDay) (bool, error) {
	return true, nil
}
func (f *fakeStatisticsRepository) Statistics(context.Context, int64, string, string) ([]domain.StatisticsDay, *domain.StatisticsSnapshot, error) {
	return f.days, f.snapshot, nil
}
func testStatisticsDay(date string, exposures, clicks int64) domain.StatisticsDay {
	dateTime, _ := time.ParseInLocation(time.DateOnly, date, statisticsLocation)
	d := domain.StatisticsDay{SchemaVersion: 1, AppID: "100001", Date: date, Timezone: StatisticsTimezone, Revision: 1, GeneratedAt: dateTime.Add(24 * time.Hour).Unix(), Counts: emptyStatisticsCounts(), Snapshot: domain.StatisticsSnapshot{GeneratedAt: dateTime.Add(24 * time.Hour).Unix(), HistorySince: date}}
	for h := 0; h < 24; h++ {
		d.Hours = append(d.Hours, domain.StatisticsHour{Hour: h, Counts: emptyStatisticsCounts()})
	}
	d.Hours[0].Counts.Exposures = exposures
	d.Hours[0].Counts.Clicks = clicks
	d.Hours[0].Counts.Events = map[string]int64{"show": exposures, "click": clicks}
	addStatisticsCounts(&d.Counts, d.Hours[0].Counts)
	return d
}
func TestValidateStatistics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domain.StatisticsDay)
		valid  bool
	}{
		{"valid", func(d *domain.StatisticsDay) {}, true},
		{"noncanonical app", func(d *domain.StatisticsDay) { d.AppID = "0100001" }, false},
		{"negative event", func(d *domain.StatisticsDay) { d.Counts.Events["tip"] = -1 }, false},
		{"incorrect total", func(d *domain.StatisticsDay) { d.Counts.Clicks++ }, false},
		{"missing hour", func(d *domain.StatisticsDay) { d.Hours = d.Hours[:23] }, false},
		{"wrong timezone", func(d *domain.StatisticsDay) { d.Timezone = "UTC" }, false},
		{"duplicate hour", func(d *domain.StatisticsDay) { d.Hours[1].Hour = 0 }, false},
		{"impossible stock", func(d *domain.StatisticsDay) { d.Snapshot.ItemAvailable = 2 }, false},
		{"unknown raw event field", func(d *domain.StatisticsDay) { d.Counts.Events["user@example.com"] = 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testStatisticsDay("2026-01-01", 10, 2)
			tc.mutate(&d)
			if err := validateStatistics(d, time.Now()); (err == nil) != tc.valid {
				t.Fatalf("err=%v valid=%v", err, tc.valid)
			}
		})
	}
}
func TestStatisticsQueryWeightedCTRAndMissingBuckets(t *testing.T) {
	repo := &fakeStatisticsRepository{days: []domain.StatisticsDay{testStatisticsDay("2026-01-01", 10, 5), testStatisticsDay("2026-01-02", 90, 9)}}
	s := NewStatisticsService(repo)
	result, err := s.Query(context.Background(), "100001", "2026-01-01", "2026-01-03", "day")
	if err != nil {
		t.Fatal(err)
	}
	if result.CTR == nil || *result.CTR != .14 {
		t.Fatalf("CTR must be 14%%, got %v", result.CTR)
	}
	if result.MissingBuckets != 1 || result.Points[2].Counts != nil {
		t.Fatal("missing buckets must not become zero")
	}
	hourly, err := s.Query(context.Background(), "100001", "2026-01-01", "2026-01-01", "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly.Points) != 24 || hourly.Points[1].Counts == nil || hourly.Points[1].CTR != nil {
		t.Fatal("known zero is ready with null CTR")
	}
	for _, app := range []string{"0", "-1", "bad", "0100001"} {
		if _, err := s.Query(context.Background(), app, "2026-01-01", "2026-01-01", "day"); err == nil {
			t.Fatalf("accepted invalid app %s", app)
		}
	}
	if _, err := s.Query(context.Background(), "100001", "2026-01-01", "2026-04-01", "day"); err == nil {
		t.Fatal("accepted oversized range")
	}
}

func TestStatisticsQueryFlagsUnfinishedHistoricalDay(t *testing.T) {
	day := testStatisticsDay("2026-01-01", 10, 2)
	day.GeneratedAt -= 3600 // Last upload stopped before the day finished.
	repo := &fakeStatisticsRepository{
		days:     []domain.StatisticsDay{day},
		snapshot: &domain.StatisticsSnapshot{GeneratedAt: time.Now().Unix()},
	}
	result, err := NewStatisticsService(repo).Query(context.Background(), "100001", day.Date, day.Date, "day")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stale || !result.Points[0].Partial {
		t.Fatal("a fresh inventory snapshot must not hide unfinished historical statistics")
	}
}
