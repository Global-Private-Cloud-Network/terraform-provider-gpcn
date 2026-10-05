package resourcegroups

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
)

func refusal(code string, details map[string]any) error {
	return &client.HTTPError{StatusCode: http.StatusConflict, Code: code, Message: "refused", Details: details}
}

func TestDeleteRefusalDetail_Default(t *testing.T) {
	detail, refused := DeleteRefusalDetail("rg-1", refusal("RESOURCE_GROUP_IS_DEFAULT", nil))
	want := "Resource group rg-1 is the tenant's Default resource group, and GPCN does not delete it. To stop managing it with Terraform, run: terraform state rm <address>"
	if !refused || detail != want {
		t.Fatalf("got %v %q, want %q", refused, detail, want)
	}
}

func TestDeleteRefusalDetail_NotEmptyWithDetails(t *testing.T) {
	details := map[string]any{
		"virtualMachines": float64(2), "gpuVirtualMachines": float64(0),
		"volumes": float64(1), "vpcs": float64(0), "l2Segments": float64(0),
	}
	detail, refused := DeleteRefusalDetail("rg-1", refusal("RESOURCE_GROUP_NOT_EMPTY", details))
	want := "Resource group rg-1 still has resources assigned to it (virtualMachines: 2, volumes: 1). Move them to another resource group, then destroy it again."
	if !refused || detail != want {
		t.Fatalf("got %v %q, want %q", refused, detail, want)
	}
}

func TestDeleteRefusalDetail_NotEmptyWithoutDetails(t *testing.T) {
	want := "Resource group rg-1 still has resources assigned to it. Move them to another resource group, then destroy it again."
	for name, details := range map[string]map[string]any{
		"absent":           nil,
		"all zero":         {"volumes": float64(0), "vpcs": float64(0)},
		"non numeric only": {"volumes": "three", "vpcs": nil},
	} {
		detail, refused := DeleteRefusalDetail("rg-1", refusal("RESOURCE_GROUP_NOT_EMPTY", details))
		if !refused || detail != want {
			t.Errorf("%s: got %v %q, want %q", name, refused, detail, want)
		}
	}
}

func TestDeleteRefusalDetail_IgnoresNonNumericEntry(t *testing.T) {
	details := map[string]any{"volumes": json.Number("3"), "vpcs": "many", "l2Segments": float64(1)}
	detail, refused := DeleteRefusalDetail("rg-1", refusal("RESOURCE_GROUP_NOT_EMPTY", details))
	if !refused || !strings.Contains(detail, "(l2Segments: 1, volumes: 3)") {
		t.Fatalf("got %v %q", refused, detail)
	}
}

func TestDeleteRefusalDetail_OtherErrors(t *testing.T) {
	for _, err := range []error{errors.New("boom"), refusal("SOMETHING_ELSE", nil)} {
		if detail, refused := DeleteRefusalDetail("rg-1", err); refused || detail != "" {
			t.Errorf("%v: got %v %q", err, refused, detail)
		}
	}
}

func assertListQuery(t *testing.T, r *http.Request) {
	t.Helper()
	query := r.URL.Query()
	if query.Get("limit") != "100" {
		t.Errorf("limit = %q, want 100 (query %q)", query.Get("limit"), r.URL.RawQuery)
	}
	if query.Get("page") == "" {
		t.Errorf("page missing (query %q)", r.URL.RawQuery)
	}
	if _, present := query["pageSize"]; present {
		t.Errorf("the API has no pageSize parameter (query %q)", r.URL.RawQuery)
	}
}

func listPage(rows []map[string]any, page, totalPages int) map[string]any {
	return map[string]any{
		"success": true,
		"data":    rows,
		"meta":    map[string]any{"page": page, "totalPages": totalPages},
	}
}

func TestFetchResourceGroups_SinglePage(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/resource-groups/" {
				t.Errorf("unexpected path %q", r.URL.Path)
			}
			assertListQuery(t, r)
			testutil.WriteJSONResponse(w, listPage([]map[string]any{
				{"id": "rg-1", "name": "Default", "description": "d", "isDefault": true},
				{"id": "rg-2", "name": "other", "description": "o", "isDefault": false},
			}, 1, 1))
		},
	})
	defer server.Close()

	rows, err := FetchResourceGroups(gpcnClient, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []ResourceGroupSummary{
		{ID: "rg-1", Name: "Default", Description: "d", IsDefault: true},
		{ID: "rg-2", Name: "other", Description: "o"},
	}
	if fmt.Sprint(rows) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", rows, want)
	}
}

func TestFetchResourceGroups_TwoPages(t *testing.T) {
	var pages []string
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			pages = append(pages, page)
			assertListQuery(t, r)
			if page == "1" {
				testutil.WriteJSONResponse(w, listPage([]map[string]any{{"id": "rg-1", "name": "a", "isDefault": false}}, 1, 2))
				return
			}
			testutil.WriteJSONResponse(w, listPage([]map[string]any{{"id": "rg-2", "name": "b", "isDefault": true}}, 2, 2))
		},
	})
	defer server.Close()

	rows, err := FetchResourceGroups(gpcnClient, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "rg-1" || rows[1].ID != "rg-2" || !rows[1].IsDefault {
		t.Fatalf("got %v", rows)
	}
	if strings.Join(pages, ",") != "1,2" {
		t.Fatalf("pages requested: %v", pages)
	}
}

func TestFetchResourceGroups_NullDescription(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			testutil.WriteJSONResponse(w, listPage([]map[string]any{{"id": "rg-1", "name": "a", "description": nil, "isDefault": false}}, 1, 1))
		},
	})
	defer server.Close()

	rows, err := FetchResourceGroups(gpcnClient, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Description != "" {
		t.Fatalf("got %v", rows)
	}
}

func TestMapResourceGroupResponseToModel_IsDefault(t *testing.T) {
	var response readResourceGroupResponse
	response.Data.ID = "rg-1"
	response.Data.IsDefault = true
	model := MapResourceGroupResponseToModel(&response, ResourceModel{})
	if !model.IsDefault.ValueBool() || model.IsDefault.IsNull() {
		t.Fatalf("is_default = %v", model.IsDefault)
	}
}
