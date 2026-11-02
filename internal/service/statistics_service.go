package service

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/apperror"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/domain"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/repository"
)

const StatisticsTimezone = "Asia/Shanghai"

var statisticsLocation, _ = time.LoadLocation(StatisticsTimezone)
var statisticsEventPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

type statisticsRepository interface {
	PutStatistics(context.Context, domain.StatisticsDay) (bool, error)
	Statistics(context.Context, int64, string, string) ([]domain.StatisticsDay, *domain.StatisticsSnapshot, error)
}
type StatisticsService struct{ repo statisticsRepository }

func NewStatisticsService(repo statisticsRepository) *StatisticsService {
	return &StatisticsService{repo: repo}
}

type StatisticsPoint struct {
	Time          string                   `json:"time"`
	Counts        *domain.StatisticsCounts `json:"counts"`
	ReportedItems *int64                   `json:"reported_items"`
	CTR           *float64                 `json:"ctr"`
	GeneratedAt   int64                    `json:"generated_at,omitempty"`
	Partial       bool                     `json:"partial"`
}
type StatisticsResult struct {
	AppID          string                     `json:"appid"`
	Timezone       string                     `json:"timezone"`
	Granularity    string                     `json:"granularity"`
	Snapshot       *domain.StatisticsSnapshot `json:"snapshot"`
	Summary        domain.StatisticsCounts    `json:"summary"`
	CTR            *float64                   `json:"ctr"`
	Points         []StatisticsPoint          `json:"points"`
	MissingBuckets int                        `json:"missing_buckets"`
	Stale          bool                       `json:"stale"`
}

func (s *StatisticsService) Put(ctx context.Context, pathAppID, pathDate string, input domain.StatisticsDay) (bool, error) {
	if pathAppID != input.AppID || pathDate != input.Date {
		return false, apperror.BadRequest("statistics path and payload mismatch", nil)
	}
	if err := validateStatistics(input, time.Now()); err != nil {
		return false, apperror.BadRequest(err.Error(), nil)
	}
	changed, err := s.repo.PutStatistics(ctx, input)
	if errors.Is(err, repository.ErrConflict) {
		return false, apperror.Conflict("stale statistics revision or conflicting retry", nil)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return false, apperror.NotFound("app not found", nil)
	}
	if err != nil {
		return false, apperror.Internal("write statistics failed", err)
	}
	return changed, nil
}

func validateStatistics(v domain.StatisticsDay, now time.Time) error {
	invalid := errors.New("invalid statistics snapshot")
	appid, err := strconv.ParseInt(v.AppID, 10, 64)
	if err != nil || appid <= 0 || strconv.FormatInt(appid, 10) != v.AppID {
		return invalid
	}
	day, err := time.ParseInLocation(time.DateOnly, v.Date, statisticsLocation)
	if err != nil || v.SchemaVersion != 1 || v.Timezone != StatisticsTimezone || v.Revision <= 0 || v.Revision > 9007199254740991 {
		return invalid
	}
	if v.GeneratedAt < day.Unix() || v.GeneratedAt > now.Unix()+300 || len(v.Hours) != 24 {
		return invalid
	}
	if v.Snapshot.GeneratedAt != v.GeneratedAt || v.Snapshot.ItemAvailable < 0 || v.Snapshot.ItemTotal < v.Snapshot.ItemAvailable || v.Snapshot.ItemTotal > 1e12 {
		return invalid
	}
	since, err := time.ParseInLocation(time.DateOnly, v.Snapshot.HistorySince, statisticsLocation)
	if err != nil || since.After(day) {
		return invalid
	}
	if !validStatisticsCounts(v.Counts) {
		return invalid
	}
	summed := emptyStatisticsCounts()
	var uniqueSum, uniqueMax int64
	for index, hour := range v.Hours {
		if hour.Hour != index || !validStatisticsCounts(hour.Counts) || hour.ReportedItems < 0 || hour.ReportedItems > hour.Counts.ItemReports {
			return invalid
		}
		if day.Add(time.Duration(index)*time.Hour).Unix() > v.GeneratedAt && !zeroStatisticsCounts(hour.Counts) {
			return invalid
		}
		addStatisticsCounts(&summed, hour.Counts)
		uniqueSum += hour.ReportedItems
		if hour.ReportedItems > uniqueMax {
			uniqueMax = hour.ReportedItems
		}
	}
	if v.ReportedItems < uniqueMax || v.ReportedItems > uniqueSum || v.ReportedItems > v.Counts.ItemReports {
		return invalid
	}
	if !equalStatisticsCounts(summed, v.Counts) {
		return errors.New("statistics totals must equal hourly totals")
	}
	return nil
}

func validStatisticsCounts(c domain.StatisticsCounts) bool {
	for _, n := range []int64{c.ItemReports, c.NewItems, c.Exposures, c.Clicks, c.MissingEventIDs, c.DuplicateEvents} {
		if n < 0 || n > 1e12 {
			return false
		}
	}
	if c.NewItems > c.ItemReports || len(c.Events) > 64 || c.Events == nil || c.Exposures > c.Events["show"] || c.Clicks > c.Events["click"] {
		return false
	}
	for name, n := range c.Events {
		if !statisticsEventPattern.MatchString(name) || n < 0 || n > 1e12 {
			return false
		}
	}
	return true
}
func emptyStatisticsCounts() domain.StatisticsCounts {
	return domain.StatisticsCounts{Events: map[string]int64{}}
}
func addStatisticsCounts(a *domain.StatisticsCounts, b domain.StatisticsCounts) {
	a.ItemReports += b.ItemReports
	a.NewItems += b.NewItems
	a.Exposures += b.Exposures
	a.Clicks += b.Clicks
	a.MissingEventIDs += b.MissingEventIDs
	a.DuplicateEvents += b.DuplicateEvents
	for k, v := range b.Events {
		a.Events[k] += v
	}
}
func equalStatisticsCounts(a, b domain.StatisticsCounts) bool {
	if a.ItemReports != b.ItemReports || a.NewItems != b.NewItems || a.Exposures != b.Exposures || a.Clicks != b.Clicks || a.MissingEventIDs != b.MissingEventIDs || a.DuplicateEvents != b.DuplicateEvents {
		return false
	}
	for k, v := range a.Events {
		if b.Events[k] != v {
			return false
		}
	}
	for k, v := range b.Events {
		if a.Events[k] != v {
			return false
		}
	}
	return true
}
func zeroStatisticsCounts(c domain.StatisticsCounts) bool {
	return equalStatisticsCounts(c, emptyStatisticsCounts())
}
func statisticsCTR(c domain.StatisticsCounts) *float64 {
	if c.Exposures == 0 {
		return nil
	}
	value := float64(c.Clicks) / float64(c.Exposures)
	return &value
}

func (s *StatisticsService) Query(ctx context.Context, appID, from, to, granularity string) (StatisticsResult, error) {
	result := StatisticsResult{AppID: appID, Timezone: StatisticsTimezone, Granularity: granularity, Summary: emptyStatisticsCounts(), Points: []StatisticsPoint{}}
	appid, err := strconv.ParseInt(appID, 10, 64)
	if err != nil || appid <= 0 || strconv.FormatInt(appid, 10) != appID {
		return result, apperror.BadRequest("invalid appid", nil)
	}
	start, e1 := time.ParseInLocation(time.DateOnly, from, statisticsLocation)
	end, e2 := time.ParseInLocation(time.DateOnly, to, statisticsLocation)
	if e1 != nil || e2 != nil || end.Before(start) || end.Sub(start) >= 90*24*time.Hour || (granularity != "day" && granularity != "hour") {
		return result, apperror.BadRequest("use YYYY-MM-DD, a range of 1–90 days, and day or hour granularity", nil)
	}
	days, snapshot, err := s.repo.Statistics(ctx, appid, from, to)
	if errors.Is(err, repository.ErrNotFound) {
		return result, apperror.NotFound("app not found", nil)
	}
	if err != nil {
		return result, apperror.Internal("read statistics failed", err)
	}
	result.Snapshot = snapshot
	result.Stale = snapshot == nil || time.Now().Unix()-snapshot.GeneratedAt > 1800
	indexed := map[string]domain.StatisticsDay{}
	for _, d := range days {
		indexed[d.Date] = d
	}
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		d, exists := indexed[date.Format(time.DateOnly)]
		steps := 1
		if granularity == "hour" {
			steps = 24
		}
		for hour := 0; hour < steps; hour++ {
			at := date.Add(time.Duration(hour) * time.Hour)
			p := StatisticsPoint{Time: at.Format(time.RFC3339)}
			if exists && at.Unix() <= d.GeneratedAt {
				counts := d.Counts
				unique := d.ReportedItems
				width := 24 * time.Hour
				if granularity == "hour" {
					counts = d.Hours[hour].Counts
					unique = d.Hours[hour].ReportedItems
					width = time.Hour
				}
				p.Counts = &counts
				p.ReportedItems = &unique
				p.CTR = statisticsCTR(counts)
				p.GeneratedAt = d.GeneratedAt
				p.Partial = at.Add(width).Unix() > d.GeneratedAt
				addStatisticsCounts(&result.Summary, counts)
				if p.Partial && time.Now().Unix()-d.GeneratedAt > 1800 {
					result.Stale = true
				}
			} else {
				result.MissingBuckets++
			}
			result.Points = append(result.Points, p)
		}
	}
	result.CTR = statisticsCTR(result.Summary)
	return result, nil
}
