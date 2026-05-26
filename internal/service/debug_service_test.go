package service

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/JiXiangTools/ms-sar-dashboard/internal/config"
	"github.com/JiXiangTools/ms-sar-dashboard/internal/platform/elasticsearch"
)

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

func TestParseESRawConsoleInput(t *testing.T) {
	req, err := ParseESRawConsoleInput("GET /user/xxx\n\n{\"query\":{\"match_all\":{}}}")
	if err != nil {
		t.Fatalf("parse raw input: %v", err)
	}
	if req.Method != "GET" || req.Path != "/user/xxx" || req.Body != `{"query":{"match_all":{}}}` {
		t.Fatalf("unexpected request: %#v", req)
	}

	if _, err := ParseESRawConsoleInput("GET /user/xxx extra\n{}"); err == nil {
		t.Fatal("expected malformed first line to fail")
	}
}

func TestNormalizeESRawRequestAllowsOnlyReadJSONRequests(t *testing.T) {
	method, path, body, err := normalizeESRawRequest(ESRawRequest{
		Method: "get",
		Path:   "ms_search_product_100001_v1/_search?pretty=true",
		Body:   `{"size":1}`,
	})
	if err != nil {
		t.Fatalf("normalize raw request: %v", err)
	}
	if method != http.MethodGet || path != "/ms_search_product_100001_v1/_search?pretty=true" || string(body) != `{"size":1}` {
		t.Fatalf("unexpected normalized request: method=%s path=%s body=%s", method, path, string(body))
	}

	for _, req := range []ESRawRequest{
		{Method: "POST", Path: "/user/xxx", Body: `{}`},
		{Method: "GET", Path: "http://elasticsearch:9200/user/xxx", Body: `{}`},
		{Method: "GET", Path: "/_bulk", Body: `{}`},
		{Method: "GET", Path: "/user/xxx", Body: `{bad}`},
	} {
		if _, _, _, err := normalizeESRawRequest(req); err == nil {
			t.Fatalf("expected request to fail: %#v", req)
		}
	}
}

func TestDebugServiceRawESReturnsElasticsearchJSON(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.RequestURI()
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"_index":"user","_id":"xxx","found":true}`))
	}))
	defer server.Close()

	es, err := elasticsearch.New(config.ElasticsearchConfig{
		Addrs:        []string{server.URL},
		DebugEnabled: true,
	})
	if err != nil {
		t.Fatalf("create es client: %v", err)
	}
	service := NewDebugService(config.Config{
		Elasticsearch: config.ElasticsearchConfig{DebugEnabled: true},
	}, nil, es, nil, log.New(io.Discard, "", 0))

	value, err := service.RawES(context.Background(), 7, ESRawRequest{
		Method: "GET",
		Path:   "/user/xxx",
		Body:   `{}`,
	})
	if err != nil {
		t.Fatalf("raw es: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/user/xxx" || gotBody != `{}` {
		t.Fatalf("unexpected upstream request method=%s path=%s body=%s", gotMethod, gotPath, gotBody)
	}
	want := map[string]any{"_index": "user", "_id": "xxx", "found": true}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("unexpected value: %#v", value)
	}
}

func TestDebugServiceRawESReturnsElasticsearchErrorJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"type":"index_not_found_exception"},"status":404}`))
	}))
	defer server.Close()

	es, err := elasticsearch.New(config.ElasticsearchConfig{
		Addrs:        []string{server.URL},
		DebugEnabled: true,
	})
	if err != nil {
		t.Fatalf("create es client: %v", err)
	}
	service := NewDebugService(config.Config{
		Elasticsearch: config.ElasticsearchConfig{DebugEnabled: true},
	}, nil, es, nil, log.New(io.Discard, "", 0))

	value, err := service.RawES(context.Background(), 7, ESRawRequest{Method: "GET", Path: "/missing", Body: `{}`})
	if err != nil {
		t.Fatalf("raw es must return ES JSON for non-2xx responses: %v", err)
	}
	encoded := formatRawTestJSON(t, value)
	if !strings.Contains(encoded, `"status":404`) || !strings.Contains(encoded, "index_not_found_exception") {
		t.Fatalf("unexpected error value: %s", encoded)
	}
}

func formatRawTestJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(raw)
}
