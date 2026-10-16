package vpcvpnconnections

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	VpcID                types.String `tfsdk:"vpc_id"`
	VpnGatewayID         types.String `tfsdk:"vpn_gateway_id"`
	Name                 types.String `tfsdk:"name"`
	Description          types.String `tfsdk:"description"`
	PeerAddress          types.String `tfsdk:"peer_address"`
	PeerCidrs            types.Set    `tfsdk:"peer_cidrs"`
	Psk                  types.String `tfsdk:"psk"`
	PskWo                types.String `tfsdk:"psk_wo"`
	PskWoVersion         types.Int64  `tfsdk:"psk_wo_version"`
	IkeVersion           types.String `tfsdk:"ike_version"`
	Encryption           types.String `tfsdk:"encryption"`
	Integrity            types.String `tfsdk:"integrity"`
	DhGroup              types.Int64  `tfsdk:"dh_group"`
	PfsGroup             types.Int64  `tfsdk:"pfs_group"`
	IkeLifetimeSeconds   types.Int64  `tfsdk:"ike_lifetime_seconds"`
	IpsecLifetimeSeconds types.Int64  `tfsdk:"ipsec_lifetime_seconds"`
	Dpd                  types.Bool   `tfsdk:"dpd"`
	State                types.String `tfsdk:"state"`
	FailureReason        types.String `tfsdk:"failure_reason"`
	DesiredApplied       types.Bool   `tfsdk:"desired_applied"`
	GatewayKind          types.String `tfsdk:"gateway_kind"`
	LocalEndpointAddress types.String `tfsdk:"local_endpoint_address"`
	LocalNetworks        types.List   `tfsdk:"local_networks"`
}

// optionalString keeps a null as null. An empty string would hide the difference.
func optionalString(raw *string) types.String {
	if raw == nil {
		return types.StringNull()
	}
	return types.StringValue(*raw)
}

// MapConnectionToModel writes the computed attributes from the response. With
// refreshConfigured set (Read), it also writes the configured attributes that
// the API stores, because each reconciles in place. Create and Update leave
// them at the planned values. The pre-shared key attributes never change here,
// because the API never returns the key. psk_wo is always null in state.
func MapConnectionToModel(ctx context.Context, response *ApiVpnConnectionDetail, model ResourceModel, refreshConfigured bool) (ResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	model.ID = types.StringValue(response.ID)
	model.PskWo = types.StringNull()
	model.State = types.StringValue(response.State)
	model.FailureReason = optionalString(response.FailureReason)
	model.DesiredApplied = types.BoolValue(response.DesiredApplied)
	model.GatewayKind = types.StringValue(response.GatewayKind)
	model.LocalEndpointAddress = optionalString(response.LocalEndpointAddress)

	localNetworks, listDiags := types.ListValueFrom(ctx, types.StringType, nonNilStrings(response.ParameterSheet.LocalNetworks))
	diags.Append(listDiags...)
	if listDiags.HasError() {
		localNetworks = types.ListNull(types.StringType)
	}
	model.LocalNetworks = localNetworks

	if !refreshConfigured {
		return model, diags
	}

	model.Name = types.StringValue(response.Name)
	model.Description = types.StringValue("")
	if response.Description != nil {
		model.Description = types.StringValue(*response.Description)
	}
	model.PeerAddress = types.StringValue(response.PeerAddress)
	peerCidrs, setDiags := types.SetValueFrom(ctx, types.StringType, nonNilStrings(response.PeerCidrs))
	diags.Append(setDiags...)
	if setDiags.HasError() {
		peerCidrs = types.SetNull(types.StringType)
	}
	model.PeerCidrs = peerCidrs
	model.IkeVersion = types.StringValue(response.IkeVersion)
	model.Encryption = types.StringValue(response.Encryption)
	model.Integrity = types.StringValue(response.Integrity)
	model.DhGroup = types.Int64Value(response.DhGroup)
	model.PfsGroup = types.Int64Value(response.PfsGroup)
	model.IkeLifetimeSeconds = types.Int64Value(response.IkeLifetimeSeconds)
	model.IpsecLifetimeSeconds = types.Int64Value(response.IpsecLifetimeSeconds)
	model.Dpd = types.BoolValue(response.Dpd)
	return model, diags
}

// ModelFromPlanWithID returns the planned model with the connection ID, for a
// create that found the row but could not read it. The computed attributes the
// plan left unknown become null, because state cannot hold an unknown. The next
// refresh fills them.
func ModelFromPlanWithID(plan ResourceModel, connectionID string) ResourceModel {
	plan.ID = types.StringValue(connectionID)
	plan.PskWo = types.StringNull()
	plan.State = types.StringNull()
	plan.FailureReason = types.StringNull()
	plan.DesiredApplied = types.BoolNull()
	plan.GatewayKind = types.StringNull()
	plan.LocalEndpointAddress = types.StringNull()
	plan.LocalNetworks = types.ListNull(types.StringType)
	return plan
}

// nonNilStrings turns a missing array into an empty one, so the value is known.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func sortedElements(ctx context.Context, set types.Set) ([]string, diag.Diagnostics) {
	var values []string
	diags := set.ElementsAs(ctx, &values, false)
	slices.Sort(values)
	return values, diags
}

// BuildCreateBody returns the POST body: every configured attribute. The
// description is left out when empty, and the key comes from psk or psk_wo.
func BuildCreateBody(ctx context.Context, plan ResourceModel, pskWo string) (map[string]any, diag.Diagnostics) {
	peerCidrs, diags := sortedElements(ctx, plan.PeerCidrs)
	if diags.HasError() {
		return nil, diags
	}

	psk := pskWo
	if !plan.Psk.IsNull() && !plan.Psk.IsUnknown() {
		psk = plan.Psk.ValueString()
	}

	body := map[string]any{
		keyName:                 plan.Name.ValueString(),
		keyPeerAddress:          plan.PeerAddress.ValueString(),
		keyPeerCidrs:            peerCidrs,
		keyPsk:                  psk,
		keyIkeVersion:           plan.IkeVersion.ValueString(),
		keyEncryption:           plan.Encryption.ValueString(),
		keyIntegrity:            plan.Integrity.ValueString(),
		keyDhGroup:              plan.DhGroup.ValueInt64(),
		keyPfsGroup:             plan.PfsGroup.ValueInt64(),
		keyIkeLifetimeSeconds:   plan.IkeLifetimeSeconds.ValueInt64(),
		keyIpsecLifetimeSeconds: plan.IpsecLifetimeSeconds.ValueInt64(),
		keyDpd:                  plan.Dpd.ValueBool(),
	}
	if plan.Description.ValueString() != "" {
		body[keyDescription] = plan.Description.ValueString()
	}
	return body, diags
}

// BuildUpdateBody returns the PUT body: only the keys whose plan differs from
// state. With reapplyAll set, it also carries every key that is not a label,
// because the API saved the values of a failed update before the job ran, so a
// refresh shows no difference but the data center runs the old values. The API
// keeps a key the body omits. An empty description is sent as
// null, because the API clears the field on null. The key goes out when psk
// changes, or when psk_wo_version changes and psk_wo is set. The body is empty
// when no stored attribute changed.
func BuildUpdateBody(ctx context.Context, plan, state ResourceModel, pskWo string, reapplyAll bool) (map[string]any, diag.Diagnostics) {
	body := map[string]any{}
	var diags diag.Diagnostics

	if !plan.Name.Equal(state.Name) {
		body[keyName] = plan.Name.ValueString()
	}
	if !plan.Description.Equal(state.Description) {
		if plan.Description.ValueString() == "" {
			body[keyDescription] = nil
		} else {
			body[keyDescription] = plan.Description.ValueString()
		}
	}
	if reapplyAll || !plan.PeerAddress.Equal(state.PeerAddress) {
		body[keyPeerAddress] = plan.PeerAddress.ValueString()
	}
	if reapplyAll || !plan.PeerCidrs.Equal(state.PeerCidrs) {
		peerCidrs, cidrDiags := sortedElements(ctx, plan.PeerCidrs)
		diags.Append(cidrDiags...)
		body[keyPeerCidrs] = peerCidrs
	}
	if reapplyAll || !plan.IkeVersion.Equal(state.IkeVersion) {
		body[keyIkeVersion] = plan.IkeVersion.ValueString()
	}
	if reapplyAll || !plan.Encryption.Equal(state.Encryption) {
		body[keyEncryption] = plan.Encryption.ValueString()
	}
	if reapplyAll || !plan.Integrity.Equal(state.Integrity) {
		body[keyIntegrity] = plan.Integrity.ValueString()
	}
	if reapplyAll || !plan.DhGroup.Equal(state.DhGroup) {
		body[keyDhGroup] = plan.DhGroup.ValueInt64()
	}
	if reapplyAll || !plan.PfsGroup.Equal(state.PfsGroup) {
		body[keyPfsGroup] = plan.PfsGroup.ValueInt64()
	}
	if reapplyAll || !plan.IkeLifetimeSeconds.Equal(state.IkeLifetimeSeconds) {
		body[keyIkeLifetimeSeconds] = plan.IkeLifetimeSeconds.ValueInt64()
	}
	if reapplyAll || !plan.IpsecLifetimeSeconds.Equal(state.IpsecLifetimeSeconds) {
		body[keyIpsecLifetimeSeconds] = plan.IpsecLifetimeSeconds.ValueInt64()
	}
	if reapplyAll || !plan.Dpd.Equal(state.Dpd) {
		body[keyDpd] = plan.Dpd.ValueBool()
	}

	switch {
	case !plan.Psk.IsNull() && !plan.Psk.Equal(state.Psk):
		body[keyPsk] = plan.Psk.ValueString()
	case !plan.PskWoVersion.IsNull() && !plan.PskWoVersion.Equal(state.PskWoVersion) && pskWo != "":
		body[keyPsk] = pskWo
	}
	return body, diags
}
