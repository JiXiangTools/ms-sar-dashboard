package domain

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

const RecommendationSchemaVersion = 2

const (
	RecommendationPersonalized = "personalized"
	RecommendationRelated      = "related"
)

var StrategyIDPattern = regexp.MustCompile(`^[a-z][a-z_]{0,9}$`)
var RecommendationIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var keyPlaceholderPattern = regexp.MustCompile(`\{[^{}]*\}`)

type RecallSource struct {
	SourceID    string    `json:"source_id"`
	Version     int64     `json:"version"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Producer    string    `json:"producer"`
	HotPeriod   string    `json:"hot_period,omitempty"`
	KeyTemplate string    `json:"key_template"`
	ValueFormat string    `json:"value_format"`
	CreatedAt   time.Time `json:"created_at"`
}

type StrategyRecall struct {
	SourceID   string  `json:"source_id"`
	Weight     float64 `json:"weight"`
	RecallRate float64 `json:"recall_rate"`
	Disabled   bool    `json:"disabled"`
}

type StrategyNewest struct {
	SourceID     string   `json:"source_id"`
	SharePercent int      `json:"share_percent"`
	Scenes       []string `json:"scenes"`
}

func (n StrategyNewest) IsZero() bool {
	return n.SourceID == "" && n.SharePercent == 0 && len(n.Scenes) == 0
}

type RecommendationStrategy struct {
	RecommendationType string           `json:"recommendation_type,omitempty"`
	AppID              string           `json:"appid"`
	StrategyID         string           `json:"strategy_id"`
	Version            int64            `json:"version"`
	Name               string           `json:"name"`
	Recall             []StrategyRecall `json:"recall"`
	FallbackSourceID   string           `json:"fallback_source_id"`
	Newest             StrategyNewest   `json:"newest,omitzero"` // Read-only compatibility for historical versions.
	Rank               string           `json:"rank"`
	CreatedAt          time.Time        `json:"created_at"`
}

// Old immutable snapshots predate strategy types and remain personalized.
func (s RecommendationStrategy) EffectiveType() string {
	if s.RecommendationType == "" {
		return RecommendationPersonalized
	}
	return s.RecommendationType
}

type ExperimentAssignment struct {
	StrategyID      string `json:"strategy_id"`
	StrategyVersion int64  `json:"strategy_version"`
	BucketStart     int    `json:"bucket_start"`
	BucketEnd       int    `json:"bucket_end"`
}

type ExperimentControl struct {
	StrategyID      string `json:"strategy_id"`
	StrategyVersion int64  `json:"strategy_version"`
}

type RecommendationExperiment struct {
	AppID           string                 `json:"appid"`
	ExperimentID    string                 `json:"experiment_id"`
	Version         int64                  `json:"experiment_version"`
	Name            string                 `json:"name"`
	BucketBy        string                 `json:"bucket_by"`
	BucketAlgorithm string                 `json:"bucket_algorithm"`
	Salt            string                 `json:"salt"`
	Assignments     []ExperimentAssignment `json:"assignments"`
	Controls        []ExperimentControl    `json:"controls,omitempty"`
}

type ExperimentVersion struct {
	RecommendationExperiment
	Status    string               `json:"status"`
	CreatedAt time.Time            `json:"created_at"`
	StartedAt *time.Time           `json:"started_at"`
	EndedAt   *time.Time           `json:"ended_at"`
	Snapshot  RecommendationConfig `json:"snapshot"`
}

// Each publication is a complete, immutable snapshot. Redis contains only the
// most recently published snapshot; historical snapshots remain in PostgreSQL.
type RecommendationConfig struct {
	SchemaVersion int                       `json:"schema_version"`
	AppID         string                    `json:"appid"`
	Revision      int64                     `json:"revision"`
	PublishedAt   int64                     `json:"published_at"`
	Experiment    *RecommendationExperiment `json:"experiment"`
	Sources       []RecallSource            `json:"sources"`
	Strategies    []RecommendationStrategy  `json:"strategies"`
}

type RecommendationPublication struct {
	AppID             string               `json:"appid"`
	Revision          int64                `json:"revision"`
	PublishedRevision int64                `json:"published_revision"`
	Config            RecommendationConfig `json:"config"`
}

func ValidateRecallSource(s RecallSource) error {
	if !RecommendationIDPattern.MatchString(s.SourceID) || strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 64 {
		return errors.New("invalid source id or name")
	}
	if s.Kind != "item" && s.Kind != "user" && s.Kind != "global" {
		return errors.New("source kind must be item, user or global")
	}
	if s.Producer != "icf" && s.Producer != "ir" && s.Producer != "nl" && s.Producer != "hot" && s.Producer != "external" {
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
	if (s.Producer == "icf" || s.Producer == "ir" || s.Producer == "nl") && s.Kind != "item" {
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
	if s.Producer != "external" && !strings.HasPrefix(s.KeyTemplate, s.Producer+"_") {
		return errors.New("built-in producer templates must start with the producer name followed by an underscore")
	}
	if !strings.Contains(s.KeyTemplate, "_{appid}_") && !strings.HasSuffix(s.KeyTemplate, "_{appid}") && !strings.HasPrefix(s.KeyTemplate, "{appid}_") {
		return errors.New("{appid} must be a separate underscore-delimited key segment")
	}
	return nil
}

func ValidateRecommendationStrategy(s RecommendationStrategy, sources map[string]RecallSource) error {
	if !s.Newest.IsZero() {
		return errors.New("configure newest as a recall source with weight and recall_rate, not a separate newest stage")
	}
	return ValidatePublishedRecommendationStrategy(s, sources)
}

// Historical snapshots retain their original behavior; new definitions use recall.
func ValidatePublishedRecommendationStrategy(s RecommendationStrategy, sources map[string]RecallSource) error {
	if s.EffectiveType() != RecommendationPersonalized && s.EffectiveType() != RecommendationRelated {
		return errors.New("recommendation_type must be personalized or related")
	}
	if s.EffectiveType() == RecommendationRelated && (s.FallbackSourceID != "" || s.Newest.SourceID != "" || s.Newest.SharePercent != 0 || len(s.Newest.Scenes) != 0) {
		return errors.New("related strategies support recall and rank only; fallback and a separate newest stage are not allowed")
	}
	if !StrategyIDPattern.MatchString(s.StrategyID) || s.StrategyID == "default" || strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 64 {
		return errors.New("strategy id must be 1–10 lowercase letters/underscores, start with a letter, and not be default")
	}
	if len(s.Recall) == 0 || len(s.Recall) > 16 || s.Rank != "recall_weight" {
		return errors.New("select 1–16 recall sources and recall_weight ranking")
	}
	seen := map[string]bool{}
	for _, r := range s.Recall {
		if _, ok := sources[r.SourceID]; !ok || seen[r.SourceID] {
			return errors.New("unknown or duplicate recall source")
		}
		seen[r.SourceID] = true
		if math.IsNaN(r.Weight) || math.IsInf(r.Weight, 0) || r.Weight <= 0 || r.Weight > 1000 || math.IsNaN(r.RecallRate) || math.IsInf(r.RecallRate, 0) || r.RecallRate <= 0 || r.RecallRate > 1 {
			return errors.New("weight must be >0 and <=1000; recall_rate must be >0 and <=1")
		}
	}
	for _, id := range []string{s.FallbackSourceID, s.Newest.SourceID} {
		if id != "" {
			source, ok := sources[id]
			if !ok || source.Kind != "global" {
				return errors.New("fallback/newest require a global source")
			}
		}
	}
	if s.Newest.SharePercent < 0 || s.Newest.SharePercent > 100 || len(s.Newest.Scenes) > 32 || (s.Newest.SharePercent > 0 && (s.Newest.SourceID == "" || len(s.Newest.Scenes) == 0)) {
		return errors.New("invalid newest percentage or scenes")
	}
	for _, scene := range s.Newest.Scenes {
		if len(scene) == 0 || len(scene) > 32 {
			return errors.New("invalid newest scene")
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
	if e.BucketAlgorithm != "sha256" {
		return errors.New("bucket algorithm must be sha256")
	}
	if len(e.Salt) > 64 || len(e.Assignments) == 0 || len(e.Assignments) > 32 {
		return errors.New("invalid experiment salt or assignments")
	}
	strategies := map[string]bool{}
	for _, a := range e.Assignments {
		if !StrategyIDPattern.MatchString(a.StrategyID) || a.StrategyID == "default" || strategies[a.StrategyID] || a.BucketStart < 0 || a.BucketEnd > 99 || a.BucketEnd < a.BucketStart || a.BucketEnd-a.BucketStart+1 > 99 {
			return errors.New("each strategy requires a unique, non-overlapping range of 1–99 buckets within 0–99")
		}
		strategies[a.StrategyID] = true
	}
	if len(e.Controls) > 2 {
		return errors.New("select at most one control strategy per recommendation type")
	}
	for _, control := range e.Controls {
		if !StrategyIDPattern.MatchString(control.StrategyID) || control.StrategyID == "default" || control.StrategyVersion < 0 || strategies[control.StrategyID] {
			return errors.New("control strategies must be distinct from treatment strategies and each other")
		}
		strategies[control.StrategyID] = true
	}
	return nil
}

// Schema v2 pins the default/control flow as well as each treatment flow.
func ValidateRecommendationControls(e RecommendationExperiment, strategies map[string]RecommendationStrategy) error {
	participating := map[string]bool{}
	for _, assignment := range e.Assignments {
		strategy, ok := strategies[assignment.StrategyID]
		if !ok {
			return errors.New("missing treatment strategy")
		}
		participating[strategy.EffectiveType()] = true
	}
	controls := map[string]bool{}
	for _, reference := range e.Controls {
		strategy, ok := strategies[reference.StrategyID]
		if !ok || strategy.Version != reference.StrategyVersion {
			return errors.New("control references missing strategy version")
		}
		kind := strategy.EffectiveType()
		if !participating[kind] || controls[kind] {
			return errors.New("select exactly one control for each participating recommendation type")
		}
		controls[kind] = true
	}
	if len(controls) != len(participating) {
		return errors.New("each participating recommendation type requires a pinned control strategy")
	}
	return nil
}

// A single App experiment can allocate both interfaces independently. Resolve
// the pinned strategy versions before checking ranges, never the latest edits.
func ValidateRecommendationAssignments(e RecommendationExperiment, strategies map[string]RecommendationStrategy) error {
	if err := ValidateRecommendationExperiment(e); err != nil {
		return err
	}
	used := map[string][100]bool{}
	for _, a := range e.Assignments {
		strategy, ok := strategies[a.StrategyID]
		if !ok || strategy.Version != a.StrategyVersion {
			return errors.New("assignment references missing strategy version")
		}
		kind := strategy.EffectiveType()
		if kind != RecommendationPersonalized && kind != RecommendationRelated {
			return errors.New("invalid recommendation type in assignment")
		}
		buckets := used[kind]
		for bucket := a.BucketStart; bucket <= a.BucketEnd; bucket++ {
			if buckets[bucket] {
				return errors.New("bucket ranges overlap within recommendation type " + kind)
			}
			buckets[bucket] = true
		}
		used[kind] = buckets
	}
	return nil
}
