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

// dataCenterImagesAndSize returns the common datacenter, image, and size datasource lookup blocks for Chicago.
func dataCenterImagesAndSize() string {
	return `
data "gpcn_datacenters" "central_us" {
	country_name = "United States"
	region_name  = "Central"
	name         = "Chicago"
}

data "gpcn_virtualmachine_images" "vm_image" {
	datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
	image_name    = "Alma Linux 8"
}

data "gpcn_virtualmachine_sizes" "vm_size" {
	datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
	category      = "general-purpose"
	min_cpu       = 2
}
`
}

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
				Config: providerConfig + dataCenterImagesAndSize() + fmt.Sprintf(`
			resource "gpcn_resource_group" "vm_group" {
				name = "terraform-demo-group"
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

				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id

				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id,
					gpcn_network.vm_network_custom.id
				]

				resource_group_id = gpcn_resource_group.vm_group.id

				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, networkStdName, networkCustName, volumeName, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_resource_group.vm_group", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_ssh_key.vm_uploaded_key", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_volume.vm_storage", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_network.vm_network", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_network.vm_network_custom", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "id"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "created_time"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "last_updated"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.datacenter"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.region"),
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "location.country"),
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
				ImportStateVerifyIgnore: []string{"created_time", "last_updated"},
			},
			// Update and Read testing
			{
				Config: providerConfig + dataCenterImagesAndSize() + fmt.Sprintf(`
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
				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]
				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, networkStdName, vmNameUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "name", vmNameUpdated),
				),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_resource_group.vm_group", plancheck.ResourceActionDestroy),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("network_ids"), knownvalue.ListSizeExact(1)),
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("resource_group_id"), knownvalue.Null()),
				},
			},
			// Changing image_id forces a replace
			{
				Config: providerConfig + `
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name         = "Chicago"
			}
			data "gpcn_virtualmachine_images" "vm_image" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				image_name    = "Alma Linux 9"
			}
			data "gpcn_virtualmachine_sizes" "vm_size" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				category      = "general-purpose"
				min_cpu       = 2
			}
			` + fmt.Sprintf(`
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
				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]
				initial_auth = {
					ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
					username   = "testuser"
				}
			}
			`, sshKeyName, networkStdName, vmNameUpdated),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "image_id"),
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
	t.Parallel()
	rName := acctest.RandString(8)
	sshKeyName := fmt.Sprintf("vm-public-ip-key-%s", rName)
	networkName := fmt.Sprintf("vm-public-ip-net-%s", rName)
	vmName := fmt.Sprintf("vm-public-ip-%s", rName)

	vmConfig := func(sshKey, network, vm string, allocatePublicIp bool) string {
		return providerConfig + dataCenterImagesAndSize() + fmt.Sprintf(`
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
			  size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
			  image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
			  allocate_public_ip = %t
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  initial_auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKey, network, vm, allocatePublicIp)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Set baseline
			{
				Config: vmConfig(sshKeyName, networkName, vmName, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_network.vm_network", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// Update allocate_public_ip to true
			{
				Config: vmConfig(sshKeyName, networkName, vmName, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("allocate_public_ip"), knownvalue.Bool(true)),
				},
			},
			// Release the IP
			{
				Config: vmConfig(sshKeyName, networkName, vmName, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
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

	vmConfig := func(sshKey, network, vm, sizeDataSource string) string {
		return providerConfig + `
			data "gpcn_datacenters" "central_us" {
				country_name = "United States"
				region_name  = "Central"
				name         = "Chicago"
			}

			data "gpcn_virtualmachine_images" "vm_image" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				image_name    = "Alma Linux 8"
			}
		` + sizeDataSource + fmt.Sprintf(`
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
			  size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
			  image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
			  allocate_public_ip = false
			  network_ids = [
			    gpcn_network.vm_network.id
			  ]
			  initial_auth = {
				ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
    			username   = "testuser"
			  }
			}
			`, sshKey, network, vm)
	}

	microSize := `
			data "gpcn_virtualmachine_sizes" "vm_size" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				category      = "general-purpose"
				min_cpu       = 2
				min_memory_gb = 4
			}
	`
	smallSize := `
			data "gpcn_virtualmachine_sizes" "vm_size" {
				datacenter_id = data.gpcn_datacenters.central_us.datacenters[0].id
				category      = "general-purpose"
				min_cpu       = 4
			}
	`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with micro size
			{
				Config: vmConfig(sshKeyName, networkName, vmName, microSize),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("size_id"), knownvalue.NotNull()),
				},
			},
			// Upgrade to a larger size - should update in place
			{
				Config: vmConfig(sshKeyName, networkName, vmName, smallSize),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("size_id"), knownvalue.NotNull()),
				},
			},
			// Downgrade back to micro - should require replacement
			{
				Config: vmConfig(sshKeyName, networkName, vmName, microSize),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionReplace),
					},
				},
			},
		},
	})
}

func TestVirtualMachinesVolumeAttachment(t *testing.T) {
	t.Parallel()
	rName := acctest.RandString(8)
	sshKeyName := fmt.Sprintf("vm-vol-attach-key-%s", rName)
	vol1Name := fmt.Sprintf("vm-vol-attach-vol1-%s", rName)
	vol2Name := fmt.Sprintf("vm-vol-attach-vol2-%s", rName)
	vmName := fmt.Sprintf("vm-vol-attach-%s", rName)

	vmBase := func(sshKey, vol1, vol2, vm string) string {
		return providerConfig + dataCenterImagesAndSize() + fmt.Sprintf(`
			resource "gpcn_ssh_key" "vm_uploaded_key" {
				name       = "%s"
				public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl terraform-acc-test"
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
			  size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
			  image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id
			  allocate_public_ip = false
			  initial_auth = {
			    ssh_key_id = gpcn_ssh_key.vm_uploaded_key.id
			    username   = "testuser"
			  }
			}
			`, sshKey, vol1, vol2, vm)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create VM with no volumes
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume.vm_vol1", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction("gpcn_volume.vm_vol2", plancheck.ResourceActionCreate),
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionCreate),
					},
				},
			},
			// Attach the first volume
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName) + `
			resource "gpcn_volume_attachment" "vol1_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol1.id
			}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume_attachment.vol1_attach", plancheck.ResourceActionCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gpcn_volume_attachment.vol1_attach", "id"),
					resource.TestCheckResourceAttrSet("gpcn_volume_attachment.vol1_attach", "virtual_machine_id"),
					resource.TestCheckResourceAttrSet("gpcn_volume_attachment.vol1_attach", "volume_id"),
				),
			},
			// Attach second volume
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName) + `
			resource "gpcn_volume_attachment" "vol1_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol1.id
			}
			resource "gpcn_volume_attachment" "vol2_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol2.id
			  depends_on         = [gpcn_volume_attachment.vol1_attach]
			}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume_attachment.vol2_attach", plancheck.ResourceActionCreate),
					},
				},
			},
			// Detach first volume attachment by removing its attachment resource
			{
				Config: vmBase(sshKeyName, vol1Name, vol2Name, vmName) + `
			resource "gpcn_volume_attachment" "vol2_attach" {
			  virtual_machine_id = gpcn_virtualmachine.test.id
			  volume_id          = gpcn_volume.vm_vol2.id
			}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("gpcn_volume_attachment.vol1_attach", plancheck.ResourceActionDestroy),
					},
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
				Config: providerConfig + dataCenterImagesAndSize() + fmt.Sprintf(`
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

				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id

				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]

				initial_auth = {
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
					resource.TestCheckResourceAttrSet(gpcnVirtualMachineTest, "initial_auth.ssh_key_id"),
					resource.TestCheckResourceAttr(gpcnVirtualMachineTest, "initial_auth.username", "testuser"),
				),
			},
			// Changing initial_auth is a no-op - state is updated with new config values but no API calls are made
			{
				Config: providerConfig + dataCenterImagesAndSize() + fmt.Sprintf(`
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

				size_id  = data.gpcn_virtualmachine_sizes.vm_size.sizes[0].id
				image_id = data.gpcn_virtualmachine_images.vm_image.images[0].id

				allocate_public_ip = false
				network_ids = [
					gpcn_network.vm_network.id
				]

				initial_auth = {
					password = "Test1Password!"
					username = "newuser"
				}
			}
			`, networkName, vmName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(gpcnVirtualMachineTest, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(gpcnVirtualMachineTest, tfjsonpath.New("initial_auth").AtMapKey("username"), knownvalue.StringExact("newuser")),
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
func TestVirtualMachinesMissingSizeId(t *testing.T) {
	config := providerConfig + `
		resource "gpcn_virtualmachine" "test" {
		  name          = "terraform-volume-test-vm"
		  datacenter_id = "any-datacenter-id"
		  image_id           = "eb7da49d-cc71-480a-968d-fbf2841bedf7"
		  allocate_public_ip = false
		  initial_auth = {
		    ssh_key_id = "ssh-key-123"
		    username   = "testuser"
		  }
		}
		`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`The argument "size_id" is required`),
			},
		},
	})
}

func TestVirtualMachinesInvalidAuth(t *testing.T) {
	// vmConfigWithAuth is a helper that builds a minimal VM config with the given auth block content.
	vmConfigWithAuth := func(authBlock string) string {
		return providerConfig + `
		resource "gpcn_virtualmachine" "test" {
		  name          = "terraform-auth-test-vm"
		  datacenter_id = "any-datacenter-id"
		  size_id            = "sku-abc-123"
		  image_id         = "eb7da49d-cc71-480a-968d-fbf2841bedf7"
		  allocate_public_ip = false
		  initial_auth = {
		    username = "testuser"
		    ` + authBlock + `
		  }
		}
		`
	}

	// Password validation is tested directly in internal/virtualmachines/validators_test.go.
	// The cases below test schema-level constraints (framework validators) that require
	// a full provider roundtrip to exercise.
	tests := []struct {
		name      string
		authBlock string
		wantErr   string
	}{
		{
			name:      "ssh_key_id_and_password_conflict",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `password = "Test1Password!"`,
			wantErr:   "cannot be specified when",
		},
		{
			name:      "username_too_short",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "ab"`,
			wantErr:   "string length must be between",
		},
		{
			name:      "username_too_long",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "averylongusernamethatexceedslimit"`,
			wantErr:   "string length must be between",
		},
		{
			name:      "username_starts_with_digit",
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "1testuser"`,
			wantErr:   "Username must start with a letter",
		},
		{
			name:      "username_invalid_chars", // spaces are not allowed
			authBlock: `ssh_key_id = "ssh-key-123"` + "\n" + `username = "test user"`,
			wantErr:   "Username must start with a letter",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      vmConfigWithAuth(tc.authBlock),
						ExpectError: regexp.MustCompile(tc.wantErr),
					},
				},
			})
		})
	}
}
