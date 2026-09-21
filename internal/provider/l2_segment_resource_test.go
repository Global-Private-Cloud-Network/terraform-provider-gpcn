package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// l2SegmentTestDatacenterEnvVar names an L2-capable datacenter. Only the provider
// module decides L2 capability, so the case takes the datacenter from the
// environment rather than guessing one from a listing.
const l2SegmentTestDatacenterEnvVar = "GPCN_TEST_L2_DATACENTER_ID"

func l2SegmentTestConfig(name, description, datacenterID string) string {
	return providerConfig + fmt.Sprintf(`
resource "gpcn_l2_segment" "test" {
  name          = %q
  description   = %q
  datacenter_id = %q
}
`, name, description, datacenterID)
}

// TestL2SegmentResource creates a segment, renames it in place and destroys it.
// A rename is a database write, so it must never plan a replacement.
func TestL2SegmentResource(t *testing.T) {
	t.Parallel()
	datacenterID := os.Getenv(l2SegmentTestDatacenterEnvVar)

	suffix := acctest.RandString(8)
	name := fmt.Sprintf("segment-%s", suffix)
	renamed := fmt.Sprintf("segment-renamed-%s", suffix)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			if datacenterID == "" {
				t.Skipf("%s is not set: name an L2-capable datacenter", l2SegmentTestDatacenterEnvVar)
			}
		},
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: l2SegmentTestConfig(name, "Created by acceptance tests", datacenterID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "name", name),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "datacenter_id", datacenterID),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "description", "Created by acceptance tests"),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "state", "ready"),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "attached_nic_count", "0"),
					resource.TestCheckResourceAttrSet(gpcnL2SegmentTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnL2SegmentTest, "offering"),
					resource.TestCheckResourceAttrSet(gpcnL2SegmentTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnL2SegmentTest, "last_updated"),
				),
			},
			{
				// The timestamps are derived from the detail by the same mapper
				// the import path runs, so an import reproduces them exactly.
				ResourceName:      gpcnL2SegmentTest,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: l2SegmentTestConfig(renamed, "Renamed by acceptance tests", datacenterID),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnL2SegmentTest, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "name", renamed),
					resource.TestCheckResourceAttr(gpcnL2SegmentTest, "description", "Renamed by acceptance tests"),
				),
			},
		},
	})
}
