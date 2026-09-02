package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var gpcnResourceGroupTest = "gpcn_resource_group.test"

func TestResourceGroupResource(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	name := fmt.Sprintf("group-%s", rName)
	nameUpdated := fmt.Sprintf("group-updated-%s", rName)
	description := "Test resource group created by acceptance tests"
	descriptionUpdated := "Updated description for acceptance tests"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroy,
		Steps: []resource.TestStep{
			// Create and Read testing (with description)
			{
				Config: providerConfig + fmt.Sprintf(`
			resource "gpcn_resource_group" "test" {
				name        = "%s"
				description = "%s"
			}
			`, name, description),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "name", name),
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "description", description),
					resource.TestCheckResourceAttrSet(gpcnResourceGroupTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnResourceGroupTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnResourceGroupTest, "last_updated"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnResourceGroupTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// ImportState testing
			{
				ResourceName:            gpcnResourceGroupTest,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"created_time", "last_updated"},
			},
			// Update name and description
			{
				Config: providerConfig + fmt.Sprintf(`
			resource "gpcn_resource_group" "test" {
				name        = "%s"
				description = "%s"
			}
			`, nameUpdated, descriptionUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "name", nameUpdated),
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "description", descriptionUpdated),
					resource.TestCheckResourceAttrSet(gpcnResourceGroupTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnResourceGroupTest, "last_updated"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnResourceGroupTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnResourceGroupTest, tfjsonpath.New("name"), knownvalue.StringExact(nameUpdated)),
					statecheck.ExpectKnownValue(gpcnResourceGroupTest, tfjsonpath.New("description"), knownvalue.StringExact(descriptionUpdated)),
				},
			},
			// Remove description (set to null)
			{
				Config: providerConfig + fmt.Sprintf(`
			resource "gpcn_resource_group" "test" {
				name = "%s"
			}
			`, nameUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnResourceGroupTest, "name", nameUpdated),
					resource.TestCheckNoResourceAttr(gpcnResourceGroupTest, "description"),
					resource.TestCheckResourceAttrSet(gpcnResourceGroupTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnResourceGroupTest, plancheck.ResourceActionUpdate),
					},
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
func TestResourceGroupNameLengthValidator(t *testing.T) {
	t.Run("name_too_long", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			CheckDestroy:             testAccCheckDestroy,
			Steps: []resource.TestStep{
				{
					// 65-character name (exceeds 64 max)
					Config: providerConfig + `
				resource "gpcn_resource_group" "test" {
					name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				}
				`,
					ExpectError: regexp.MustCompile("Invalid Attribute Value Length"),
				},
			},
		})
	})
}

func TestResourceGroupDescriptionLengthValidator(t *testing.T) {
	t.Run("description_too_long", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			CheckDestroy:             testAccCheckDestroy,
			Steps: []resource.TestStep{
				{
					// 256-character description (exceeds 255 max)
					Config: providerConfig + `
				resource "gpcn_resource_group" "test" {
					name        = "valid-name"
					description = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				}
				`,
					ExpectError: regexp.MustCompile("Invalid Attribute Value Length"),
				},
			},
		})
	})
}
