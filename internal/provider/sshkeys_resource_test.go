package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

var gpcnSSHKeyTest = "gpcn_ssh_key.test"

func TestSSHKeyResourceUpload(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	nameInitial := fmt.Sprintf("ssh-key-upload-%s", rName)
	nameRenamed := fmt.Sprintf("ssh-key-upload-renamed-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create an SSH key using a sample public key
			{
				Config: providerConfig + fmt.Sprintf(`
resource "gpcn_ssh_key" "test" {
  name       = "%s"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
}
`, nameInitial),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", nameInitial),
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "public_key", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "last_updated"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnSSHKeyTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// Update the name and validate the name has changed in the state
			{
				Config: providerConfig + fmt.Sprintf(`
resource "gpcn_ssh_key" "test" {
  name       = "%s"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
}
`, nameRenamed),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", nameRenamed),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnSSHKeyTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnSSHKeyTest, tfjsonpath.New("name"), knownvalue.StringExact(nameRenamed)),
				},
			},
			// Update the key and validate a recreate must happen and the new key is updated in state
			{
				Config: providerConfig + fmt.Sprintf(`
resource "gpcn_ssh_key" "test" {
  name       = "%s"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test-2"
}
`, nameRenamed),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", nameRenamed),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnSSHKeyTest, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnSSHKeyTest, tfjsonpath.New("public_key"), knownvalue.StringExact("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test-2")),
				},
			},
			// Import state sets all computed fields correctly
			{
				ResourceName:            gpcnSSHKeyTest,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"created_time", "last_updated"},
			},
		},
	})
}
