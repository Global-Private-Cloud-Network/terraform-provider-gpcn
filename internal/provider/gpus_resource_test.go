package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var gpcnGPUTest = "gpcn_gpu.test"

func TestGPUResource(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	gpuName := fmt.Sprintf("gpu-basic-%s", rName)
	gpuNameUpdated := fmt.Sprintf("gpu-basic-updated-%s", rName)
	sshKeyName := fmt.Sprintf("gpu-basic-key-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "central"
				name         = "Kansas"
			}

			resource "gpcn_ssh_key" "test" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_gpu" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				series_name   = "RTX A6000 Series"
				gpu_count     = 1
				image_name    = "ubuntu-22.04"
				auth = {
					ssh_key_id = gpcn_ssh_key.test.id
				}
			}
			`, sshKeyName, gpuName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "last_updated"),
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_code", "rtx_a6000_series"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "location.country"),
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", gpuName),
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_name", "RTX A6000 Series"),
					resource.TestCheckResourceAttr(gpcnGPUTest, "gpu_count", "1"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "auth.ssh_key_id"),
				),
			},
			// ImportState testing
			{
				ResourceName:      gpcnGPUTest,
				ImportState:       true,
				ImportStateVerify: true,
				// Image name is mapped to a "full" image name on the API
				ImportStateVerifyIgnore: []string{"image_name", "created_time", "last_updated"},
			},
			// Update and Read testing (name change only — does not trigger replacement)
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "central"
				name         = "Kansas"
			}

			resource "gpcn_ssh_key" "test" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_gpu" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				series_name   = "RTX A6000 Series"
				gpu_count     = 1
				image_name    = "ubuntu-22.04"
				auth = {
					ssh_key_id = gpcn_ssh_key.test.id
				}
			}
			`, sshKeyName, gpuNameUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", gpuNameUpdated),
				),
			},
		},
	})
}

func TestGPUResourceAuthReplacement(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	gpuName := fmt.Sprintf("gpu-auth-replace-%s", rName)
	key1Name := fmt.Sprintf("gpu-auth-key-1-%s", rName)
	key2Name := fmt.Sprintf("gpu-auth-key-2-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "central"
				name         = "Kansas"
			}

			resource "gpcn_ssh_key" "key1" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl key1"
			}

			resource "gpcn_gpu" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				series_name   = "RTX A6000 Series"
				gpu_count     = 1
				image_name    = "ubuntu-22.04"
				auth = {
					ssh_key_id = gpcn_ssh_key.key1.id
				}
			}
			`, key1Name, gpuName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "id"),
				),
			},
			// Changing ssh_key_id triggers replacement
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "central"
				name         = "Kansas"
			}

			resource "gpcn_ssh_key" "key2" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl key2"
			}

			resource "gpcn_gpu" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				series_name   = "RTX A6000 Series"
				gpu_count     = 1
				image_name    = "ubuntu-22.04"
				auth = {
					ssh_key_id = gpcn_ssh_key.key2.id
				}
			}
			`, key2Name, gpuName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "auth.ssh_key_id"),
				),
			},
		},
	})
}

func TestGPUResourceNoAvailability(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	gpuName := fmt.Sprintf("gpu-no-avail-%s", rName)
	sshKeyName := fmt.Sprintf("gpu-no-avail-key-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "east"
				name = "Maryland"
			}

			resource "gpcn_ssh_key" "test" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_gpu" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				series_code   = "a100_series"
				gpu_count     = 4
				image_name    = "ubuntu-22.04"
				auth = {
					ssh_key_id = gpcn_ssh_key.test.id
				}
			}
			`, sshKeyName, gpuName),
				ExpectError: regexp.MustCompile("(no GPU availability|No GPU Inventory Available|Unable to create GPCN GPU)"),
			},
		},
	})
}

/*
*
----- Unit tests -----
*
*/
func TestGPUResourceInvalidSeries(t *testing.T) {
	gpuConfigWithSeries := func(seriesField, gpuCount, imageName string) string {
		return providerConfig + fmt.Sprintf(`
		resource "gpcn_gpu" "test" {
		  name          = "terraform-gpu-series-test"
		  datacenter_id = "any-datacenter-id"
		  %s
		  gpu_count  = %s
		  image_name = %q
		  auth = {
		    ssh_key_id = "any-ssh-key-id"
		  }
		}
		`, seriesField, gpuCount, imageName)
	}

	tests := []struct {
		name        string
		seriesField string
		gpuCount    string
		imageName   string
		wantErr     string
	}{
		{
			name:        "invalid_series_code",
			seriesField: `series_code = "invalid_series_code"`,
			gpuCount:    "1", imageName: "ubuntu-22.04",
			wantErr: "Attribute series_code value must be one of",
		},
		{
			name:        "invalid_series_name",
			seriesField: `series_name = "Invalid GPU Series"`,
			gpuCount:    "1", imageName: "ubuntu-22.04",
			wantErr: "Attribute series_name value must be one of",
		},
		{
			name:        "both_code_and_name",
			seriesField: `series_name = "RTX A6000 Series"` + "\n" + `		  series_code = "rtx_a6000_series"`,
			gpuCount:    "1", imageName: "ubuntu-22.04",
			wantErr: "2 attributes specified",
		},
		{
			name:        "neither_code_nor_name",
			seriesField: "",
			gpuCount:    "1", imageName: "ubuntu-22.04",
			wantErr: "No attribute specified when one",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      gpuConfigWithSeries(tc.seriesField, tc.gpuCount, tc.imageName),
						ExpectError: regexp.MustCompile(tc.wantErr),
					},
				},
			})
		})
	}
}

func TestGPUResourceInvalidImageName(t *testing.T) {
	t.Parallel()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
			resource "gpcn_gpu" "test" {
				name          = "terraform-gpu-invalid-image"
				datacenter_id = "any-datacenter-id"
				series_name   = "RTX A6000 Series"
				gpu_count     = 1
				image_name    = "Invalid Image Name"
				auth = {
					ssh_key_id = "any-ssh-key-id"
				}
			}
			`,
				ExpectError: regexp.MustCompile("Attribute image_name value must be one of"),
			},
		},
	})
}

func TestGPUResourceInvalidGPUCount(t *testing.T) {
	t.Run("gpu_count_zero", func(t *testing.T) {
		t.Parallel()
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_gpu" "test" {
					name          = "terraform-gpu-invalid-count"
					datacenter_id = "any-datacenter-id"
					series_name   = "RTX A6000 Series"
					gpu_count     = 0
					image_name    = "ubuntu-22.04"
					auth = {
						ssh_key_id = "any-ssh-key-id"
					}
				}
				`,
					ExpectError: regexp.MustCompile("Attribute gpu_count value must be one of"),
				},
			},
		})
	})
}

func TestGPUResourceMissingAuth(t *testing.T) {
	t.Parallel()
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
			resource "gpcn_gpu" "test" {
				name          = "terraform-gpu-missing-auth"
				datacenter_id = "any-datacenter-id"
				series_name   = "RTX A6000 Series"
				gpu_count     = 1
				image_name    = "ubuntu-22.04"
			}
			`,
				ExpectError: regexp.MustCompile(`The argument "auth" is required`),
			},
		},
	})
}
