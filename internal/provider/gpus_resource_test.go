package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var gpcnGPUTest = "gpcn_gpu.test"

func TestGPUResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "central"
  name         = "Kansas"
}

resource "gpcn_gpu" "test" {
  name          = "terraform-demo-gpu"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  series_name   = "RTX A6000 Series"
  gpu_count     = 1
  image_name    = "ubuntu-22.04"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect initial create action
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify computed attributes are set
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "last_updated"),
					// Verify series_code was computed from series_name
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_code", "rtx_a6000_series"),
					// Verify location map is populated
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnGPUTest, "location.country"),
					// Verify configured attributes
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", "terraform-demo-gpu"),
					resource.TestCheckResourceAttr(gpcnGPUTest, "series_name", "RTX A6000 Series"),
					resource.TestCheckResourceAttr(gpcnGPUTest, "gpu_count", "1"),
				),
			},
			// ImportState testing
			{
				ResourceName: gpcnGPUTest,
				ImportState:  true,
			},
			// Update and Read testing (name change)
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "central"
  name         = "Kansas"
}

resource "gpcn_gpu" "test" {
  name          = "terraform-demo-gpu-updated"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  series_name   = "RTX A6000 Series"
  gpu_count     = 1
  image_name    = "ubuntu-22.04"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect update action for name change
						plancheck.ExpectResourceAction(gpcnGPUTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify name has been updated
					resource.TestCheckResourceAttr(gpcnGPUTest, "name", "terraform-demo-gpu-updated"),
				),
			},
		},
	})
}

func TestGPUResourceNoAvailability(t *testing.T) {
	// This is subject to change based on datacenter availability
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "gpcn_datacenters" "central_us" {
  country_name = "United States"
  region_name  = "east"
  name = "Beltsville"
}

resource "gpcn_gpu" "test" {
  name          = "terraform-demo-gpu-no-availability"
  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
  series_code   = "a100_series"
  gpu_count     = 4
  image_name    = "ubuntu-22.04"
}
`,
				// This should fail due to insufficient GPU availability
				// The exact error message depends on the API response
				ExpectError: regexp.MustCompile("(no GPU availability|No GPU Inventory Available|Unable to create GPCN GPU)"),
			},
		},
	})
}

func TestGPUResourceInvalidSeries(t *testing.T) {
	t.Run("invalid_series_code", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_gpu" "test" {
  name          = "terraform-gpu-bad-code"
  datacenter_id = "any-datacenter-id"
  series_code   = "invalid_series_code"
  gpu_count     = 1
  image_name    = "ubuntu-22.04"
}
`,
					ExpectError: regexp.MustCompile("Attribute series_code value must be one of"),
				},
			},
		})
	})

	t.Run("invalid_series_name", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_gpu" "test" {
  name          = "terraform-gpu-bad-series"
  datacenter_id = "any-datacenter-id"
  series_name   = "Invalid GPU Series"
  gpu_count     = 1
  image_name    = "ubuntu-22.04"
}
`,
					ExpectError: regexp.MustCompile("Attribute series_name value must be one of"),
				},
			},
		})
	})

	t.Run("both_code_and_name", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_gpu" "test" {
  name          = "terraform-gpu-both-series"
  datacenter_id = "any-datacenter-id"
  series_name   = "RTX A6000 Series"
  series_code   = "rtx_a6000_series"
  gpu_count     = 1
  image_name    = "ubuntu-22.04"
}
`,
					ExpectError: regexp.MustCompile("2 attributes specified"),
				},
			},
		})
	})

	t.Run("neither_code_nor_name", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
resource "gpcn_gpu" "test" {
  name          = "terraform-gpu-no-series"
  datacenter_id = "any-datacenter-id"
  gpu_count     = 1
  image_name    = "ubuntu-22.04"
}
`,
					ExpectError: regexp.MustCompile("No attribute specified when one"),
				},
			},
		})
	})
}

func TestGPUResourceInvalidImageName(t *testing.T) {
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
}
`,
				ExpectError: regexp.MustCompile("Attribute image_name value must be one of"),
			},
		},
	})
}

func TestGPUResourceInvalidGPUCount(t *testing.T) {
	t.Run("gpu_count_zero", func(t *testing.T) {
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
}
`,
					ExpectError: regexp.MustCompile("Attribute gpu_count value must be one of"),
				},
			},
		})
	})
}
