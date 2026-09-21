package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const (
	datacenterPlanTestPath        = "/v1/resource/data-centers/"
	datacenterPlanTestDataSource  = "data.gpcn_datacenters.test"
	datacenterPlanTestCountryName = "United States"
	datacenterPlanTestRegionAlpha = "alpharegion"
	datacenterPlanTestRegionBeta  = "betaregion"
)

// datacenterPlanTestRecorder keeps every request the data source makes. The
// suggestion path once called routes that the API does not serve, so a test
// asserts on the recorded paths.
type datacenterPlanTestRecorder struct {
	mu      sync.Mutex
	paths   []string
	queries []string
}

func (rec *datacenterPlanTestRecorder) record(r *http.Request) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.paths = append(rec.paths, r.URL.Path)
	rec.queries = append(rec.queries, r.URL.RawQuery)
}

func (rec *datacenterPlanTestRecorder) snapshot() (paths []string, queries []string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.paths...), append([]string(nil), rec.queries...)
}

func datacenterPlanTestRow(id, name, regionName string, gpuEnabled bool) map[string]any {
	return map[string]any{
		"id":                  id,
		"name":                name,
		"code":                strings.ToLower(id),
		"regionId":            1,
		"regionName":          regionName,
		"countryId":           "11111111-1111-1111-1111-111111111111",
		"countryName":         datacenterPlanTestCountryName,
		"countryAbbreviation": "US",
		"continentCode":       "NA",
		"continentName":       "North America",
		"gpuEnabled":          gpuEnabled,
		"vpcCapable":          true,
		"customImages":        true,
		"l2Capable":           true,
	}
}

func datacenterPlanTestBody(rows []map[string]any, page, totalPages int) map[string]any {
	return map[string]any{
		"success": true,
		"message": "Data centers retrieved successfully",
		"data":    rows,
		"meta": map[string]any{
			"total":           len(rows) * totalPages,
			"page":            page,
			"pageSize":        100,
			"totalPages":      totalPages,
			"hasNextPage":     page < totalPages,
			"hasPreviousPage": page > 1,
		},
	}
}

// startDatacenterPlanMockServer serves the list route only. Every other path
// reaches the default branch, which records the miss and answers 404.
func startDatacenterPlanMockServer(t *testing.T, list func(r *http.Request) map[string]any) (*httptest.Server, *datacenterPlanTestRecorder) {
	t.Helper()

	rec := &datacenterPlanTestRecorder{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		if r.Method == http.MethodGet && r.URL.Path == datacenterPlanTestPath {
			testutil.WriteJSONResponse(w, list(r))
			return
		}
		testutil.LogUnexpectedRequest(t, w, r)
	}))
	t.Cleanup(server.Close)

	return server, rec
}

func datacenterPlanTestConfig(host, filters string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

data "gpcn_datacenters" "test" {
%s}
`, host, filters)
}

func datacenterPlanTestAssertOnlyListPath(t *testing.T, rec *datacenterPlanTestRecorder) {
	t.Helper()

	paths, _ := rec.snapshot()
	if len(paths) == 0 {
		t.Fatal("the data source made no request")
	}
	for _, path := range paths {
		if path != datacenterPlanTestPath {
			t.Errorf("unexpected request path %q, want %q", path, datacenterPlanTestPath)
		}
	}
}

func TestDatacentersDataSourceSuggestsFromUnfilteredList(t *testing.T) {
	t.Parallel()

	server, rec := startDatacenterPlanMockServer(t, func(r *http.Request) map[string]any {
		if r.URL.Query().Get("countryName") != "" {
			return datacenterPlanTestBody([]map[string]any{}, 1, 1)
		}
		return datacenterPlanTestBody([]map[string]any{
			datacenterPlanTestRow("dc-1", "Chicago", datacenterPlanTestRegionAlpha, true),
			datacenterPlanTestRow("dc-2", "Dallas", datacenterPlanTestRegionBeta, true),
		}, 1, 1)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      datacenterPlanTestConfig(server.URL, "  country_name = \"Atlantis\"\n"),
				ExpectError: regexp.MustCompile("(?s)" + datacenterPlanTestRegionAlpha + ".*" + datacenterPlanTestRegionBeta),
			},
		},
	})

	datacenterPlanTestAssertOnlyListPath(t, rec)
}

func TestDatacentersDataSourceFiltersGpuEnabledServerSide(t *testing.T) {
	t.Parallel()

	// The row contradicts the filter. A client-side filter would drop it, so the
	// row proves the provider trusts the server.
	server, rec := startDatacenterPlanMockServer(t, func(_ *http.Request) map[string]any {
		return datacenterPlanTestBody([]map[string]any{
			datacenterPlanTestRow("dc-1", "Chicago", datacenterPlanTestRegionAlpha, false),
		}, 1, 1)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: datacenterPlanTestConfig(server.URL, "  gpu_enabled = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.#", "1"),
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.0.id", "dc-1"),
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.0.gpu_enabled", "false"),
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.0.code", "dc-1"),
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.0.continent_code", "NA"),
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.0.continent_name", "North America"),
				),
			},
		},
	})

	datacenterPlanTestAssertOnlyListPath(t, rec)

	_, queries := rec.snapshot()
	for _, query := range queries {
		if !strings.Contains(query, "gpuEnabled=true") {
			t.Errorf("query %q does not carry gpuEnabled=true", query)
		}
	}
}

func TestDatacentersDataSourcePagesToTotal(t *testing.T) {
	t.Parallel()

	server, rec := startDatacenterPlanMockServer(t, func(r *http.Request) map[string]any {
		if r.URL.Query().Get("page") == "2" {
			return datacenterPlanTestBody([]map[string]any{
				datacenterPlanTestRow("dc-2", "Dallas", datacenterPlanTestRegionBeta, true),
			}, 2, 2)
		}
		return datacenterPlanTestBody([]map[string]any{
			datacenterPlanTestRow("dc-1", "Chicago", datacenterPlanTestRegionAlpha, true),
		}, 1, 2)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: datacenterPlanTestConfig(server.URL, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.#", "2"),
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.0.id", "dc-1"),
					resource.TestCheckResourceAttr(datacenterPlanTestDataSource, "datacenters.1.id", "dc-2"),
				),
			},
		},
	})

	datacenterPlanTestAssertOnlyListPath(t, rec)
}

func TestDatacentersDataSourceNamesGpuEnabledWhenNoRowMatches(t *testing.T) {
	t.Parallel()

	server, rec := startDatacenterPlanMockServer(t, func(r *http.Request) map[string]any {
		if r.URL.Query().Get("gpuEnabled") != "" {
			return datacenterPlanTestBody([]map[string]any{}, 1, 1)
		}
		return datacenterPlanTestBody([]map[string]any{
			datacenterPlanTestRow("dc-1", "Chicago", datacenterPlanTestRegionAlpha, false),
		}, 1, 1)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      datacenterPlanTestConfig(server.URL, "  gpu_enabled = true\n"),
				ExpectError: regexp.MustCompile("(?s)specified filters have.*gpu_enabled = true.*none matched that value"),
			},
		},
	})

	datacenterPlanTestAssertOnlyListPath(t, rec)
}

func TestDatacentersDataSourceSuggestsWhenAnotherFilterMatchesNone(t *testing.T) {
	t.Parallel()

	server, rec := startDatacenterPlanMockServer(t, func(r *http.Request) map[string]any {
		if r.URL.Query().Get("countryName") != "" {
			return datacenterPlanTestBody([]map[string]any{}, 1, 1)
		}
		return datacenterPlanTestBody([]map[string]any{
			datacenterPlanTestRow("dc-1", "Chicago", datacenterPlanTestRegionAlpha, true),
		}, 1, 1)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      datacenterPlanTestConfig(server.URL, "  country_name = \"Atlantis\"\n  gpu_enabled  = true\n"),
				ExpectError: regexp.MustCompile("(?s)Some possible.*values are.*" + datacenterPlanTestRegionAlpha),
			},
		},
	})

	datacenterPlanTestAssertOnlyListPath(t, rec)
}

func TestDatacentersDataSourceReportsNoVisibleDatacenters(t *testing.T) {
	t.Parallel()

	server, rec := startDatacenterPlanMockServer(t, func(_ *http.Request) map[string]any {
		return datacenterPlanTestBody([]map[string]any{}, 1, 1)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      datacenterPlanTestConfig(server.URL, "  country_name = \"Atlantis\"\n"),
				ExpectError: regexp.MustCompile("(?s)this API key.*can see no datacenters at all"),
			},
		},
	})

	datacenterPlanTestAssertOnlyListPath(t, rec)
}

func TestDatacentersDataSourceSuggestsEachPairOnce(t *testing.T) {
	t.Parallel()

	server, rec := startDatacenterPlanMockServer(t, func(r *http.Request) map[string]any {
		if r.URL.Query().Get("countryName") != "" {
			return datacenterPlanTestBody([]map[string]any{}, 1, 1)
		}
		return datacenterPlanTestBody([]map[string]any{
			datacenterPlanTestRow("dc-1", "Chicago", datacenterPlanTestRegionAlpha, true),
			datacenterPlanTestRow("dc-2", "Dallas", datacenterPlanTestRegionBeta, true),
			datacenterPlanTestRow("dc-3", "Aurora", datacenterPlanTestRegionAlpha, true),
		}, 1, 1)
	})

	// ExpectError proves a match, never the absence of one, and the helper skips
	// ErrorCheck for a step that sets it. ErrorCheck alone counts the pairs.
	var checked bool
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		ErrorCheck: func(err error) error {
			checked = true
			repeated := strings.Count(err.Error(), datacenterPlanTestRegionAlpha)
			once := strings.Count(err.Error(), datacenterPlanTestRegionBeta)
			if once < 1 || repeated != once {
				t.Errorf("the suggestion names %q %d times and %q %d times, want the same count",
					datacenterPlanTestRegionAlpha, repeated, datacenterPlanTestRegionBeta, once)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: datacenterPlanTestConfig(server.URL, "  country_name = \"Atlantis\"\n")},
		},
	})

	if !checked {
		t.Fatal("the data source returned no error, so the suggestion went unchecked")
	}

	datacenterPlanTestAssertOnlyListPath(t, rec)
}
