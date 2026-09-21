package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/datacenters"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func NewDatacenterDataSource() datasource.DataSource {
	return &datacenterDataSource{}
}

type datacenterDataSource struct {
	client *client.GpcnClient
}

type datacenterDataSourceModel struct {
	Name         types.String `tfsdk:"name"`
	CountryName  types.String `tfsdk:"country_name"`
	RegionName   types.String `tfsdk:"region_name"`
	GPUEnabled   types.Bool   `tfsdk:"gpu_enabled"`
	CustomImages types.Bool   `tfsdk:"custom_images"`
	DataCenters  types.List   `tfsdk:"datacenters"`
}

type datacenterRow struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Code                string `json:"code"`
	RegionID            int64  `json:"regionId"`
	RegionName          string `json:"regionName"`
	CountryID           string `json:"countryId"`
	CountryName         string `json:"countryName"`
	CountryAbbreviation string `json:"countryAbbreviation"`
	ContinentCode       string `json:"continentCode"`
	ContinentName       string `json:"continentName"`
	GPUEnabled          bool   `json:"gpuEnabled"`
	CustomImages        bool   `json:"customImages"`
}

type datacenterMeta struct {
	TotalPages int `json:"totalPages"`
}

type datacenterResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    []datacenterRow `json:"data"`
	Meta    datacenterMeta  `json:"meta"`
}

type datacenterDataResponseTF struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	Code                types.String `tfsdk:"code"`
	RegionID            types.Int64  `tfsdk:"region_id"`
	RegionName          types.String `tfsdk:"region_name"`
	CountryID           types.String `tfsdk:"country_id"`
	CountryName         types.String `tfsdk:"country_name"`
	CountryAbbreviation types.String `tfsdk:"country_abbreviation"`
	ContinentCode       types.String `tfsdk:"continent_code"`
	ContinentName       types.String `tfsdk:"continent_name"`
	GPUEnabled          types.Bool   `tfsdk:"gpu_enabled"`
	CustomImages        types.Bool   `tfsdk:"custom_images"`
}

func (o datacenterDataResponseTF) AttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                   types.StringType,
		"name":                 types.StringType,
		"code":                 types.StringType,
		"region_id":            types.Int64Type,
		"region_name":          types.StringType,
		"country_id":           types.StringType,
		"country_name":         types.StringType,
		"country_abbreviation": types.StringType,
		"continent_code":       types.StringType,
		"continent_name":       types.StringType,
		"gpu_enabled":          types.BoolType,
		"custom_images":        types.BoolType,
	}
}

func (d *datacenterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datacenters"
}

func (d *datacenterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves information about available GPCN datacenters. Use this data source to filter and find datacenters by name, country, or region.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter datacenters by name",
			},
			"country_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter datacenters by country name (e.g., 'United States', 'Canada').",
			},
			"region_name": schema.StringAttribute{
				Optional:    true,
				Description: "Filter datacenters by region name within a country. (e.g., 'East', 'West', 'Central')",
			},
			"gpu_enabled": schema.BoolAttribute{
				Optional:    true,
				Description: "Filter datacenters to only those with GPU support enabled.",
			},
			"custom_images": schema.BoolAttribute{
				Optional:    true,
				Description: "Filter datacenters to only those that support custom images.",
			},
			"datacenters": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of datacenters matching the specified filter criteria.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier of the datacenter.",
						},
						"name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the datacenter.",
						},
						"code": schema.StringAttribute{
							Computed:    true,
							Description: "Short code that identifies the datacenter.",
						},
						"region_id": schema.Int64Attribute{
							Computed:    true,
							Description: "Numeric identifier of the region where the datacenter is located.",
						},
						"region_name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the region where the datacenter is located.",
						},
						"country_id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier of the country where the datacenter is located.",
						},
						"country_name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the country where the datacenter is located.",
						},
						"country_abbreviation": schema.StringAttribute{
							Computed:    true,
							Description: "Two-letter country code abbreviation (e.g., 'US').",
						},
						"continent_code": schema.StringAttribute{
							Computed:    true,
							Description: "Code of the continent where the datacenter is located (e.g., 'NA').",
						},
						"continent_name": schema.StringAttribute{
							Computed:    true,
							Description: "Name of the continent where the datacenter is located.",
						},
						"gpu_enabled": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether GPU resources are available in this datacenter.",
						},
						"custom_images": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether custom images are supported in this datacenter.",
						}},
				},
			},
		},
	}
}

func (d *datacenterDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	gpcnClient, ok := req.ProviderData.(*client.GpcnClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.GpcnClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = gpcnClient
}

func (d *datacenterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// Add correlation ID for request tracing
	ctx = client.WithCorrelationID(ctx)

	var state datacenterDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Construct request URL from values we have available
	var otherFilters string
	if !state.CountryName.IsNull() {
		otherFilters += "&countryName=" + url.QueryEscape(state.CountryName.ValueString())
	}
	if !state.RegionName.IsNull() {
		otherFilters += "&regionName=" + url.QueryEscape(state.RegionName.ValueString())
	}
	if !state.Name.IsNull() {
		otherFilters += "&search=" + url.QueryEscape(state.Name.ValueString())
	}

	query := otherFilters
	if !state.GPUEnabled.IsNull() {
		query += "&gpuEnabled=" + strconv.FormatBool(state.GPUEnabled.ValueBool())
	}

	rows, truncated, err := d.getDatacenters(ctx, query)
	if err != nil {
		// Big failure, no helpful error message
		resp.Diagnostics.AddError(
			datacenters.ErrSummaryUnableGetDatacenters,
			err.Error(),
		)
		return
	}
	if truncated {
		resp.Diagnostics.Append(datacenterTruncationWarning())
	}

	// The list endpoint has no filter for custom images, so the filter stays here.
	if !state.CustomImages.IsNull() {
		want := state.CustomImages.ValueBool()
		unfiltered := rows
		filtered := unfiltered[:0]
		for _, dc := range unfiltered {
			if dc.CustomImages == want {
				filtered = append(filtered, dc)
			}
		}
		rows = filtered
		if len(unfiltered) > 0 && len(filtered) == 0 {
			resp.Diagnostics.AddError(
				datacenters.ErrSummaryUnableGetDatacenters,
				fmt.Sprintf(datacenters.ErrDetailDatacenterNoCustomImages, want),
			)
			return
		}
	}

	if len(rows) < 1 {
		d.addNoMatchError(ctx, state, otherFilters, resp)
		return
	}

	var datacenters []datacenterDataResponseTF
	for _, datacenter := range rows {
		datacenters = append(datacenters, datacenterDataResponseTF{
			ID:                  types.StringValue(datacenter.ID),
			Name:                types.StringValue(datacenter.Name),
			Code:                types.StringValue(datacenter.Code),
			RegionID:            types.Int64Value(datacenter.RegionID),
			RegionName:          types.StringValue(datacenter.RegionName),
			CountryID:           types.StringValue(datacenter.CountryID),
			CountryName:         types.StringValue(datacenter.CountryName),
			CountryAbbreviation: types.StringValue(datacenter.CountryAbbreviation),
			ContinentCode:       types.StringValue(datacenter.ContinentCode),
			ContinentName:       types.StringValue(datacenter.ContinentName),
			GPUEnabled:          types.BoolValue(datacenter.GPUEnabled),
			CustomImages:        types.BoolValue(datacenter.CustomImages),
		})
	}

	var listDiags diag.Diagnostics
	state.DataCenters, listDiags = types.ListValueFrom(ctx, types.ObjectType{AttrTypes: datacenterDataResponseTF{}.AttrTypes()}, datacenters)
	if listDiags.HasError() {
		resp.Diagnostics.Append(listDiags...)
		return
	}

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// addNoMatchError explains an empty result. The API serves no region route and
// no country route, so the suggestion comes from one unfiltered list call.
func (d *datacenterDataSource) addNoMatchError(ctx context.Context, state datacenterDataSourceModel, otherFilters string, resp *datasource.ReadResponse) {
	// The suggestion ends in an error, so a truncated list needs no warning.
	rows, _, err := d.getDatacenters(ctx, "")
	if err != nil {
		resp.Diagnostics.AddError(
			datacenters.ErrSummaryUnableGetDatacenters,
			err.Error(),
		)
		return
	}

	if len(rows) < 1 {
		resp.Diagnostics.AddError(
			datacenters.ErrSummaryUnableGetDatacenters,
			datacenters.ErrDetailDatacenterNoneVisible,
		)
		return
	}

	if !state.GPUEnabled.IsNull() {
		// The server applies gpuEnabled, so the same filters without it separate the
		// two causes. Rows here mean gpu_enabled is the one value that matches none.
		matched := rows
		if otherFilters != "" {
			matched, _, err = d.getDatacenters(ctx, otherFilters)
			if err != nil {
				resp.Diagnostics.AddError(
					datacenters.ErrSummaryUnableGetDatacenters,
					err.Error(),
				)
				return
			}
		}
		if len(matched) > 0 {
			resp.Diagnostics.AddError(
				datacenters.ErrSummaryUnableGetDatacenters,
				fmt.Sprintf(datacenters.ErrDetailDatacenterNoGPUEnabled, state.GPUEnabled.ValueBool()),
			)
			return
		}
	}

	var countryAndRegion []string
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		pair := row.CountryName + " - " + row.RegionName
		if _, found := seen[pair]; found {
			continue
		}
		seen[pair] = struct{}{}
		countryAndRegion = append(countryAndRegion, pair)
	}

	countryAndRegionFormatted := strings.Join(countryAndRegion, ", ")
	resp.Diagnostics.AddError(
		datacenters.ErrSummaryUnableGetDatacenters,
		fmt.Sprintf(datacenters.ErrDetailDatacenterNotFound, countryAndRegionFormatted),
	)
}

// The provider must not read an unbounded list. The page size keeps each
// request small, and the page cap bounds the whole read.
const (
	datacenterPageLimit = 100
	datacenterPageCap   = 100
)

// datacenterTruncationWarning tells the operator that the page cap truncates the
// list. A datacenter beyond the cap is absent.
func datacenterTruncationWarning() diag.Diagnostic {
	return diag.NewWarningDiagnostic(
		datacenters.WarnSummaryDatacenterListTruncated,
		fmt.Sprintf(datacenters.WarnDetailDatacenterListTruncated, datacenterPageCap*datacenterPageLimit),
	)
}

// One page holds at most 100 rows, so a filter that matches more than one page
// needs every page. The second return value reports a list that the cap
// truncates.
func (d *datacenterDataSource) getDatacenters(ctx context.Context, queryString string) ([]datacenterRow, bool, error) {
	var rows []datacenterRow
	truncated := false

	for page, totalPages := 1, 1; page <= totalPages; page++ {
		response, err := d.getDatacenterPage(ctx, page, queryString)
		if err != nil {
			return nil, false, err
		}

		rows = append(rows, response.Data...)
		// The first answer bounds the loop. A later page cannot extend it.
		if page == 1 && response.Meta.TotalPages > totalPages {
			totalPages = response.Meta.TotalPages
			if totalPages > datacenterPageCap {
				totalPages = datacenterPageCap
				truncated = true
			}
		}
	}

	return rows, truncated, nil
}

func (d *datacenterDataSource) getDatacenterPage(ctx context.Context, page int, queryString string) (*datacenterResponse, error) {
	datacenterUrl := fmt.Sprintf("%s?page=%d&limit=%d", datacenters.BASE_URL_V1, page, datacenterPageLimit)
	request, err := http.NewRequestWithContext(ctx, "GET", datacenterUrl+queryString, nil)
	if err != nil {
		return nil, err
	}

	response, err := d.client.DoWithRetry(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var dcResp datacenterResponse
	err = json.Unmarshal(body, &dcResp)

	if err != nil {
		return nil, err
	}
	return &dcResp, nil
}
