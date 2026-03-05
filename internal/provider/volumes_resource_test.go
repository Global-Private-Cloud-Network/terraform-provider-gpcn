package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var gpcnVolumeTest = "gpcn_volume.test"

func TestVolumesResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name = "Chicago"
}

resource "gpcn_volume" "test" {
  name = "terraform-demo"

  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

  volume_type = "SSD"

  size_gb = 256
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", "terraform-demo"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", "256"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type", "SSD"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "location.country"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "volume_type_id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect initial create action
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// ImportState testing
			{
				ResourceName: gpcnVolumeTest,
				ImportState:  true,
			},
			// Update and Read testing with little changes
			// Increasing the size does not result in a replace
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name = "Chicago"
}

resource "gpcn_volume" "test" {
  name = "terraform-demo"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  volume_type = "SSD"
  size_gb = 512
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", "terraform-demo"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", "512"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type", "SSD"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "created_time"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// This is a straightforward update, check for a regular update action
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVolumeTest, tfjsonpath.New("size_gb"), knownvalue.Int64Exact(512)),
				},
			},
			// Update and Read testing with a replace
			// Decreasing the size forces a replace
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name = "Chicago"
}

resource "gpcn_volume" "test" {
  name = "terraform-demo"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  volume_type = "SSD"
  size_gb = 256
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", "terraform-demo"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", "256"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type", "SSD"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "created_time"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Decreasing size forces a replace
						plancheck.ExpectResourceAction(gpcnVolumeTest, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVolumeTest, tfjsonpath.New("size_gb"), knownvalue.Int64Exact(256)),
				},
			},
		},
	})
}

func TestVolumesResourceInvalidSize(t *testing.T) {
	t.Run("invalid_size", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "Central"
  name = "Chicago"
}

resource "gpcn_volume" "test" {
  name = "terraform-demo"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  volume_type = "SSD"
  size_gb = 555
}
			`,
					ExpectError: regexp.MustCompile("the specified volume size is not available for this datacenter"),
				},
			},
		})
	})
}
