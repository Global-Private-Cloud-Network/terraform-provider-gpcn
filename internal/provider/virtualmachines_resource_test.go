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

var gpcnVirtualMachineTest = "gpcn_virtualmachine.test"

func TestVirtualMachinesResource(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	sshKeyName := fmt.Sprintf("vm-basic-key-%s", rName)
	networkStdName := fmt.Sprintf("vm-basic-net-std-%s", rName)
	networkCustName := fmt.Sprintf("vm-basic-net-cust-%s", rName)
	volumeName := fmt.Sprintf("vm-basic-vol-%s", rName)
	vmName := fmt.Sprintf("vm-basic-%s", rName)
	vmNameUpdated := fmt.Sprintf("vm-basic-updated-%s", rName)

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

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
				name          = "%s"
				network_type  = "standard"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr_block = "10.0.0.0/24"
				dhcp_start_address = "10.0.0.10"
				dhcp_end_address   = "10.0.0.254"
				dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_network" "vm_network_custom" {
				name          = "%s"
				network_type  = "custom"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			}

			resource "gpcn_volume" "vm_storage" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				volume_type   = "SSD"
				size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size = {
					category = "general"
					tier     = "g-micro-1"
				}
				image = "Alma Linux 8.x"

				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id,
					gpcn_network.vm_network_custom.id
				]

				volume_ids = [
					gpcn_volume.vm_storage.id
				]

				auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, networkStdName, networkCustName, volumeName, vmName),
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
				ResourceName:            gpcnVirtualMachineTest,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"volume_ids"},
			},
			// Update and Read testing
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
				name          = "%s"
				network_type  = "standard"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr_block = "10.0.0.0/24"
				dhcp_start_address = "10.0.0.10"
				dhcp_end_address   = "10.0.0.254"
				dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				size = {
					category = "general"
					tier     = "g-micro-1"
				}
				image = "Alma Linux 8.x"
				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]
				auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, networkStdName, vmNameUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify name has been updated
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", vmNameUpdated),
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
			// Changing image forces a replace
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
				name          = "%s"
				network_type  = "standard"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr_block = "10.0.0.0/24"
				dhcp_start_address = "10.0.0.10"
				dhcp_end_address   = "10.0.0.254"
				dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				size = {
					category = "general"
					tier     = "g-micro-1"
				}
				image = "Alma Linux 9.x"
				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]
				auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, networkStdName, vmNameUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Verify image name has been updated
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "image", "Alma Linux 9.x"),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						// Changing image forces a replace
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("image"), knownvalue.StringExact("Alma Linux 9.x")),
				},
			},
		},
	})
}

func TestVirtualMachinesChangePublicIpAllocation(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	sshKeyName := fmt.Sprintf("vm-public-ip-key-%s", rName)
	networkName := fmt.Sprintf("vm-public-ip-net-%s", rName)
	vmName := fmt.Sprintf("vm-public-ip-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Set baseline
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vmName),
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
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = true
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vmName),

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
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vmName),
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
	t.Parallel()
	rName := acctest.RandString(8)
	sshKeyName := fmt.Sprintf("vm-size-upgrade-key-%s", rName)
	networkName := fmt.Sprintf("vm-size-upgrade-net-%s", rName)
	vmName := fmt.Sprintf("vm-size-upgrade-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with g-micro-1 size
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vmName),
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
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-small-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vmName),
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
	t.Parallel()
	rName := acctest.RandString(8)
	sshKeyName := fmt.Sprintf("vm-vol-attach-key-%s", rName)
	networkName := fmt.Sprintf("vm-vol-attach-net-%s", rName)
	vol1Name := fmt.Sprintf("vm-vol-attach-vol1-%s", rName)
	vol2Name := fmt.Sprintf("vm-vol-attach-vol2-%s", rName)
	vmName := fmt.Sprintf("vm-vol-attach-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with no volumes
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vol1Name, vol2Name, vmName),
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
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]

			  volume_ids = [
			    gpcn_volume.vm_vol1.id
			  ]

			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vol1Name, vol2Name, vmName),
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
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]

			  volume_ids = [
			    gpcn_volume.vm_vol1.id,
			    gpcn_volume.vm_vol2.id
			  ]

			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vol1Name, vol2Name, vmName),
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
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
			  name          = "%s"
			  network_type  = "standard"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  cidr_block = "10.0.0.0/24"
			  dhcp_start_address = "10.0.0.10"
			  dhcp_end_address   = "10.0.0.254"
			  dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_volume" "vm_vol1" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_volume" "vm_vol2" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
			  volume_type   = "SSD"
			  size_gb       = 256
			}

			resource "gpcn_virtualmachine" "test" {
			  name          = "%s"
			  datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

			  size = {
			    category = "general"
			    tier     = "g-micro-1"
			  }
			  image = "Alma Linux 8.x"

	
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]

			  volume_ids = [
			    gpcn_volume.vm_vol2.id
			  ]

			  auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKeyName, networkName, vol1Name, vol2Name, vmName),
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

func TestVirtualMachinesAuth(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	sshKeyName := fmt.Sprintf("vm-auth-key-%s", rName)
	networkName := fmt.Sprintf("vm-auth-net-%s", rName)
	vmName := fmt.Sprintf("vm-auth-%s", rName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with ssh_key_id and username
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
			}

			resource "gpcn_network" "vm_network" {
				name          = "%s"
				network_type  = "standard"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr_block = "10.0.0.0/24"
				dhcp_start_address = "10.0.0.10"
				dhcp_end_address   = "10.0.0.254"
				dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size = {
					category = "general"
					tier     = "g-micro-1"
				}
				image = "Alma Linux 8.x"

	
				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]

				auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, networkName, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "auth.ssh_key_id"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "auth.username", "testuser"),
				),
			},
			// Switch to password + username - should force replace
			{
				Config: providerConfig + fmt.Sprintf(`
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name = "Chicago"
			}

			resource "gpcn_network" "vm_network" {
				name          = "%s"
				network_type  = "standard"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				cidr_block = "10.0.0.0/24"
				dhcp_start_address = "10.0.0.10"
				dhcp_end_address   = "10.0.0.254"
				dns_servers = ["8.8.8.8", "8.8.4.4"]
			}

			resource "gpcn_virtualmachine" "test" {
				name          = "%s"
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id

				size = {
					category = "general"
					tier     = "g-micro-1"
				}
				image = "Alma Linux 8.x"

	
				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]

				auth = {
					password = "Test1Password!"
					username = "newuser"
				}
			}
			`, networkName, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "auth.username", "newuser"),
				),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("auth").AtMapKey("username"), knownvalue.StringExact("newuser")),
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
func TestVirtualMachinesInvalidSizes(t *testing.T) {
	t.Run("invalid_category", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: providerConfig + `
				resource "gpcn_virtualmachine" "test" {
				  name          = "terraform-volume-test-vm"
				  datacenter_id = "any-datacenter-id"

				  size = {
				    category = "bad-category"
				    tier     = "g-micro-1"
				  }
				  image = "Alma Linux 8.x"

		
				  allocate_public_ip = false
				  auth = {
				    ssh_key_id = "ssh-key-123"
				    username    = "testuser"
				  }
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
				  datacenter_id = "any-datacenter-id"

				  size = {
				    category = "general"
				    tier     = "bad-tier"
				  }
				  image = "Alma Linux 8.x"

		
				  allocate_public_ip = false
				  auth = {
				    ssh_key_id = "ssh-key-123"
				    username    = "testuser"
				  }
				}
				`,
					ExpectError: regexp.MustCompile("Attribute size.tier value must be one of"),
				},
			},
		})
	})
}

func TestVirtualMachinesInvalidAuth(t *testing.T) {
	// vmConfigWithAuth is a helper that builds a minimal VM config with the given auth block content.
	vmConfigWithAuth := func(authBlock string) string {
		return providerConfig + `
		resource "gpcn_virtualmachine" "test" {
		  name          = "terraform-auth-test-vm"
		  datacenter_id = "any-datacenter-id"
		  size = {
		    category = "general"
		    tier     = "g-micro-1"
		  }
		  image            = "Alma Linux 8.x"
		  allocate_public_ip = false
		  auth = {
		    username = "testuser"
		    ` + authBlock + `
		  }
		}
		`
	}

	t.Run("ssh_key_id_and_password_conflict", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`ssh_key_id = "ssh-key-123"` + "\n" + `password = "Test1Password!"`),
					ExpectError: regexp.MustCompile("cannot be specified when"),
				},
			},
		})
	})

	t.Run("password_too_short", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`password = "Short1!"`),
					ExpectError: regexp.MustCompile("Invalid password length"),
				},
			},
		})
	})

	t.Run("password_too_long", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`password = "GigaLongPassword123!ExtraChars"`),
					ExpectError: regexp.MustCompile("Invalid password length"),
				},
			},
		})
	})

	t.Run("password_missing_uppercase", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`password = "test1password!"`),
					ExpectError: regexp.MustCompile("Password missing uppercase letter"),
				},
			},
		})
	})

	t.Run("password_missing_lowercase", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`password = "TEST1PASSWORD!"`),
					ExpectError: regexp.MustCompile("Password missing lowercase letter"),
				},
			},
		})
	})

	t.Run("password_missing_digit", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`password = "TestPassword!!"`),
					ExpectError: regexp.MustCompile("Password missing digit"),
				},
			},
		})
	})

	t.Run("password_missing_symbol", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`password = "Test1Password1"`),
					ExpectError: regexp.MustCompile("Password missing symbol"),
				},
			},
		})
	})

	t.Run("password_invalid_chars", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					// $ is not in the allowed set: ! @ # % - _ .
					Config:      vmConfigWithAuth(`password = "Test1Pass$word"`),
					ExpectError: regexp.MustCompile("Invalid password characters"),
				},
			},
		})
	})

	t.Run("username_too_short", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`ssh_key_id = "ssh-key-123"` + "\n" + `username = "ab"`),
					ExpectError: regexp.MustCompile("string length must be between"),
				},
			},
		})
	})

	t.Run("username_too_long", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`ssh_key_id = "ssh-key-123"` + "\n" + `username = "averylongusernamethatexceedslimit"`),
					ExpectError: regexp.MustCompile("string length must be between"),
				},
			},
		})
	})

	t.Run("username_starts_with_digit", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config:      vmConfigWithAuth(`ssh_key_id = "ssh-key-123"` + "\n" + `username = "1testuser"`),
					ExpectError: regexp.MustCompile("Username must start with a letter or underscore"),
				},
			},
		})
	})

	t.Run("username_invalid_chars", func(t *testing.T) {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					// spaces are not allowed
					Config:      vmConfigWithAuth(`ssh_key_id = "ssh-key-123"` + "\n" + `username = "test user"`),
					ExpectError: regexp.MustCompile("Username must start with a letter or underscore"),
				},
			},
		})
	})
}
