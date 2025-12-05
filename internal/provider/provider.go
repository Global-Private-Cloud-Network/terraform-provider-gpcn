package provider

import (
	"context"
	"net/url"
	"os"

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
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional: true,
			},
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

type gpcnProviderModel struct {
	Host   types.String `tfsdk:"host"`
	APIKey types.String `tfsdk:"api_key"`
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

	httpClient, err := client.NewHttpClient(host, apiKey)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create new HTTP Client", err.Error(),
		)
	}

	resp.DataSourceData = httpClient
	resp.ResourceData = httpClient
	tflog.Debug(ctx, "HTTP Client successfully created. GPCN provider online")
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
		NewNetworksResource,
		NewVolumesResource,
		NewVirtualMachinesResource,
	}
}
