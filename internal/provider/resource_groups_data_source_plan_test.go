package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const rgDataSourceTestAddress = "data.gpcn_resource_groups.test"

type rgDataSourceTestRow struct {
	id, name  string
	isDefault bool
}

type rgDataSourceTestMock struct {
	url string

	mutex sync.Mutex
	pages []string
}

func (m *rgDataSourceTestMock) requestedPages() []string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return append([]string(nil), m.pages...)
}

// startRgDataSourceTestMock serves one slice of rows per page.
func startRgDataSourceTestMock(t *testing.T, pages [][]rgDataSourceTestRow) *rgDataSourceTestMock {
	t.Helper()
	mock := &rgDataSourceTestMock{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check" {
			testutil.HandleAuthCheck(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/resource-groups/" {
			testutil.LogUnexpectedRequest(t, w, r)
			return
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if r.URL.Query().Get("limit") != "100" || r.URL.Query().Has("pageSize") {
			testutil.LogUnexpectedRequest(t, w, r)
			return
		}
		if err != nil || page < 1 || page > len(pages) {
			testutil.LogUnexpectedRequest(t, w, r)
			return
		}
		mock.mutex.Lock()
		mock.pages = append(mock.pages, r.URL.Query().Get("page"))
		mock.mutex.Unlock()

		rows := make([]map[string]any, 0, len(pages[page-1]))
		for _, row := range pages[page-1] {
			rows = append(rows, map[string]any{
				"id":          row.id,
				"name":        row.name,
				"description": nil,
				"isDefault":   row.isDefault,
			})
		}
		testutil.WriteJSONResponse(w, map[string]any{
			"success": true,
			"data":    rows,
			"meta":    map[string]any{"page": page, "totalPages": len(pages)},
		})
	}))
	t.Cleanup(server.Close)
	mock.url = server.URL
	return mock
}

func rgDataSourceTestConfig(host, filters string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

data "gpcn_resource_groups" "test" {
%s}
`, host, filters)
}

var rgDataSourceTestRows = []rgDataSourceTestRow{
	{id: "rg-default", name: "Default", isDefault: true},
	{id: "rg-other", name: "other"},
}

func TestResourceGroupsDataSourcePlanListsAndFindsDefault(t *testing.T) {
	t.Parallel()
	mock := startRgDataSourceTestMock(t, [][]rgDataSourceTestRow{rgDataSourceTestRows})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: rgDataSourceTestConfig(mock.url, ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.#", "2"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.0.id", "rg-default"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.0.is_default", "true"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.1.name", "other"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.1.is_default", "false"),
				resource.TestCheckNoResourceAttr(rgDataSourceTestAddress, "resource_groups.1.description"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "default_resource_group_id", "rg-default"),
			),
		}},
	})
}

func TestResourceGroupsDataSourcePlanFiltersByName(t *testing.T) {
	t.Parallel()
	mock := startRgDataSourceTestMock(t, [][]rgDataSourceTestRow{rgDataSourceTestRows})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: rgDataSourceTestConfig(mock.url, `  name = "other"`+"\n"),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.#", "1"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.0.id", "rg-other"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "default_resource_group_id", "rg-default"),
			),
		}},
	})
}

func TestResourceGroupsDataSourcePlanNoMatchErrors(t *testing.T) {
	t.Parallel()
	mock := startRgDataSourceTestMock(t, [][]rgDataSourceTestRow{rgDataSourceTestRows})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      rgDataSourceTestConfig(mock.url, `  name = "missing"`+"\n"),
			ExpectError: regexp.MustCompile(`(?s)No Resource Group Matches.*No\s+resource\s+group\s+named\s+"missing"\s+is\s+visible\s+to\s+this\s+API\s+key\.`),
		}},
	})
}

func TestResourceGroupsDataSourcePlanPagesToTotalPages(t *testing.T) {
	t.Parallel()
	mock := startRgDataSourceTestMock(t, [][]rgDataSourceTestRow{
		{{id: "rg-1", name: "one"}},
		{{id: "rg-2", name: "two"}},
		{{id: "rg-3", name: "three", isDefault: true}},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: rgDataSourceTestConfig(mock.url, ""),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "resource_groups.#", "3"),
				resource.TestCheckResourceAttr(rgDataSourceTestAddress, "default_resource_group_id", "rg-3"),
				func(*terraform.State) error {
					got := mock.requestedPages()
					if len(got) < 3 || got[0] != "1" || got[1] != "2" || got[2] != "3" {
						return fmt.Errorf("pages requested = %v, want 1,2,3 first", got)
					}
					return nil
				},
			),
		}},
	})
}
