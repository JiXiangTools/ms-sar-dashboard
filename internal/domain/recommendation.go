package domain

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// This is the wire format discriminator, not a business definition version.
const RecommendationSchemaVersion = 3
const (
	RecommendationPersonalized = "personalized"
	RecommendationRelated      = "related"
)

var RecommendationIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var StrategyIDPattern = RecommendationIDPattern
var keyPlaceholderPattern = regexp.MustCompile(`\{[^{}]*\}`)
var behaviorICFKeyPrefixes = map[string]string{
	"click_icf":     "ckicf",
	"like_icf":      "like_icf",
	"comshare_icf":  "comshare_icf",
	"subscribe_icf": "subscribe_icf",
	"tipby_icf":     "tipby_icf",
}

type RecallSource struct {
	RecallID    string    `json:"recall_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Producer    string    `json:"producer"`
	HotPeriod   string    `json:"hot_period,omitempty"`
	KeyTemplate string    `json:"key_template"`
	ValueFormat string    `json:"value_format"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}
type StrategyRecall struct {
	RecallID   string  `json:"recall_id"`
	Weight     float64 `json:"weight"`
	RecallRate float64 `json:"recall_rate"`
	Disabled   bool    `json:"disabled"`
}
type RecommendationStrategy struct {
	RecommendationType string           `json:"recommendation_type"`
	AppID              string           `json:"appid,omitempty"`
	StrategyID         string           `json:"strategy_id"`
	Name               string           `json:"name"`
	Recall             []StrategyRecall `json:"recall"`
	Fallback           []StrategyRecall `json:"fallback"`
	Rank               string           `json:"rank"`
	CreatedAt          time.Time        `json:"created_at,omitempty"`
}

func (s RecommendationStrategy) EffectiveType() string { return s.RecommendationType }

type ExperimentAssignment struct {
	StrategyID  string `json:"strategy_id"`
	BucketStart int    `json:"bucket_start"`
	BucketEnd   int    `json:"bucket_end"`
}
type RecommendationExperiment struct {
	AppID        string                 `json:"appid"`
	ExperimentID string                 `json:"experiment_id"`
	Name         string                 `json:"name"`
	BucketBy     string                 `json:"bucket_by"`
	Assignments  []ExperimentAssignment `json:"assignments"`
}
type ExperimentDefinition struct {
	RecommendationExperiment
	Status    string               `json:"status"`
	CreatedAt time.Time            `json:"created_at"`
	StartedAt *time.Time           `json:"started_at"`
	EndedAt   *time.Time           `json:"ended_at"`
	Snapshot  RecommendationConfig `json:"snapshot"`
}
type RecommendationConfig struct {
	SchemaVersion int                       `json:"schema_version"`
	AppID         string                    `json:"appid,omitempty"`
	PublishedAt   int64                     `json:"published_at,omitempty"`
	Experiment    *RecommendationExperiment `json:"experiment"`
	Recalls       []RecallSource            `json:"recalls"`
	Strategies    []RecommendationStrategy  `json:"strategies"`
}
type RecommendationPublication struct {
	AppID   string               `json:"appid"`
	Pending bool                 `json:"pending"`
	Config  RecommendationConfig `json:"config"`
}

func ValidateRecallSource(s RecallSource) error {
	if !RecommendationIDPattern.MatchString(s.RecallID) || strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 64 {
		return errors.New("invalid recall id or name")
	}
	if s.Kind != "item" && s.Kind != "user" && s.Kind != "global" {
		return errors.New("source kind must be item, user or global")
	}
	prefix, behaviorICF := behaviorICFKeyPrefixes[s.Producer]
	if !behaviorICF {
		prefix = s.Producer
	}
	if !behaviorICF && s.Producer != "icf" && s.Producer != "ir" && s.Producer != "nl" && s.Producer != "hot" && s.Producer != "external" {
		return errors.New("unsupported source producer")
	}
	if s.ValueFormat != "compact_score" {
		return errors.New("only compact_score is supported")
	}
	if len(s.KeyTemplate) == 0 || len(s.KeyTemplate) > 256 || strings.ContainsAny(s.KeyTemplate, "*?[]\r\n\x03 \t") || !strings.Contains(s.KeyTemplate, "{appid}") {
		return errors.New("key template requires {appid} and cannot contain wildcard or whitespace")
	}
	allowed := map[string]bool{"{appid}": true}
	if s.Kind == "item" {
		allowed["{itemid}"] = true
		if !strings.Contains(s.KeyTemplate, "{itemid}") {
			return errors.New("item source requires {itemid}")
		}
	}
	if s.Kind == "user" {
		allowed["{userid}"] = true
		if !strings.Contains(s.KeyTemplate, "{userid}") {
			return errors.New("user source requires {userid}")
		}
	}
	for _, p := range keyPlaceholderPattern.FindAllString(s.KeyTemplate, -1) {
		if !allowed[p] {
			return fmt.Errorf("unsupported placeholder %s", p)
		}
	}
	if strings.ContainsAny(keyPlaceholderPattern.ReplaceAllString(s.KeyTemplate, ""), "{}") {
		return errors.New("invalid template braces")
	}
	if s.Kind == "user" && s.Producer != "external" {
		return errors.New("user recall requires an external producer")
	}
	if s.Producer == "external" && strings.HasPrefix(s.KeyTemplate, "hot_") {
		return errors.New("hot_ keys are reserved for period rankings and nearline newest")
	}
	if (behaviorICF || s.Producer == "icf" || s.Producer == "ir" || s.Producer == "nl") && s.Kind != "item" {
		return errors.New("similarity producers require an item source")
	}
	if s.Producer == "hot" && s.Kind != "global" {
		return errors.New("hot producer requires a global source")
	}
	if s.Producer == "hot" {
		if !map[string]bool{"hour": true, "day": true, "week": true, "month": true, "quarter": true, "all": true, "newest": true}[s.HotPeriod] {
			return errors.New("hot producer requires a valid hot_period")
		}
		if s.KeyTemplate != "hot_{appid}_"+s.HotPeriod {
			return errors.New("hot sources must use hot_{appid}_<hot_period>; newest is maintained by nearline")
		}
	} else if s.HotPeriod != "" {
		return errors.New("hot_period is only valid for a hot producer")
	}
	if s.Producer != "external" && !strings.HasPrefix(s.KeyTemplate, prefix+"_") {
		return fmt.Errorf("built-in producer templates must start with %s_", prefix)
	}
	if !strings.Contains(s.KeyTemplate, "_{appid}_") && !strings.HasSuffix(s.KeyTemplate, "_{appid}") && !strings.HasPrefix(s.KeyTemplate, "{appid}_") {
		return errors.New("{appid} must be a separate underscore-delimited key segment")
	}
	return nil
}

func ValidateRecommendationStrategy(s RecommendationStrategy, recalls map[string]RecallSource) error {
	return validateStrategy(s, recalls, false)
}
func ValidateDefaultStrategy(s RecommendationStrategy, recalls map[string]RecallSource) error {
	return validateStrategy(s, recalls, true)
}
func validateStrategy(s RecommendationStrategy, recalls map[string]RecallSource, local bool) error {
	if s.RecommendationType != RecommendationPersonalized && s.RecommendationType != RecommendationRelated {
		return errors.New("recommendation_type must be personalized or related")
	}
	if !StrategyIDPattern.MatchString(s.StrategyID) || (s.StrategyID == "default") != local || strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 64 {
		return errors.New("invalid strategy id or name; default is reserved for Online defaults")
	}
	if s.Fallback == nil || len(s.Recall) == 0 || len(s.Recall) > 16 || len(s.Fallback) > 16 || s.Rank != "recall_weight" {
		return errors.New("select 1–16 recalls, at most 16 fallbacks and recall_weight ranking")
	}
	if s.RecommendationType == RecommendationRelated && len(s.Fallback) != 0 {
		return errors.New("related strategies support recall and rank only")
	}
	for stage, routes := range [][]StrategyRecall{s.Recall, s.Fallback} {
		seen := map[string]bool{}
		for _, route := range routes {
			recall, ok := recalls[route.RecallID]
			if !ok || seen[route.RecallID] {
				return errors.New("unknown or duplicate recall id within a stage")
			}
			seen[route.RecallID] = true
			if stage == 1 && recall.Kind != "global" {
				return errors.New("fallback requires global recalls")
			}
			if math.IsNaN(route.Weight) || math.IsInf(route.Weight, 0) || route.Weight <= 0 || route.Weight > 1000 || math.IsNaN(route.RecallRate) || math.IsInf(route.RecallRate, 0) || route.RecallRate <= 0 || route.RecallRate > 1 {
				return errors.New("weight must be >0 and <=1000; recall_rate must be >0 and <=1")
			}
		}
	}
	return nil
}
func ValidateRecommendationExperiment(e RecommendationExperiment) error {
	if !RecommendationIDPattern.MatchString(e.ExperimentID) || strings.TrimSpace(e.Name) == "" || len([]rune(e.Name)) > 64 {
		return errors.New("invalid experiment id or name")
	}
	if e.BucketBy != "user_id" && e.BucketBy != "anonymous_id" {
		return errors.New("bucket_by must be user_id or anonymous_id")
	}
	if len(e.Assignments) == 0 || len(e.Assignments) > 32 {
		return errors.New("select 1–32 assignments")
	}
	seen := map[string]bool{}
	for _, a := range e.Assignments {
		if !StrategyIDPattern.MatchString(a.StrategyID) || a.StrategyID == "default" || seen[a.StrategyID] || a.BucketStart < 0 || a.BucketEnd > 99 || a.BucketEnd < a.BucketStart {
			return errors.New("each strategy requires a unique range of 1–100 buckets within 0–99")
		}
		seen[a.StrategyID] = true
	}
	return nil
}
func ValidateRecommendationAssignments(e RecommendationExperiment, strategies map[string]RecommendationStrategy) error {
	if err := ValidateRecommendationExperiment(e); err != nil {
		return err
	}
	used := map[string][100]bool{}
	for _, a := range e.Assignments {
		s, ok := strategies[a.StrategyID]
		if !ok {
			return errors.New("assignment references missing strategy")
		}
		kind := s.RecommendationType
		if kind != RecommendationPersonalized && kind != RecommendationRelated {
			return errors.New("invalid recommendation type")
		}
		buckets := used[kind]
		for b := a.BucketStart; b <= a.BucketEnd; b++ {
			if buckets[b] {
				return errors.New("bucket ranges overlap within recommendation type " + kind)
			}
			buckets[b] = true
		}
		used[kind] = buckets
	}
	return nil
}
