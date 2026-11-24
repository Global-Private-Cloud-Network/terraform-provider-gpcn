package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const (
	vmImagesPlanTestDatacenterID = "dc-images-1"
	vmImagesPlanTestPath         = "/v1/resource/data-centers/" + vmImagesPlanTestDatacenterID + "/virtual-machine-images"
	vmImagesPlanTestDataSource   = "data.gpcn_virtualmachine_images.test"
)

func vmImagesPlanTestImage(id string, config map[string]any) map[string]any {
	return map[string]any{"id": id, "name": id, "config": config}
}

func vmImagesPlanTestBody() map[string]any {
	return map[string]any{
		"success": true,
		"message": "Virtual machine images retrieved successfully",
		"data": []any{
			map[string]any{
				"id":        1,
				"name":      "Linux",
				"sortOrder": 1,
				"images": []any{
					vmImagesPlanTestImage("img-both", map[string]any{
						"managed":        true,
						"customizations": map[string]any{"sshKey": true, "password": true, "username": true},
					}),
					vmImagesPlanTestImage("img-password", map[string]any{
						"managed":        true,
						"customizations": map[string]any{"sshKey": false, "password": true, "username": true},
					}),
					vmImagesPlanTestImage("img-key", map[string]any{
						"managed":        true,
						"customizations": map[string]any{"sshKey": true, "password": false, "username": true},
					}),
					vmImagesPlanTestImage("img-none", map[string]any{
						"managed":        true,
						"customizations": map[string]any{"sshKey": false, "password": false, "username": false},
					}),
				},
			},
			map[string]any{
				"id":        4,
				"name":      "Custom",
				"sortOrder": 4,
				"images": []any{
					vmImagesPlanTestImage("img-unmanaged-flags", map[string]any{
						"managed":        false,
						"customizations": map[string]any{"sshKey": true, "password": true, "username": true},
					}),
					vmImagesPlanTestImage("img-unmanaged-bare", map[string]any{
						"managed": false,
					}),
				},
			},
		},
	}
}

func TestVirtualMachineImagesDataSourcePlanReadsSetupFlags(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/auth/check" {
			testutil.HandleAuthCheck(w)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == vmImagesPlanTestPath {
			testutil.WriteJSONResponse(w, vmImagesPlanTestBody())
			return
		}
		testutil.LogUnexpectedRequest(t, w, r)
	}))
	t.Cleanup(server.Close)

	config := fmt.Sprintf(`
provider "gpcn" {
  host    = %q
  api_key = "test-key"
}

data "gpcn_virtualmachine_images" "test" {
  datacenter_id = %q
}
`, server.URL, vmImagesPlanTestDatacenterID)

	want := []struct {
		id              string
		managed         string
		acceptsSshKey   string
		acceptsPassword string
	}{
		{"img-both", "true", "true", "true"},
		{"img-password", "true", "false", "true"},
		{"img-key", "true", "true", "false"},
		{"img-none", "true", "false", "false"},
		{"img-unmanaged-flags", "false", "false", "false"},
		{"img-unmanaged-bare", "false", "false", "false"},
	}

	checks := make([]resource.TestCheckFunc, 0, 1+4*len(want))
	checks = append(checks, resource.TestCheckResourceAttr(vmImagesPlanTestDataSource, "images.#", fmt.Sprint(len(want))))
	for i, image := range want {
		prefix := fmt.Sprintf("images.%d.", i)
		checks = append(checks,
			resource.TestCheckResourceAttr(vmImagesPlanTestDataSource, prefix+"id", image.id),
			resource.TestCheckResourceAttr(vmImagesPlanTestDataSource, prefix+"managed", image.managed),
			resource.TestCheckResourceAttr(vmImagesPlanTestDataSource, prefix+"accepts_ssh_key", image.acceptsSshKey),
			resource.TestCheckResourceAttr(vmImagesPlanTestDataSource, prefix+"accepts_password", image.acceptsPassword),
		)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}
