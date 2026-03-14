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

var gpcnSSHKeyTest = "gpcn_ssh_key.test"

func TestSSHKeyResourceGeneratedEd25519(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a generated SSH key with an algorithm of ed25519 and no passphrase
			{
				Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name      = "terraform-acc-test-ed25519"
  algorithm = "ed25519"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", "terraform-acc-test-ed25519"),
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "algorithm", "ed25519"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "private_key"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "last_updated"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnSSHKeyTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// Update the name of the SSH key and validate the name has changed in the state
			{
				Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name      = "terraform-acc-test-ed25519-renamed"
  algorithm = "ed25519"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", "terraform-acc-test-ed25519-renamed"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnSSHKeyTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnSSHKeyTest, tfjsonpath.New("name"), knownvalue.StringExact("terraform-acc-test-ed25519-renamed")),
				},
			},
		},
	})
}

func TestSSHKeyResourceGeneratedWithPassphrase(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a generated SSH key with a passphrase
			{
				Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name       = "terraform-acc-test-passphrase"
  algorithm  = "ed25519"
  passphrase = "test-passphrase-12345"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", "terraform-acc-test-passphrase"),
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "algorithm", "ed25519"),
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "passphrase", "test-passphrase-12345"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "private_key"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "last_updated"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnSSHKeyTest, plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

func TestSSHKeyResourceImport(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create the resource to import
			{
				Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name      = "terraform-acc-test-import"
  algorithm = "ed25519"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
				),
			},
			// Import state sets all computed fields correctly
			{
				ResourceName:      gpcnSSHKeyTest,
				ImportState:       true,
				ImportStateVerify: true,
				// private_key is only returned at creation time and will not be present after import
				ImportStateVerifyIgnore: []string{"private_key"},
			},
		},
	})
}

func TestSSHKeyResourceUpload(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create an SSH key using a sample public key
			{
				Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name       = "terraform-acc-test-upload"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", "terraform-acc-test-upload"),
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "public_key", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"),
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "algorithm", "ed25519"),
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
			// Switch from uploaded key to generated key by removing public_key and adding algorithm
			{
				Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name      = "terraform-acc-test-upload"
  algorithm = "ed25519"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "name", "terraform-acc-test-upload"),
					resource.TestCheckResourceAttr(gpcnSSHKeyTest, "algorithm", "ed25519"),
					resource.TestCheckNoResourceAttr(gpcnSSHKeyTest, "public_key"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "private_key"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnSSHKeyTest, "last_updated"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnSSHKeyTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

func TestSSHKeyResourceValidation(t *testing.T) {
	t.Run("public_key_with_algorithm", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name       = "terraform-acc-test-validation"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
  algorithm  = "ed25519"
}
`,
					ExpectError: regexp.MustCompile(`'algorithm' cannot be set when 'public_key' is specified`),
				},
			},
		})
	})

	t.Run("public_key_with_passphrase", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name       = "terraform-acc-test-validation"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
  passphrase = "some-passphrase"
}
`,
					ExpectError: regexp.MustCompile(`'passphrase' cannot be set when 'public_key' is specified`),
				},
			},
		})
	})

	t.Run("missing_algorithm_and_public_key", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_ssh_key" "test" {
  name = "terraform-acc-test-validation"
}
`,
					ExpectError: regexp.MustCompile(`'algorithm' is required when 'public_key' is not specified`),
				},
			},
		})
	})
}
