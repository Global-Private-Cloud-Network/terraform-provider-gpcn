package virtualmachines

var BASE_URL_V1 string = "/v1/resource/virtual-machines/"
var DATA_CENTERS_BASE_URL_V1 = "/v1/resource/data-centers/"
var MAX_NETWORKS_ATTACHED_ALLOWED int = 5
var MAX_VOLUMES_ATTACHED_ALLOWED int = 5
var DEFAULT_NETWORK_TIMEOUT_SECONDS int = 300

// Virtual Machine lifecycle statuses
const (
	Running string = "Running"
	Shutoff string = "Shutoff"
)

// Virtual Machine size tiers in ascending order (smallest to largest)
var (
	// General purpose tiers (g- prefix) in ascending order by size
	GeneralTiers = []string{"g-micro-1", "g-small-1", "g-medium-1", "g-large-1", "g-xl-1"}

	// Memory optimized tiers (m- prefix) in ascending order by size
	MemoryTiers = []string{"m-micro-1", "m-small-1", "m-medium-1", "m-large-1", "m-xl-1"}

	// AllTiers combines all tiers for validation purposes
	AllTiers = append(append([]string{}, GeneralTiers...), MemoryTiers...)
)

// Virtual Machine categories
const (
	CategoryGeneral = "general"
	CategoryMemory  = "memory"
)

// Valid virtual machine image names
var ValidImageNames = []string{
	"Ubuntu 20.04 LTS",
	"NetBSD 10.x",
	"OpenSUSE Leap 15.x JeOS. Cloud",
	"OpenSUSE Leap 15.x Minimal VM. Cloud",
	"OPNSense 25.x",
	"PFSense CE 2.7.2",
	"Rocky 8.x",
	"Rocky 9.x",
	"Rocky 10.x",
	"Ubuntu 18.04 LTS",
	"Gentoo",
	"Ubuntu 22.04 LTS",
	"Ubuntu 24.04 LTS",
	"Ubuntu 25.04",
	"Windows 2012 Standard",
	"Windows 2016 Standard",
	"Windows 2019 Standard",
	"Windows 2022 Standard",
	"Windows 2025 Standard",
	"Windows 11 Pro (BYOL)",
	"Cloudlinux 9.5",
	"Alma Linux 9.x",
	"Alma Linux 10.x",
	"Alpine 3.x",
	"Arch Linux Cloudimg",
	"CentOS Stream 10.x",
	"CentOS Stream 8.x",
	"CentOS Stream 9.x",
	"Cirros 0.6.2",
	"Cirros 0.6.3",
	"Alma Linux 8.x",
	"Cloudlinux 9.5 Cpanel",
	"Coriolis Appliance",
	"Debian 10.x",
	"Debian 11.x",
	"Debian 12.x",
	"Debian 13.x",
	"Fedora CoreOS k8saas",
	"Fedora Generic 42.x",
	"FreeBSD 14.x",
}
