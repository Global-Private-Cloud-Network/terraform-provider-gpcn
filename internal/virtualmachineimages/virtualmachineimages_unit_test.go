package virtualmachineimages

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/testutil"
)

func nestedImagesResponse() map[string]any {
	return map[string]any{
		"success": true,
		"message": "Virtual machine images retrieved successfully",
		"data": []any{
			map[string]any{
				"id":        1,
				"name":      "Linux",
				"sortOrder": 1,
				"images": []any{
					map[string]any{"id": "eb7da49d-cc71-480a-968d-fbf2841bedf7", "name": "Alma Linux 8.x"},
					map[string]any{"id": "894529cd-45cf-4b36-b5fe-2634d54ad7c9", "name": "Alma Linux 9.x"},
					map[string]any{"id": "b79f0102-ff16-43ad-a24f-e59ad1aab998", "name": "Ubuntu 24.04 LTS"},
				},
			},
			map[string]any{
				"id":        2,
				"name":      "Windows",
				"sortOrder": 2,
				"images": []any{
					map[string]any{"id": "fec02d29-4f30-4aa1-ac74-805a7cd01e28", "name": "Windows 2022 Standard"},
				},
			},
			map[string]any{
				"id":        4,
				"name":      "Custom",
				"sortOrder": 4,
				"images": []any{
					map[string]any{"id": "121219fe-3d5e-43e5-8f81-e42ddba4322e", "name": "Fedora 43"},
				},
			},
		},
	}
}

func TestFetchImages(t *testing.T) {
	const datacenterID = "dc-test-123"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/virtual-machine-images") {
				testutil.WriteJSONResponse(w, nestedImagesResponse())
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	images, err := FetchImages(gpcnClient, context.Background(), datacenterID)
	if err != nil {
		t.Fatalf("FetchImages failed: %v", err)
	}
	if len(images) != 5 {
		t.Errorf("Expected 5 flattened images, got %d", len(images))
	}
	// Verify ImageType is populated from the category
	for _, img := range images {
		if img.ImageType == "" {
			t.Errorf("Expected ImageType to be set for image %q", img.Name)
		}
	}
}

func TestFilterImagesNoFilter(t *testing.T) {
	images := []FlatImage{
		{ID: "1", Name: "Alma Linux 8.x", ImageType: "Linux"},
		{ID: "2", Name: "Windows 2022 Standard", ImageType: "Windows"},
		{ID: "3", Name: "Fedora 43", ImageType: "Custom"},
	}

	result := FilterImages(context.Background(), images, "", "")
	if len(result) != 3 {
		t.Errorf("Expected 3 images with no filter, got %d", len(result))
	}
}

func TestFilterImagesByType(t *testing.T) {
	images := []FlatImage{
		{ID: "1", Name: "Alma Linux 8.x", ImageType: "Linux"},
		{ID: "2", Name: "Ubuntu 24.04 LTS", ImageType: "Linux"},
		{ID: "3", Name: "Windows 2022 Standard", ImageType: "Windows"},
	}

	result := FilterImages(context.Background(), images, "Linux", "")
	if len(result) != 2 {
		t.Errorf("Expected 2 Linux images, got %d", len(result))
	}
	for _, img := range result {
		if img.ImageType != "Linux" {
			t.Errorf("Expected ImageType 'Linux', got %q", img.ImageType)
		}
	}
}

func TestFilterImagesByTypeExactMatch(t *testing.T) {
	images := []FlatImage{
		{ID: "1", Name: "Alma Linux 8.x", ImageType: "Linux"},
		{ID: "2", Name: "Windows 2022 Standard", ImageType: "Windows"},
	}

	// "linux" (lowercase) should NOT match "Linux" (exact match required)
	result := FilterImages(context.Background(), images, "linux", "")
	if len(result) != 0 {
		t.Errorf("Expected 0 results for case-sensitive image_type filter, got %d", len(result))
	}
}

func TestFilterImagesByName(t *testing.T) {
	images := []FlatImage{
		{ID: "1", Name: "Alma Linux 8.x", ImageType: "Linux"},
		{ID: "2", Name: "Alma Linux 9.x", ImageType: "Linux"},
		{ID: "3", Name: "Ubuntu 24.04 LTS", ImageType: "Linux"},
		{ID: "4", Name: "Windows 2022 Standard", ImageType: "Windows"},
	}

	result := FilterImages(context.Background(), images, "", "alma linux")
	if len(result) != 2 {
		t.Errorf("Expected 2 Alma Linux images, got %d", len(result))
	}
}

func TestFilterImagesByNameCaseInsensitive(t *testing.T) {
	images := []FlatImage{
		{ID: "1", Name: "Alma Linux 8.x", ImageType: "Linux"},
		{ID: "2", Name: "Ubuntu 24.04 LTS", ImageType: "Linux"},
	}

	result := FilterImages(context.Background(), images, "", "ALMA")
	if len(result) != 1 {
		t.Errorf("Expected 1 result for case-insensitive name filter, got %d", len(result))
	}
	if result[0].Name != "Alma Linux 8.x" {
		t.Errorf("Expected 'Alma Linux 8.x', got %q", result[0].Name)
	}
}

func TestFilterImagesByTypeAndName(t *testing.T) {
	images := []FlatImage{
		{ID: "1", Name: "Alma Linux 8.x", ImageType: "Linux"},
		{ID: "2", Name: "Alma Linux 9.x", ImageType: "Linux"},
		{ID: "3", Name: "Windows 2022 Standard", ImageType: "Windows"},
		{ID: "4", Name: "Fedora 43", ImageType: "Custom"},
	}

	result := FilterImages(context.Background(), images, "Linux", "8")
	if len(result) != 1 {
		t.Errorf("Expected 1 result, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("Expected image ID '1', got %q", result[0].ID)
	}
}

func TestFilterImagesNoMatches(t *testing.T) {
	images := []FlatImage{
		{ID: "1", Name: "Alma Linux 8.x", ImageType: "Linux"},
	}

	result := FilterImages(context.Background(), images, "", "NonExistentOS 99.x")
	if len(result) != 0 {
		t.Errorf("Expected 0 results for non-matching filter, got %d", len(result))
	}
}
