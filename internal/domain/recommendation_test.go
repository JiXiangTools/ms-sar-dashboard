package domain

import "testing"

func TestValidateRecallSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*RecallSource)
		valid  bool
	}{
		{"item", func(*RecallSource) {}, true},
		{"wildcard", func(s *RecallSource) { s.KeyTemplate = "icf_{appid}_*{itemid}" }, false},
		{"tenant missing", func(s *RecallSource) { s.KeyTemplate = "icf_{itemid}" }, false},
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
			if err := ValidateRecommendationExperiment(e); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
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
