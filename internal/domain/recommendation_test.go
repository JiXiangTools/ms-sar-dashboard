package domain

import (
	"math"
	"testing"
)

func TestValidateRecallSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*RecallSource)
		valid  bool
	}{
		{"item", func(*RecallSource) {}, true},
		{"wildcard", func(s *RecallSource) { s.KeyTemplate = "icf_{appid}_*{itemid}" }, false},
		{"tenant missing", func(s *RecallSource) { s.KeyTemplate = "icf_{itemid}" }, false},
		{"tenant concatenation", func(s *RecallSource) { s.KeyTemplate = "icf_{appid}{itemid}" }, false},
		{"wrong identity", func(s *RecallSource) { s.KeyTemplate = "icf_{appid}_{userid}" }, false},
		{"broken braces", func(s *RecallSource) { s.KeyTemplate = "icf_{appid}_{itemid}{" }, false},
		{"reserved output", func(s *RecallSource) { s.KeyTemplate = "app_auth_{appid}_{itemid}" }, false},
		{"hot missing period", func(s *RecallSource) { s.Kind = "global"; s.Producer = "hot"; s.KeyTemplate = "hot_{appid}_day" }, false},
		{"hot day", func(s *RecallSource) {
			s.Kind = "global"
			s.Producer = "hot"
			s.HotPeriod = "day"
			s.KeyTemplate = "hot_{appid}_day"
		}, true},
		{"day cannot write newest", func(s *RecallSource) {
			s.Kind = "global"
			s.Producer = "hot"
			s.HotPeriod = "day"
			s.KeyTemplate = "hot_{appid}_newest"
		}, false},
		{"newest cannot write day", func(s *RecallSource) {
			s.Kind = "global"
			s.Producer = "hot"
			s.HotPeriod = "newest"
			s.KeyTemplate = "hot_{appid}_day"
		}, false},
		{"hot custom alias", func(s *RecallSource) {
			s.Kind = "global"
			s.Producer = "hot"
			s.HotPeriod = "day"
			s.KeyTemplate = "hot_{appid}_exp_day"
		}, false},
		{"external cannot own hot keys", func(s *RecallSource) {
			s.Kind = "global"
			s.Producer = "external"
			s.KeyTemplate = "hot_{appid}_newest"
		}, false},
		{"user external", func(s *RecallSource) {
			s.Kind = "user"
			s.Producer = "external"
			s.KeyTemplate = "ucf_{appid}_{userid}"
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := RecallSource{RecallID: "icf", Name: "Item CF", Kind: "item", Producer: "icf", KeyTemplate: "icf_{appid}_{itemid}", ValueFormat: "compact_score"}
			tc.change(&s)
			if err := ValidateRecallSource(s); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestRecallAndFallbackUseTheSameRouteValidation(t *testing.T) {
	recalls := map[string]RecallSource{"icf": {Kind: "item"}, "day": {Kind: "global"}, "hour": {Kind: "global"}}
	for _, tc := range []struct {
		name  string
		edit  func(*RecommendationStrategy)
		valid bool
	}{
		{"multiple fallbacks", func(*RecommendationStrategy) {}, true},
		{"item fallback", func(s *RecommendationStrategy) { s.Fallback[0].RecallID = "icf" }, false},
		{"duplicate fallback", func(s *RecommendationStrategy) { s.Fallback[1].RecallID = "day" }, false},
		{"disabled still validated", func(s *RecommendationStrategy) { s.Fallback[0].Disabled = true; s.Fallback[0].Weight = 0 }, false},
		{"invalid quota", func(s *RecommendationStrategy) { s.Fallback[0].RecallRate = 1.1 }, false},
		{"nonfinite weight", func(s *RecommendationStrategy) { s.Recall[0].Weight = math.NaN() }, false},
		{"related has no fallback", func(s *RecommendationStrategy) {
			s.RecommendationType = RecommendationRelated
			s.Fallback = []StrategyRecall{}
		}, true},
		{"fallback must be an array", func(s *RecommendationStrategy) { s.Fallback = nil }, false},
		{"related fallback", func(s *RecommendationStrategy) { s.RecommendationType = RecommendationRelated }, false},
		{"type required", func(s *RecommendationStrategy) { s.RecommendationType = "" }, false},
		{"default reserved", func(s *RecommendationStrategy) { s.StrategyID = "default" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := RecommendationStrategy{RecommendationType: RecommendationPersonalized, StrategyID: "strategy_a", Name: "策略", Rank: "recall_weight", Recall: []StrategyRecall{{RecallID: "icf", Weight: 1, RecallRate: 1}}, Fallback: []StrategyRecall{{RecallID: "day", Weight: 1, RecallRate: .5}, {RecallID: "hour", Weight: 2, RecallRate: 1}}}
			tc.edit(&s)
			if err := ValidateRecommendationStrategy(s, recalls); (err == nil) != tc.valid {
				t.Fatalf("valid %v: %v", tc.valid, err)
			}
		})
	}
}
func TestValidateRecommendationExperiment(t *testing.T) {
	for _, tc := range []struct {
		name        string
		assignments []ExperimentAssignment
		valid       bool
	}{
		{"one through twenty nine", []ExperimentAssignment{{StrategyID: "a", BucketStart: 1, BucketEnd: 29}}, true},
		{"two arms", []ExperimentAssignment{{StrategyID: "a", BucketStart: 0, BucketEnd: 49}, {StrategyID: "b", BucketStart: 50, BucketEnd: 99}}, true},
		{"overlap", []ExperimentAssignment{{StrategyID: "a", BucketStart: 1, BucketEnd: 29}, {StrategyID: "b", BucketStart: 29, BucketEnd: 59}}, false},
		{"hundred percent arm", []ExperimentAssignment{{StrategyID: "a", BucketStart: 0, BucketEnd: 99}}, true},
		{"negative bucket", []ExperimentAssignment{{StrategyID: "a", BucketStart: -1, BucketEnd: 98}}, false},
		{"bucket beyond limit", []ExperimentAssignment{{StrategyID: "a", BucketStart: 1, BucketEnd: 100}}, false},
		{"duplicate strategy", []ExperimentAssignment{{StrategyID: "a", BucketStart: 0, BucketEnd: 9}, {StrategyID: "a", BucketStart: 10, BucketEnd: 19}}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := RecommendationExperiment{ExperimentID: "exp_one", Name: "首页实验", BucketBy: "user_id", Assignments: tc.assignments}
			strategies := map[string]RecommendationStrategy{}
			for _, assignment := range e.Assignments {
				strategies[assignment.StrategyID] = RecommendationStrategy{StrategyID: assignment.StrategyID, RecommendationType: RecommendationPersonalized}
			}
			if err := ValidateRecommendationAssignments(e, strategies); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestRecommendationAssignmentsPartitionByType(t *testing.T) {
	e := RecommendationExperiment{ExperimentID: "exp_one", Name: "推荐实验", BucketBy: "user_id", Assignments: []ExperimentAssignment{{StrategyID: "a", BucketStart: 1, BucketEnd: 29}, {StrategyID: "b", BucketStart: 1, BucketEnd: 29}}}
	strategies := map[string]RecommendationStrategy{
		"a": {StrategyID: "a", RecommendationType: RecommendationPersonalized},
		"b": {StrategyID: "b", RecommendationType: RecommendationRelated},
	}
	if err := ValidateRecommendationAssignments(e, strategies); err != nil {
		t.Fatalf("different interfaces must be able to share buckets: %v", err)
	}
	related := strategies["b"]
	related.RecommendationType = RecommendationPersonalized
	strategies["b"] = related
	if err := ValidateRecommendationAssignments(e, strategies); err == nil {
		t.Fatal("overlap within the same interface was accepted")
	}
}
