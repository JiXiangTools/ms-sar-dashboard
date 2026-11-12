package domain

import (
	"encoding/json"
	"testing"
)

func TestNewestIsConfiguredAsAnOrdinaryRecall(t *testing.T) {
	sources := map[string]RecallSource{"newest": {Kind: "global", Producer: "hot", HotPeriod: "newest"}}
	for _, kind := range []string{RecommendationPersonalized, RecommendationRelated} {
		t.Run(kind, func(t *testing.T) {
			s := RecommendationStrategy{StrategyID: "a", Name: "策略", RecommendationType: kind, Rank: "recall_weight", Recall: []StrategyRecall{{SourceID: "newest", Weight: 2, RecallRate: .25}}}
			if err := ValidateRecommendationStrategy(s, sources); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(s)
			var fields map[string]json.RawMessage
			if err != nil || json.Unmarshal(raw, &fields) != nil {
				t.Fatal("could not encode strategy")
			}
			if _, ok := fields["newest"]; ok {
				t.Fatal("new strategies must not emit a separate newest stage")
			}
			s.Newest = StrategyNewest{SourceID: "newest", SharePercent: 25, Scenes: []string{"discover"}}
			if err := ValidateRecommendationStrategy(s, sources); err == nil {
				t.Fatal("new strategies must reject a separate newest stage")
			}
		})
	}
}

func TestHistoricalNewestStageRemainsReadable(t *testing.T) {
	s := RecommendationStrategy{StrategyID: "a", Name: "历史策略", Rank: "recall_weight", Recall: []StrategyRecall{{SourceID: "icf", Weight: 3, RecallRate: 1}}, Newest: StrategyNewest{SourceID: "latest", SharePercent: 10, Scenes: []string{"discover"}}}
	sources := map[string]RecallSource{"icf": {Kind: "item"}, "latest": {Kind: "global"}}
	if err := ValidatePublishedRecommendationStrategy(s, sources); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRecommendationStrategy(s, sources); err == nil {
		t.Fatal("historical newest stages cannot be saved as new strategy definitions")
	}
}

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
			s := RecallSource{SourceID: "icf", Name: "Item CF", Kind: "item", Producer: "icf", KeyTemplate: "icf_{appid}_{itemid}", ValueFormat: "compact_score"}
			tc.change(&s)
			if err := ValidateRecallSource(s); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestControlsPinOneBaselineForEachParticipatingType(t *testing.T) {
	strategies := map[string]RecommendationStrategy{
		"a":     {StrategyID: "a", Version: 1, RecommendationType: RecommendationPersonalized},
		"base":  {StrategyID: "base", Version: 3, RecommendationType: RecommendationPersonalized},
		"other": {StrategyID: "other", Version: 1, RecommendationType: RecommendationRelated},
	}
	e := RecommendationExperiment{Assignments: []ExperimentAssignment{{StrategyID: "a", StrategyVersion: 1}}}
	for _, tc := range []struct {
		name     string
		controls []ExperimentControl
		valid    bool
	}{
		{"missing", nil, false},
		{"pinned", []ExperimentControl{{StrategyID: "base", StrategyVersion: 3}}, true},
		{"wrong version", []ExperimentControl{{StrategyID: "base", StrategyVersion: 2}}, false},
		{"wrong type", []ExperimentControl{{StrategyID: "other", StrategyVersion: 1}}, false},
		{"duplicate type", []ExperimentControl{{StrategyID: "base", StrategyVersion: 3}, {StrategyID: "base", StrategyVersion: 3}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.Controls = tc.controls
			if err := ValidateRecommendationControls(e, strategies); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got %v", tc.valid, err)
			}
		})
	}
}

func TestValidateRecommendationStrategy(t *testing.T) {
	sources := map[string]RecallSource{"icf": {Kind: "item"}, "day": {Kind: "global"}}
	for _, tc := range []struct {
		name   string
		change func(*RecommendationStrategy)
		valid  bool
	}{
		{"valid", func(*RecommendationStrategy) {}, true},
		{"reserved", func(s *RecommendationStrategy) { s.StrategyID = "default" }, false},
		{"too long", func(s *RecommendationStrategy) { s.StrategyID = "abcdefghijk" }, false},
		{"uppercase", func(s *RecommendationStrategy) { s.StrategyID = "A" }, false},
		{"unknown source", func(s *RecommendationStrategy) { s.Recall[0].SourceID = "missing" }, false},
		{"weight", func(s *RecommendationStrategy) { s.Recall[0].Weight = 0 }, false},
		{"recall rate", func(s *RecommendationStrategy) { s.Recall[0].RecallRate = 1.1 }, false},
		{"item fallback", func(s *RecommendationStrategy) { s.FallbackSourceID = "icf" }, false},
		{"global fallback", func(s *RecommendationStrategy) { s.FallbackSourceID = "day" }, true},
		{"unsupported rank", func(s *RecommendationStrategy) { s.Rank = "model" }, false},
		{"explicit personalized", func(s *RecommendationStrategy) { s.RecommendationType = RecommendationPersonalized }, true},
		{"related", func(s *RecommendationStrategy) { s.RecommendationType = RecommendationRelated }, true},
		{"hot is not a strategy type", func(s *RecommendationStrategy) { s.RecommendationType = "hot" }, false},
		{"unknown type", func(s *RecommendationStrategy) { s.RecommendationType = "other" }, false},
		{"related fallback", func(s *RecommendationStrategy) {
			s.RecommendationType = RecommendationRelated
			s.FallbackSourceID = "day"
		}, false},
		{"related newest source", func(s *RecommendationStrategy) {
			s.RecommendationType = RecommendationRelated
			s.Newest.SourceID = "day"
		}, false},
		{"related newest share", func(s *RecommendationStrategy) {
			s.RecommendationType = RecommendationRelated
			s.Newest.SharePercent = 1
		}, false},
		{"related newest scenes", func(s *RecommendationStrategy) {
			s.RecommendationType = RecommendationRelated
			s.Newest.Scenes = []string{"discover"}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := RecommendationStrategy{StrategyID: "a", Name: "策略 A", Rank: "recall_weight", Recall: []StrategyRecall{{SourceID: "icf", Weight: 1, RecallRate: 1}}}
			tc.change(&s)
			if err := ValidateRecommendationStrategy(s, sources); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
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
		{"hundred percent arm", []ExperimentAssignment{{StrategyID: "a", BucketStart: 0, BucketEnd: 99}}, false},
		{"duplicate strategy", []ExperimentAssignment{{StrategyID: "a", BucketStart: 0, BucketEnd: 9}, {StrategyID: "a", BucketStart: 10, BucketEnd: 19}}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := RecommendationExperiment{ExperimentID: "exp_one", Name: "首页实验", BucketBy: "user_id", BucketAlgorithm: "sha256", Assignments: tc.assignments}
			strategies := map[string]RecommendationStrategy{}
			for _, assignment := range e.Assignments {
				strategies[assignment.StrategyID] = RecommendationStrategy{StrategyID: assignment.StrategyID}
			}
			if err := ValidateRecommendationAssignments(e, strategies); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestRecommendationAssignmentsPartitionByType(t *testing.T) {
	e := RecommendationExperiment{ExperimentID: "exp_one", Name: "推荐实验", BucketBy: "user_id", BucketAlgorithm: "sha256", Assignments: []ExperimentAssignment{{StrategyID: "a", StrategyVersion: 1, BucketStart: 1, BucketEnd: 29}, {StrategyID: "b", StrategyVersion: 1, BucketStart: 1, BucketEnd: 29}}}
	strategies := map[string]RecommendationStrategy{
		"a": {StrategyID: "a", Version: 1, RecommendationType: RecommendationPersonalized},
		"b": {StrategyID: "b", Version: 1, RecommendationType: RecommendationRelated},
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
	related.RecommendationType = RecommendationRelated
	related.Version = 2
	strategies["b"] = related
	if err := ValidateRecommendationAssignments(e, strategies); err == nil {
		t.Fatal("a newer definition cannot substitute for the pinned strategy version")
	}
}

func TestRecommendationExperimentRequiresStableSHA256(t *testing.T) {
	for _, algorithm := range []string{"", "crc32", "mod100"} {
		e := RecommendationExperiment{ExperimentID: "exp_one", Name: "首页实验", BucketBy: "user_id", BucketAlgorithm: algorithm, Assignments: []ExperimentAssignment{{StrategyID: "a", BucketStart: 1, BucketEnd: 29}}}
		if err := ValidateRecommendationExperiment(e); err == nil {
			t.Fatalf("unsupported algorithm %q was accepted", algorithm)
		}
	}
}
