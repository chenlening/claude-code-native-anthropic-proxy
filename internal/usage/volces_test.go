package usage

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestVolcesGetCodingPlanUsage(t *testing.T) {
	ak := os.Getenv("VOLCES_AK")
	sk := os.Getenv("VOLCES_SK")
	if ak == "" || sk == "" {
		t.Skip("VOLCES_AK/VOLCES_SK not set")
	}

	p := &VolcesProvider{
		AK:     ak,
		SK:     sk,
		Region: "cn-beijing",
	}

	data, err := p.FetchUsage(context.Background(), "", "")
	if err != nil {
		t.Logf("FetchUsage error: %v", err)
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	t.Logf("FetchUsage result:\n%s", string(b))
}
