package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeepSeekProviderName(t *testing.T) {
	p := &DeepSeekProvider{}
	if p.Name() != "deepseek" {
		t.Errorf("Name() = %q, want %q", p.Name(), "deepseek")
	}
}

func TestDeepSeekProviderFetchUsage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/balance" {
			t.Errorf("path = %q, want /user/balance", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"is_available": true,
			"balance_infos": [
				{"currency": "CNY", "total_balance": "31.65", "granted_balance": "0.00", "topped_up_balance": "31.65"}
			]
		}`))
	}))
	defer ts.Close()

	p := &DeepSeekProvider{}
	data, err := p.FetchUsage(context.Background(), ts.URL, "test-key")
	if err != nil {
		t.Fatalf("FetchUsage error: %v", err)
	}

	if data.Provider != "deepseek" {
		t.Errorf("Provider = %q, want deepseek", data.Provider)
	}
	if data.Summary != "¥31.65" {
		t.Errorf("Summary = %q, want ¥31.65", data.Summary)
	}
	if len(data.Details) != 1 {
		t.Fatalf("Details count = %d, want 1", len(data.Details))
	}
	if data.Details[0].Balance != "31.65" {
		t.Errorf("Balance = %q, want 31.65", data.Details[0].Balance)
	}
	if data.Details[0].Currency != "CNY" {
		t.Errorf("Currency = %q, want CNY", data.Details[0].Currency)
	}
	if data.Details[0].IsAvailable == nil || !*data.Details[0].IsAvailable {
		t.Error("IsAvailable should be true")
	}
}

func TestDeepSeekProviderNotAvailable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_available": false, "balance_infos": []}`))
	}))
	defer ts.Close()

	p := &DeepSeekProvider{}
	data, err := p.FetchUsage(context.Background(), ts.URL, "test-key")
	if err != nil {
		t.Fatalf("FetchUsage error: %v", err)
	}
	if data.Details[0].IsAvailable == nil || *data.Details[0].IsAvailable {
		t.Error("IsAvailable should be false")
	}
	if data.Summary != "unavailable" {
		t.Errorf("Summary = %q, want unavailable", data.Summary)
	}
}
