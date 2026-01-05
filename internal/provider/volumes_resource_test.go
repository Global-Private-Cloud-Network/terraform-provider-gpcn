package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var gpcnVolumesTest = "gpcn_volume.test"

func TestVolumesResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "gpcn_volume" "test" {
  name = "terraform-demo"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  volume_type = "SSD"

  size_gb = 256
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumesTest, "datacenter_id", "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "name", "terraform-demo"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "size_gb", "256"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "volume_type", "SSD"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "location.country"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "volume_type_id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect initial create action
						plancheck.ExpectResourceAction(gpcnVolumesTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// ImportState testing
			{
				ResourceName: gpcnVolumesTest,
				ImportState:  true,
			},
			// Update and Read testing with little changes
			// Increasing the size does not result in a replace
			{
				Config: providerConfig + `
resource "gpcn_volume" "test" {
  name = "terraform-demo"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  volume_type = "SSD"

  size_gb = 512
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumesTest, "datacenter_id", "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "name", "terraform-demo"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "size_gb", "512"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "volume_type", "SSD"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "created_time"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// This is a straightforward update, check for a regular update action
						plancheck.ExpectResourceAction(gpcnVolumesTest, plancheck.ResourceActionUpdate),
					},
				},
			},
			// Update and Read testing with a replace
			// Decreasing the size forces a replace
			{
				Config: providerConfig + `
resource "gpcn_volume" "test" {
  name = "terraform-demo"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  volume_type = "SSD"

  size_gb = 256
}
			`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumesTest, "datacenter_id", "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "name", "terraform-demo"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "size_gb", "256"),
					resource.TestCheckResourceAttr(gpcnVolumesTest, "volume_type", "SSD"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVolumesTest, "created_time"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Since we are switching to custom, we will need to destroy and re-create
						plancheck.ExpectResourceAction(gpcnVolumesTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

func TestVolumesResourceInvalidSize(t *testing.T) {
	t.Run("invalid_size", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_volume" "test" {
  name = "terraform-demo"

  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

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
