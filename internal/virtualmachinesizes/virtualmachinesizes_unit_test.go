package virtualmachinesizes

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/testutil"
)

func testSizes() []FlatSize {
	return []FlatSize{
		{ID: "sku-gen-micro", Name: "G-Micro-1", SkuCode: "vm-gen-micro-1", Category: "general-purpose", CPU: 2, MemoryGB: 4, StorageGB: 50},
		{ID: "sku-gen-small", Name: "G-Small-1", SkuCode: "vm-gen-small-1", Category: "general-purpose", CPU: 4, MemoryGB: 8, StorageGB: 100},
		{ID: "sku-gen-medium", Name: "G-Medium-1", SkuCode: "vm-gen-medium-1", Category: "general-purpose", CPU: 8, MemoryGB: 16, StorageGB: 200},
		{ID: "sku-mem-micro", Name: "M-Micro-1", SkuCode: "vm-mem-micro-1", Category: "memory-optimized", CPU: 2, MemoryGB: 8, StorageGB: 50},
		{ID: "sku-mem-small", Name: "M-Small-1", SkuCode: "vm-mem-small-1", Category: "memory-optimized", CPU: 4, MemoryGB: 16, StorageGB: 100},
	}
}

func TestFilterSizes_NoFilters(t *testing.T) {
	sizes := testSizes()
	result := FilterSizes(context.Background(), sizes, "", 0, 0, 0)
	if len(result) != len(sizes) {
		t.Errorf("expected %d sizes, got %d", len(sizes), len(result))
	}
}

func TestFilterSizes_ByCategory(t *testing.T) {
	result := FilterSizes(context.Background(), testSizes(), "general-purpose", 0, 0, 0)
	if len(result) != 3 {
		t.Errorf("expected 3 general-purpose sizes, got %d", len(result))
	}
	for _, s := range result {
		if s.Category != "general-purpose" {
			t.Errorf("expected category general-purpose, got %s", s.Category)
		}
	}
}

func TestFilterSizes_ByMinCPU(t *testing.T) {
	result := FilterSizes(context.Background(), testSizes(), "", 4, 0, 0)
	if len(result) != 3 {
		t.Errorf("expected 3 sizes with min 4 CPU, got %d", len(result))
	}
	for _, s := range result {
		if s.CPU < 4 {
			t.Errorf("expected CPU >= 4, got %d for %s", s.CPU, s.Name)
		}
	}
}

func TestFilterSizes_ByMinMemory(t *testing.T) {
	result := FilterSizes(context.Background(), testSizes(), "", 0, 8, 0)
	if len(result) != 4 {
		t.Errorf("expected 4 sizes with min 8 GB RAM, got %d", len(result))
	}
}

func TestFilterSizes_ByMinStorage(t *testing.T) {
	result := FilterSizes(context.Background(), testSizes(), "", 0, 0, 100)
	if len(result) != 3 {
		t.Errorf("expected 3 sizes with min 100 GB storage, got %d", len(result))
	}
}

func TestFilterSizes_Combined(t *testing.T) {
	result := FilterSizes(context.Background(), testSizes(), "memory-optimized", 4, 0, 0)
	if len(result) != 1 {
		t.Errorf("expected 1 size, got %d", len(result))
	}
	if result[0].ID != "sku-mem-small" {
		t.Errorf("expected sku-mem-small, got %s", result[0].ID)
	}
}

func TestFilterSizes_EmptyResult(t *testing.T) {
	result := FilterSizes(context.Background(), testSizes(), "general-purpose", 100, 0, 0)
	if len(result) != 0 {
		t.Errorf("expected 0 sizes, got %d", len(result))
	}
}

func TestFetchSizes_MockHTTP(t *testing.T) {
	const datacenterId = "dc-123"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machine-sizes") {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "sizes retrieved",
					"data": map[string]any{
						"datacenterId": datacenterId,
						"categories": []any{
							map[string]any{
								"code": "general-purpose",
								"name": "General Purpose",
								"sizes": []any{
									map[string]any{"skuId": "sku-1", "name": "G-Micro-1", "skuCode": "vm-gen-micro-1", "cpu": 2, "ram": 4, "disk": 50},
								},
							},
						},
					},
					"meta": nil,
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	sizes, err := FetchSizes(gpcnClient, context.Background(), datacenterId, "")
	if err != nil {
		t.Fatalf("FetchSizes failed: %v", err)
	}
	if len(sizes) != 1 {
		t.Fatalf("expected 1 size, got %d", len(sizes))
	}
	if sizes[0].ID != "sku-1" {
		t.Errorf("expected ID sku-1, got %s", sizes[0].ID)
	}
	if sizes[0].Category != "general-purpose" {
		t.Errorf("expected category general-purpose, got %s", sizes[0].Category)
	}
}

func TestFetchSizes_WithVmId(t *testing.T) {
	const datacenterId = "dc-456"
	const vmId = "vm-789"
	var receivedVmId string

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machine-sizes") {
				receivedVmId = r.URL.Query().Get("vmId")
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "sizes retrieved",
					"data": map[string]any{
						"datacenterId": datacenterId,
						"categories":   []any{},
					},
					"meta": nil,
				})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	_, err := FetchSizes(gpcnClient, context.Background(), datacenterId, vmId)
	if err != nil {
		t.Fatalf("FetchSizes failed: %v", err)
	}
	if receivedVmId != vmId {
		t.Errorf("expected vmId query param %q, got %q", vmId, receivedVmId)
	}
}
