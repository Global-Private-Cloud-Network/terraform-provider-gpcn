package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var gpcnVolumeTest = "gpcn_volume.test"

func TestVolumesResource(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	volumeName := fmt.Sprintf("volume-basic-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_volume" "test" {
				name = "%s"

				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				volume_type = "SSD"

				size_gb = 256
			}
			`, volumeName),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", volumeName),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "size_gb", "256"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type", "SSD"),
					// Verify generated values are generated
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "location.country"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnVolumeTest, "location.region"),
					resource.TestCheckResourceAttr(gpcnVolumeTest, "volume_type_code", "vol-add-ssd"),
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
				ResourceName:            gpcnVolumeTest,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"created_time", "last_updated"},
			},
			// Update and Read testing with little changes
			// Increasing the size does not result in a replace
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_volume" "test" {
				name = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				volume_type = "SSD"
				size_gb = 512
			}
			`, volumeName),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", volumeName),
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
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_volume" "test" {
				name = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				volume_type = "SSD"
				size_gb = 256
			}
			`, volumeName),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify attributes are set to the values from the config
					resource.TestCheckResourceAttr(gpcnVolumeTest, "name", volumeName),
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

/*
*
----- Unit tests -----
*
*/
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

// The Description is the only place that tells the reader how to repair a
// volume whose SKU is unresolvable. The bytes are the release spec's.
func TestVolumeResourceVolumeTypeDescription(t *testing.T) {
	t.Parallel()

	var resp fwresource.SchemaResponse
	NewVolumesResource().Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Expected a schema, got %v", resp.Diagnostics)
	}

	want := "Type of storage: 'SSD', 'NVMe', or a storage component code such as 'vol-add-ultra'. " +
		"Use \"SSD\" or \"NVMe\" for the built-in storage classes and the component code for any other class. " +
		"The datacenter decides which codes it offers, and a code it does not offer is refused with the list of codes it does offer. " +
		"Changing this value requires replacing the volume. " +
		"A volume whose SKU the platform cannot resolve reads \"Unknown\"; after the platform repairs it, remove the volume from state and import it again so the code is recorded. " +
		"The built-in codes vol-add-ssd and vol-add-nvme are refused at plan time; write SSD or NVMe instead."

	attribute, ok := resp.Schema.Attributes["volume_type"]
	if !ok {
		t.Fatalf("Expected a volume_type attribute")
	}
	if got := attribute.GetDescription(); got != want {
		t.Errorf("Expected volume_type description %q, got %q", want, got)
	}
}
