package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestZhipuProviderName(t *testing.T) {
	p := &ZhipuProvider{}
	if p.Name() != "zhipu" {
		t.Errorf("Name() = %q, want %q", p.Name(), "zhipu")
	}
}

func TestZhipuProviderFetchUsage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/monitor/usage/quota/limit" {
			t.Errorf("path = %q, want /api/monitor/usage/quota/limit", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "test-api-key" {
			t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), "test-api-key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 200,
			"msg": "Operation successful",
			"data": {
				"limits": [
					{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 25, "nextResetTime": 1780124423165},
					{"type": "TOKENS_LIMIT", "unit": 6, "number": 1, "percentage": 1, "nextResetTime": 1780714432998},
					{"type": "TIME_LIMIT", "unit": 5, "number": 1, "usage": 4000, "currentValue": 469, "remaining": 3531, "percentage": 11, "nextResetTime": 1781405632992, "usageDetails": [
						{"modelCode": "search-prime", "usage": 348},
						{"modelCode": "web-reader", "usage": 121}
					]}
				],
				"level": "max"
			},
			"success": true
		}`))
	}))
	defer ts.Close()

	p := &ZhipuProvider{}
	data, err := p.FetchUsage(context.Background(), ts.URL, "test-api-key")
	if err != nil {
		t.Fatalf("FetchUsage error: %v", err)
	}

	if data.Provider != "zhipu" {
		t.Errorf("Provider = %q, want zhipu", data.Provider)
	}
	if data.PlanLevel != "max" {
		t.Errorf("PlanLevel = %q, want max", data.PlanLevel)
	}
	if len(data.Details) != 3 {
		t.Fatalf("Details count = %d, want 3", len(data.Details))
	}

	if data.Details[0].Type != "TOKENS_LIMIT" {
		t.Errorf("Details[0].Type = %q, want TOKENS_LIMIT", data.Details[0].Type)
	}
	if data.Details[0].Window != "5h" {
		t.Errorf("Details[0].Window = %q, want 5h", data.Details[0].Window)
	}
	if data.Details[0].Percentage != 25 {
		t.Errorf("Details[0].Percentage = %f, want 25", data.Details[0].Percentage)
	}

	if data.Details[2].Type != "TIME_LIMIT" {
		t.Errorf("Details[2].Type = %q, want TIME_LIMIT", data.Details[2].Type)
	}
	if data.Details[2].Used != 469 {
		t.Errorf("Details[2].Used = %d, want 469", data.Details[2].Used)
	}
	if data.Details[2].Total != 4000 {
		t.Errorf("Details[2].Total = %d, want 4000", data.Details[2].Total)
	}
	if len(data.Details[2].PerModel) != 2 {
		t.Fatalf("Details[2].PerModel count = %d, want 2", len(data.Details[2].PerModel))
	}
	if data.Details[2].PerModel[0].Model != "search-prime" {
		t.Errorf("PerModel[0].Model = %q, want search-prime", data.Details[2].PerModel[0].Model)
	}

	if data.Summary == "" {
		t.Error("Summary should not be empty")
	}
	if data.FetchedAt.IsZero() {
		t.Error("FetchedAt should not be zero")
	}
}

func TestZhipuProviderAuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":1000,"msg":"Authentication Failed","success":false}`))
	}))
	defer ts.Close()

	p := &ZhipuProvider{}
	data, err := p.FetchUsage(context.Background(), ts.URL, "bad-key")
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	if data == nil {
		t.Fatal("expected non-nil UsageData even on error")
	}
	if data.Error == "" {
		t.Error("expected error message in UsageData.Error")
	}
}
