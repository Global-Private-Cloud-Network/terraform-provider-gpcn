package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	rgPlanTestID        = "rg-1"
	rgPlanTestTimestamp = "2026-01-02T15:04:05Z"
	rgPlanTestPath      = "/v1/resource-groups/"

	rgPlanTestDefaultRefusalBody = `{"success":false,` +
		`"message":"The Default resource group cannot be deleted. Every tenant keeps one, and every resource that names no other group lives in it.",` +
		`"error":{"code":"RESOURCE_GROUP_IS_DEFAULT","statusCode":409}}`

	rgPlanTestNotEmptyRefusalBody = `{"success":false,` +
		`"message":"This resource group still has resources assigned to it. Move them to another group before deleting it.",` +
		`"error":{"code":"RESOURCE_GROUP_NOT_EMPTY","statusCode":409,` +
		`"details":{"virtualMachines":2,"gpuVirtualMachines":0,"volumes":1,"vpcs":0,"l2Segments":0}}}`
)

// rgPlanTestMock refuses a delete at most once, so the harness's final destroy
// succeeds after the test's refused one.
type rgPlanTestMock struct {
	url string

	mutex          sync.Mutex
	name           string
	description    string
	isDefault      bool
	deleteRefusal  string
	deleted        bool
	deleteNotFound bool
	deletePaths    []string
}

func (m *rgPlanTestMock) deleteRequests(path string) int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	count := 0
	for _, recorded := range m.deletePaths {
		if recorded == path {
			count++
		}
	}
	return count
}

func startRgPlanTestMock(t *testing.T, isDefault bool, deleteRefusal string) *rgPlanTestMock {
	t.Helper()
	mock := &rgPlanTestMock{isDefault: isDefault, deleteRefusal: deleteRefusal}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check" {
			testutil.HandleAuthCheck(w)
			return
		}
		mock.handle(t, w, r)
	}))
	t.Cleanup(server.Close)
	mock.url = server.URL
	return mock
}

func (m *rgPlanTestMock) handle(t *testing.T, w http.ResponseWriter, r *http.Request) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == rgPlanTestPath:
		m.applyBody(r)
		w.WriteHeader(http.StatusCreated)
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"resourceGroupId": rgPlanTestID}})
	case r.Method == http.MethodGet && r.URL.Path == rgPlanTestPath+rgPlanTestID:
		if m.deleted {
			rgPlanTestWrite(w, http.StatusNotFound, `{"success":false,"message":"not found","error":{"code":"Resource Not Found","statusCode":404}}`)
			return
		}
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{
			"id":          rgPlanTestID,
			"name":        m.name,
			"description": m.description,
			"createdAt":   rgPlanTestTimestamp,
			"updatedAt":   rgPlanTestTimestamp,
			"isDefault":   m.isDefault,
		}})
	case r.Method == http.MethodPut && r.URL.Path == rgPlanTestPath+rgPlanTestID:
		m.applyBody(r)
		testutil.WriteJSONResponse(w, map[string]any{"success": true})
	case r.Method == http.MethodDelete && r.URL.Path == rgPlanTestPath+rgPlanTestID:
		m.deletePaths = append(m.deletePaths, r.URL.Path)
		if m.deleteNotFound {
			rgPlanTestWrite(w, http.StatusNotFound, `{"success":false,"message":"Resource group not found"}`)
			return
		}
		if m.deleteRefusal != "" {
			body := m.deleteRefusal
			m.deleteRefusal = ""
			rgPlanTestWrite(w, http.StatusConflict, body)
			return
		}
		m.deleted = true
		w.WriteHeader(http.StatusAccepted)
		testutil.WriteJSONResponse(w, map[string]any{"success": true})
	default:
		testutil.LogUnexpectedRequest(t, w, r)
	}
}

func (m *rgPlanTestMock) applyBody(r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return
	}
	if name, ok := body["name"].(string); ok {
		m.name = name
	}
	if description, ok := body["description"].(string); ok {
		m.description = description
	}
}

func rgPlanTestWrite(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func rgPlanTestConfig(host, name string) string {
	return fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

resource "gpcn_resource_group" "test" {
  name = %q
}
`, host, name)
}

func TestResourceGroupResourcePlanCreateUpdateDestroy(t *testing.T) {
	t.Parallel()
	mock := startRgPlanTestMock(t, false, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if count := mock.deleteRequests(rgPlanTestPath + rgPlanTestID); count != 1 {
				return fmt.Errorf("DELETE requests for %s = %d, want 1", rgPlanTestID, count)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: rgPlanTestConfig(mock.url, "group-a"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "name", "group-a"),
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "is_default", "false"),
				),
			},
			{
				Config: rgPlanTestConfig(mock.url, "group-b"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnResourceGroupTest, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(gpcnResourceGroupTest, tfjsonpath.New("is_default"), knownvalue.Bool(false)),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "name", "group-b"),
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "is_default", "false"),
				),
			},
		},
	})
}

func TestResourceGroupResourcePlanReportsIsDefault(t *testing.T) {
	t.Parallel()
	mock := startRgPlanTestMock(t, true, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: rgPlanTestConfig(mock.url, "Default"),
				Check:  resource.TestCheckResourceAttr(gpcnResourceGroupTest, "is_default", "true"),
			},
			{
				Config:            rgPlanTestConfig(mock.url, "Default"),
				ResourceName:      gpcnResourceGroupTest,
				ImportState:       true,
				ImportStateId:     rgPlanTestID,
				ImportStateVerify: true,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["is_default"] != "true" {
						return fmt.Errorf("imported is_default = %v, want true", states)
					}
					return nil
				},
			},
		},
	})
}

// rgPlanTestStillManagedStep proves a refused destroy left the group in state.
func rgPlanTestStillManagedStep(host, name string) resource.TestStep {
	return resource.TestStep{
		Config: rgPlanTestConfig(host, name),
		ConfigPlanChecks: resource.ConfigPlanChecks{
			PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
		},
	}
}

func TestResourceGroupResourcePlanIsDefaultStaysUnknownFromNullState(t *testing.T) {
	t.Parallel()

	schemaResponse := &fwresource.SchemaResponse{}
	NewResourceGroupResource().Schema(context.Background(), fwresource.SchemaRequest{}, schemaResponse)
	attribute, ok := schemaResponse.Schema.Attributes["is_default"].(schema.BoolAttribute)
	if !ok {
		t.Fatalf("is_default is %T, want schema.BoolAttribute", schemaResponse.Schema.Attributes["is_default"])
	}

	request := planmodifier.BoolRequest{
		State:      tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})},
		StateValue: types.BoolNull(),
		PlanValue:  types.BoolUnknown(),
	}
	for _, modifier := range attribute.PlanModifiers {
		response := &planmodifier.BoolResponse{PlanValue: request.PlanValue}
		modifier.PlanModifyBool(context.Background(), request, response)
		request.PlanValue = response.PlanValue
	}
	if !request.PlanValue.IsUnknown() {
		t.Errorf("is_default plan = %v, want unknown: a null prior value must not carry into the plan", request.PlanValue)
	}
}

func TestResourceGroupResourcePlanRefusesDefaultDelete(t *testing.T) {
	t.Parallel()
	mock := startRgPlanTestMock(t, true, rgPlanTestDefaultRefusalBody)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: rgPlanTestConfig(mock.url, "Default")},
			{
				Config:  rgPlanTestConfig(mock.url, "Default"),
				Destroy: true,
				ExpectError: regexp.MustCompile(
					`(?s)Unable to Delete Resource Group.*is\s+the\s+tenant's\s+Default\s+resource\s+group,\s+and\s+GPCN\s+does\s+not\s+delete\s+it`),
			},
			rgPlanTestStillManagedStep(mock.url, "Default"),
		},
	})
}

func TestResourceGroupResourcePlanRefusesNonEmptyDelete(t *testing.T) {
	t.Parallel()
	mock := startRgPlanTestMock(t, false, rgPlanTestNotEmptyRefusalBody)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: rgPlanTestConfig(mock.url, "busy")},
			{
				Config:      rgPlanTestConfig(mock.url, "busy"),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`(?s)virtualMachines:\s+2,\s+volumes:\s+1\).*apply\s+the\s+moves\s+alone,\s+for\s+example\s+with\s+-target,\s+then\s+apply\s+again`),
			},
			rgPlanTestStillManagedStep(mock.url, "busy"),
		},
	})
}

func TestResourceGroupResourcePlanDestroyAlreadyGoneSucceeds(t *testing.T) {
	t.Parallel()
	mock := startRgPlanTestMock(t, false, "")
	mock.deleteNotFound = true

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if count := mock.deleteRequests(rgPlanTestPath + rgPlanTestID); count != 1 {
				return fmt.Errorf("DELETE requests for %s = %d, want 1", rgPlanTestID, count)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{Config: rgPlanTestConfig(mock.url, "gone")},
		},
	})
}
