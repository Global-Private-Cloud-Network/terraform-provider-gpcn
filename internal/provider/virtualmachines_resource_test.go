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

var gpcnVirtualMachineTest = "gpcn_virtualmachine.test"

func TestVirtualMachinesResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + `
resource "gpcn_network" "vm_network" {
  name          = "vm-network-standard"
  network_type  = "standard"
  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
  cidr_block = "10.0.0.0/24"
  dhcp_start_address = "10.0.0.10"
  dhcp_end_address   = "10.0.0.254"
  dns_servers = "8.8.8.8, 8.8.4.4"
}

resource "gpcn_network" "vm_network_custom" {
  name          = "vm-network-custom"
  network_type  = "custom"
  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
}

resource "gpcn_volume" "vm_storage" {
  name          = "vm-storage-primary"
  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
  volume_type   = "SSD"
  size_gb       = 256
}

resource "gpcn_virtualmachine" "test" {
  name          = "terraform-demo-vm"
  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

  size = {
    category = "general"
    tier     = "g-micro-1"
  }
  image = "Alma Linux 8.x"

  wait_for_startup = false
  allocate_public_ip = false
  network_ids = [
    gpcn_network.vm_network.id,
    gpcn_network.vm_network_custom.id
  ]

  volume_ids = [
    gpcn_volume.vm_storage.id
  ]
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect initial create action
						plancheck.ExpectResourceAction("gpcn_volume.vm_storage", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_network.vm_network", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_network.vm_network_custom", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify computed attributes are set
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "last_updated"),
					// Verify location map is populated
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.country"),
					// Verify configuration map is populated
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "configuration.cpu"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "configuration.ram"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "configuration.base_storage"),
				),
			},
			// ImportState testing
			{
				ResourceName: gpcnVirtualMachineTest,
				ImportState:  true,
			},
			// Update and Read testing
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-demo-vm-update"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
						`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify name has been updated
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", "terraform-demo-vm-update"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// Verify network and volumes have been removed
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("volume_ids"), knownvalue.ListSizeExact(0)),
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("network_ids"), knownvalue.ListSizeExact(1)),
				},
			},
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-demo-vm-update"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 9.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
						`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify image name has been updated
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "image", "Alma Linux 9.x"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

func TestVirtualMachinesChangePublicIpAllocation(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Set baseline
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-demo-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
			`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect initial create action
						plancheck.ExpectResourceAction("gpcn_network.vm_network", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// Update allocate_public_ip to true
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-demo-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = true
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
			`,

				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect update
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("allocate_public_ip"), knownvalue.Bool(true)),
				},
			},
			// Release the IP
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-demo-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
			`,

				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect update
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("allocate_public_ip"), knownvalue.Bool(false)),
				},
			},
		},
	})
}

func TestVirtualMachinesSizeUpgrade(t *testing.T) {
	t.Skip("Resizing not currently functioning due to root size changes")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with g-micro-1 size
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-size-upgrade-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
			`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("size").AtMapKey("tier"), knownvalue.StringExact("g-micro-1")),
				},
			},
			// Upgrade to g-small-1 size - should update in place
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-size-upgrade-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-small-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
			`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Expect update, not replace, when upgrading size
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("size").AtMapKey("tier"), knownvalue.StringExact("g-small-1")),
				},
			},
		},
	})
}

func TestVirtualMachinesVolumeAttachment(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with no volumes
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "vm-storage-vol1"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "vm-storage-vol2"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-volume-test-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			}
			`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("volume_ids"), knownvalue.ListSizeExact(0)),
				},
			},
			// Attach one volume
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "vm-storage-vol1"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "vm-storage-vol2"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-volume-test-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]

			  volume_ids = [
			    gpcn_volume.vm_vol1.id
			  ]
			}
			`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("volume_ids"), knownvalue.ListSizeExact(1)),
				},
			},
			// Attach second volume
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "vm-storage-vol1"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "vm-storage-vol2"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-volume-test-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]

			  volume_ids = [
			    gpcn_volume.vm_vol1.id,
			    gpcn_volume.vm_vol2.id
			  ]
			}
			`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("volume_ids"), knownvalue.ListSizeExact(2)),
				},
			},
			// Remove first volume
			{
				Config: providerConfig + `
			resource "gpcn_network" "vm_network" {
			  name          = "vm-network-standard"
			  network_type  = "standard"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = "8.8.8.8, 8.8.4.4"
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "vm-storage-vol1"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "vm-storage-vol2"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "terraform-volume-test-vm"
			  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

			  wait_for_startup = false
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]

			  volume_ids = [
			    gpcn_volume.vm_vol2.id
			  ]
			}
			`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("volume_ids"), knownvalue.ListSizeExact(1)),
				},
			},
		},
	})
}

func TestVirtualMachinesInvalidSizes(t *testing.T) {
	t.Run("invalid_category", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_virtualmachine" "test" {
				  name          = "terraform-volume-test-vm"
				  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

				  size = {
				    category = "bad-category"
				    tier     = "g-micro-1"
				  }
				  image = "Alma Linux 8.x"

				  wait_for_startup = false
				  allocate_public_ip = false
				}
				`,
					ExpectError: regexp.MustCompile("Attribute size.category value must be one of"),
				},
			},
		})
	})

	t.Run("invalid_tier", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_virtualmachine" "test" {
				  name          = "terraform-volume-test-vm"
				  datacenter_id = "1ea6b709-0671-46fa-aea8-bdc8eb897d3d"

				  size = {
				    category = "general"
				    tier     = "bad-tier"
				  }
				  image = "Alma Linux 8.x"

				  wait_for_startup = false
				  allocate_public_ip = false
				}
				`,
					ExpectError: regexp.MustCompile("Attribute size.tier value must be one of"),
				},
			},
		})
	})
}
