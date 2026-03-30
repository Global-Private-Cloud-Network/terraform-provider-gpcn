package provider

import (
	"context"
	"net/url"
	"os"
	"time"

	"terraform-provider-gpcn/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ provider.Provider = &gpcnProvider{}
)

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &gpcnProvider{
			version: version,
		}
	}
}

// gpcnProvider is the provider implementation.
type gpcnProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// Metadata returns the provider type name.
func (p *gpcnProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "gpcn"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *gpcnProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The GPCN provider allows you to manage GPCN cloud infrastructure resources.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Description: "The hostname of the GPCN API. For most users, this is https://api.gpcn.com. This must be set either in the provider configuration block, or as an environment variable exposed via GPCN_HOST",
				Optional:    true,
			},
			"api_key": schema.StringAttribute{
				Description: "API key used for programmatic authentication with the GPCN API. This can be created through the portal, under User Management -> API Keys. This must be set either in the provider configuration block (NOT RECOMMENDED), or as an environment variable exposed via GPCN_API_KEY",
				Optional:    true,
				Sensitive:   true,
			},
			"request_timeout": schema.Int64Attribute{
				Description: "Timeout in seconds for individual HTTP requests to the GPCN API. Defaults to 60 seconds.",
				Optional:    true,
			},
			"polling_timeout": schema.Int64Attribute{
				Description: "Timeout in seconds for async operations (create, update, delete). Defaults to 600 seconds (10 minutes).",
				Optional:    true,
			},
			"max_retries": schema.Int64Attribute{
				Description: "Maximum number of retries for transient failures. Defaults to 3.",
				Optional:    true,
			},
		},
	}
}

type gpcnProviderModel struct {
	Host           types.String `tfsdk:"host"`
	APIKey         types.String `tfsdk:"api_key"`
	RequestTimeout types.Int64  `tfsdk:"request_timeout"`
	PollingTimeout types.Int64  `tfsdk:"polling_timeout"`
	MaxRetries     types.Int64  `tfsdk:"max_retries"`
}

func (p *gpcnProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	tflog.Info(ctx, "Configuring GPCN...")
	// Retrieve provider data from configuration
	var config gpcnProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If practitioner provided a configuration value for any of the
	// attributes, it must be a known value.

	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown GPCN API Host",
			"The provider cannot create the GPCN API client as there is an unknown configuration value for the GPCN API host. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the GPCN_HOST environment variable.",
		)
	}

	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown GPCN API Key",
			"The provider cannot create the GPCN API client as there is an unknown configuration value for the GPCN API key. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the GPCN_API_KEY environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.

	host := os.Getenv("GPCN_HOST")
	apiKey := os.Getenv("GPCN_API_KEY")

	if !config.Host.IsNull() {
		host = config.Host.ValueString()
	}

	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}

	// If any of the expected configurations are missing, return
	// errors with provider-specific guidance.

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing GPCN API Host",
			"The provider cannot create the GPCN API client as there is a missing or empty value for the GPCN API host. "+
				"Set the host value in the configuration or use the GPCN_HOST environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing GPCN API Key",
			"The provider cannot create the GPCN API client as there is a missing or empty value for the GPCN API key. "+
				"Set the api_key value in the configuration or use the GPCN_API_KEY environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	_, err := url.Parse(host)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Host formatted incorrectly",
			"The provider cannot create the GPCN API client as the Host is an improperly formatted URL.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "GPCN successfully configured!")

	ctx = tflog.SetField(ctx, "gpcn_host", host)
	ctx = tflog.SetField(ctx, "gpcn_api_key", apiKey)
	ctx = tflog.MaskFieldValuesWithFieldKeys(ctx, "gpcn_api_key")
	tflog.Debug(ctx, "Creating HTTP Client...")

	// Build client configuration with defaults
	clientConfig := client.DefaultConfig(host, apiKey)

	// Override with user-provided values
	if !config.RequestTimeout.IsNull() {
		clientConfig.RequestTimeout = time.Duration(config.RequestTimeout.ValueInt64()) * time.Second
	}
	if !config.PollingTimeout.IsNull() {
		clientConfig.PollingTimeout = time.Duration(config.PollingTimeout.ValueInt64()) * time.Second
	}
	if !config.MaxRetries.IsNull() {
		clientConfig.MaxRetries = int(config.MaxRetries.ValueInt64())
	}

	tflog.Debug(ctx, "Client configuration",
		map[string]any{
			"request_timeout_seconds": clientConfig.RequestTimeout.Seconds(),
			"polling_timeout_seconds": clientConfig.PollingTimeout.Seconds(),
			"max_retries":             clientConfig.MaxRetries,
		})

	gpcnClient, err := client.NewGpcnClient(clientConfig)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create GPCN client", err.Error(),
		)
		return
	}

	// Pass the full GpcnClient so resources can access both HTTP client and config
	resp.DataSourceData = gpcnClient
	resp.ResourceData = gpcnClient
	tflog.Debug(ctx, "GPCN client successfully created. Provider online")
}

// DataSources defines the data sources implemented in the provider.
func (p *gpcnProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDatacenterDataSource,
	}
}

// Resources defines the resources implemented in the provider.
func (p *gpcnProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewGPUResource,
		NewNetworksResource,
		NewResourceGroupResource,
		NewSSHKeyResource,
		NewVirtualMachinesResource,
		NewVolumesResource,
	}
}
