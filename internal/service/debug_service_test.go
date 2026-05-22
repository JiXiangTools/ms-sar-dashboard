package service

import "testing"

func TestBuildRecommendDebugRequestUsesOnlineEndpoints(t *testing.T) {
	endpoint, params, query, err := buildRecommendDebugRequest(RecDebugRequest{
		Type:    "hot",
		AppID:   "100001",
		Period:  "week",
		Size:    10,
		Exclude: []string{"C1", " C2 "},
	}, 10)
	if err != nil {
		t.Fatalf("build hot request: %v", err)
	}
	if endpoint != "/api/v1/msrec/recommend/hot" {
		t.Fatalf("unexpected hot endpoint: %s", endpoint)
	}
	if params["period"] != "week" || query.Get("period") != "week" {
		t.Fatalf("hot request must include selected period")
	}
	if query.Get("exclude") != "C1,C2" {
		t.Fatalf("unexpected exclude query: %s", query.Get("exclude"))
	}

	endpoint, params, query, err = buildRecommendDebugRequest(RecDebugRequest{
		Type:   "related",
		AppID:  "100001",
		ItemID: "I100",
		Period: "day",
		Size:   20,
	}, 20)
	if err != nil {
		t.Fatalf("build related request: %v", err)
	}
	if endpoint != "/api/v1/msrec/recommend/related" {
		t.Fatalf("unexpected related endpoint: %s", endpoint)
	}
	if params["item_id"] != "I100" || query.Get("item_id") != "I100" {
		t.Fatalf("related request must include item_id")
	}
	if _, ok := params["period"]; ok || query.Has("period") {
		t.Fatalf("related request must not include hot period")
	}

	endpoint, params, query, err = buildRecommendDebugRequest(RecDebugRequest{
		Type:   "personalized",
		AppID:  "100001",
		UserID: "U100",
		Size:   20,
	}, 20)
	if err != nil {
		t.Fatalf("build personalized request: %v", err)
	}
	if endpoint != "/api/v1/msrec/recommend/personalized" {
		t.Fatalf("unexpected personalized endpoint: %s", endpoint)
	}
	if params["user_id"] != "U100" || query.Get("user_id") != "U100" {
		t.Fatalf("personalized request must include user_id")
	}
}

func TestBuildRecommendDebugRequestRejectsLegacyKeyType(t *testing.T) {
	if _, _, _, err := buildRecommendDebugRequest(RecDebugRequest{Type: "key", AppID: "100001"}, 20); err == nil {
		t.Fatal("expected key debug type to be rejected")
	}
}
