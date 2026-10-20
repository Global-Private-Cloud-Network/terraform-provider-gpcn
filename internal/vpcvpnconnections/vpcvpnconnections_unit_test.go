package vpcvpnconnections

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
	"terraform-provider-gpcn/internal/vpcvpngateways"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	testVpcID  = "11111111-1111-4111-8111-111111111111"
	testConnID = "33333333-3333-4333-8333-333333333333"
	testPsk    = "Tr0ub4dor&3-horse-staple"
	testPskTwo = "Zx9!correct-battery-wire"
)

func ptr(value string) *string { return &value }

func cidrs(values ...string) types.Set {
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	return types.SetValueMust(types.StringType, elements)
}

func baseModel() ResourceModel {
	return ResourceModel{
		ID: types.StringValue(testConnID), VpcID: types.StringValue(testVpcID), VpnGatewayID: types.StringValue("gw"),
		Name: types.StringValue("branch-office"), Description: types.StringValue(""),
		PeerAddress: types.StringValue("203.0.113.10"), PeerCidrs: cidrs("192.168.10.0/24"),
		Psk: types.StringNull(), PskWo: types.StringNull(), PskWoVersion: types.Int64Value(1),
		IkeVersion: types.StringValue("v2"), Encryption: types.StringValue("aes256"), Integrity: types.StringValue("sha256"),
		DhGroup: types.Int64Value(14), PfsGroup: types.Int64Value(14),
		IkeLifetimeSeconds: types.Int64Value(28800), IpsecLifetimeSeconds: types.Int64Value(3600), Dpd: types.BoolValue(true),
	}
}

func sortedKeys(body map[string]any) []string {
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestVpcVpnConnectionUnitBuildUpdateBodyEachAttributeAlone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*ResourceModel)
		key    string
		value  any
	}{
		{"name", func(m *ResourceModel) { m.Name = types.StringValue("renamed") }, "name", "renamed"},
		{"description set", func(m *ResourceModel) { m.Description = types.StringValue("Chicago") }, "description", "Chicago"},
		{"peer_address", func(m *ResourceModel) { m.PeerAddress = types.StringValue("203.0.113.11") }, "peerAddress", "203.0.113.11"},
		{"peer_cidrs", func(m *ResourceModel) { m.PeerCidrs = cidrs("192.168.20.0/24", "192.168.10.0/24") }, "peerCidrs", []string{"192.168.10.0/24", "192.168.20.0/24"}},
		{"ike_version", func(m *ResourceModel) { m.IkeVersion = types.StringValue("v1") }, "ikeVersion", "v1"},
		{"encryption", func(m *ResourceModel) { m.Encryption = types.StringValue("aes128") }, "encryption", "aes128"},
		{"integrity", func(m *ResourceModel) { m.Integrity = types.StringValue("sha512") }, "integrity", "sha512"},
		{"dh_group", func(m *ResourceModel) { m.DhGroup = types.Int64Value(5) }, "dhGroup", int64(5)},
		{"pfs_group", func(m *ResourceModel) { m.PfsGroup = types.Int64Value(2) }, "pfsGroup", int64(2)},
		{"ike_lifetime_seconds", func(m *ResourceModel) { m.IkeLifetimeSeconds = types.Int64Value(60) }, "ikeLifetimeSeconds", int64(60)},
		{"ipsec_lifetime_seconds", func(m *ResourceModel) { m.IpsecLifetimeSeconds = types.Int64Value(86400) }, "ipsecLifetimeSeconds", int64(86400)},
		{"dpd", func(m *ResourceModel) { m.Dpd = types.BoolValue(false) }, "dpd", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state, plan := baseModel(), baseModel()
			tc.mutate(&plan)

			body, diags := BuildUpdateBody(context.Background(), plan, state, "", false)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got := sortedKeys(body); !reflect.DeepEqual(got, []string{tc.key}) {
				t.Fatalf("Expected only %s, got %v", tc.key, got)
			}
			if !reflect.DeepEqual(body[tc.key], tc.value) {
				t.Errorf("Expected %s = %#v, got %#v", tc.key, tc.value, body[tc.key])
			}
		})
	}
}

func TestVpcVpnConnectionUnitBuildUpdateBodyClearedDescriptionSendsNull(t *testing.T) {
	t.Parallel()
	state, plan := baseModel(), baseModel()
	state.Description = types.StringValue("Chicago")

	body, _ := BuildUpdateBody(context.Background(), plan, state, "", false)

	value, found := body["description"]
	if len(body) != 1 || !found || value != nil {
		t.Errorf("Expected description null alone, got %#v", body)
	}
}

func TestVpcVpnConnectionUnitBuildUpdateBodyPskRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		state   func(*ResourceModel)
		plan    func(*ResourceModel)
		pskWo   string
		wantPsk any
	}{
		{"psk changed", func(m *ResourceModel) { m.Psk = types.StringValue(testPsk) }, func(m *ResourceModel) { m.Psk = types.StringValue(testPskTwo) }, "", testPskTwo},
		{"psk unchanged", func(m *ResourceModel) { m.Psk = types.StringValue(testPsk) }, func(m *ResourceModel) { m.Psk = types.StringValue(testPsk) }, "", nil},
		{"psk_wo_version raised", func(*ResourceModel) {}, func(m *ResourceModel) { m.PskWoVersion = types.Int64Value(2) }, testPskTwo, testPskTwo},
		{"psk_wo value changed, version same", func(*ResourceModel) {}, func(*ResourceModel) {}, testPskTwo, nil},
		{"psk_wo_version raised, no psk_wo value", func(*ResourceModel) {}, func(m *ResourceModel) { m.PskWoVersion = types.Int64Value(2) }, "", nil},
		{"switch from psk to psk_wo", func(m *ResourceModel) { m.Psk = types.StringValue(testPsk); m.PskWoVersion = types.Int64Null() }, func(m *ResourceModel) { m.Psk = types.StringNull(); m.PskWoVersion = types.Int64Value(1) }, testPskTwo, testPskTwo},
		{"switch from psk_wo to psk", func(*ResourceModel) {}, func(m *ResourceModel) { m.Psk = types.StringValue(testPsk); m.PskWoVersion = types.Int64Null() }, "", testPsk},
		{"import then first apply with psk", func(m *ResourceModel) { m.PskWoVersion = types.Int64Null() }, func(m *ResourceModel) { m.Psk = types.StringValue(testPsk); m.PskWoVersion = types.Int64Null() }, "", testPsk},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state, plan := baseModel(), baseModel()
			tc.state(&state)
			tc.plan(&plan)

			body, _ := BuildUpdateBody(context.Background(), plan, state, tc.pskWo, false)

			if tc.wantPsk == nil {
				if len(body) != 0 {
					t.Fatalf("Expected no PUT body, got %v", sortedKeys(body))
				}
				return
			}
			if got := sortedKeys(body); !reflect.DeepEqual(got, []string{"psk"}) {
				t.Fatalf("Expected only psk, got %v", got)
			}
			if body["psk"] != tc.wantPsk {
				t.Errorf("Expected psk %q, got %q", tc.wantPsk, body["psk"])
			}
		})
	}
}

func TestVpcVpnConnectionUnitBuildUpdateBodyNothingChangedIsEmpty(t *testing.T) {
	t.Parallel()
	body, diags := BuildUpdateBody(context.Background(), baseModel(), baseModel(), "", false)
	if diags.HasError() || len(body) != 0 {
		t.Errorf("Expected an empty body, got %v (%v)", body, diags)
	}
}

func TestVpcVpnConnectionUnitBuildCreateBodyCarriesEveryKey(t *testing.T) {
	t.Parallel()
	plan := baseModel()
	plan.PeerCidrs = cidrs("192.168.20.0/24", "192.168.10.0/24")

	body, _ := BuildCreateBody(context.Background(), plan, testPsk)

	want := []string{"dhGroup", "dpd", "encryption", "ikeLifetimeSeconds", "ikeVersion", "integrity", "ipsecLifetimeSeconds", "name", "peerAddress", "peerCidrs", "pfsGroup", "psk"}
	if got := sortedKeys(body); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected keys %v, got %v", want, got)
	}
	if body["psk"] != testPsk {
		t.Errorf("Expected psk_wo as psk, got %v", body["psk"])
	}
	if !reflect.DeepEqual(body["peerCidrs"], []string{"192.168.10.0/24", "192.168.20.0/24"}) {
		t.Errorf("Expected sorted peerCidrs, got %v", body["peerCidrs"])
	}

	plan.Description = types.StringValue("Chicago")
	plan.Psk = types.StringValue(testPskTwo)
	body, _ = BuildCreateBody(context.Background(), plan, "")
	if body["description"] != "Chicago" || body["psk"] != testPskTwo {
		t.Errorf("Expected the description and psk, got %v", body)
	}
}

func TestVpcVpnConnectionUnitMapConnectionToModel(t *testing.T) {
	t.Parallel()
	response := &ApiVpnConnectionDetail{
		ApiVpnConnection: ApiVpnConnection{
			ID: testConnID, Name: "api-name", Description: ptr("api description"), State: StateConnected,
			GatewayKind: "appliance", LocalEndpointAddress: ptr("198.51.100.7"), PeerAddress: "203.0.113.99",
			PeerCidrs: []string{"10.9.0.0/16"}, IkeVersion: "v1", Encryption: "aes128", Integrity: "sha512",
			DhGroup: 5, PfsGroup: 2, IkeLifetimeSeconds: 60, IpsecLifetimeSeconds: 120, Dpd: false, DesiredApplied: false,
		},
		ParameterSheet: ApiParameterSheet{LocalNetworks: []string{"10.121.1.0/24"}},
	}
	prior := baseModel()
	prior.Psk = types.StringValue(testPsk)
	prior.PskWo = types.StringValue("leftover")

	t.Run("read refreshes the stored attributes", func(t *testing.T) {
		t.Parallel()
		model, diags := MapConnectionToModel(context.Background(), response, prior, true)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		checks := map[string][2]any{
			"name":                   {model.Name.ValueString(), "api-name"},
			"description":            {model.Description.ValueString(), "api description"},
			"peer_address":           {model.PeerAddress.ValueString(), "203.0.113.99"},
			"ike_version":            {model.IkeVersion.ValueString(), "v1"},
			"encryption":             {model.Encryption.ValueString(), "aes128"},
			"integrity":              {model.Integrity.ValueString(), "sha512"},
			"dh_group":               {model.DhGroup.ValueInt64(), int64(5)},
			"pfs_group":              {model.PfsGroup.ValueInt64(), int64(2)},
			"ike_lifetime_seconds":   {model.IkeLifetimeSeconds.ValueInt64(), int64(60)},
			"ipsec_lifetime_seconds": {model.IpsecLifetimeSeconds.ValueInt64(), int64(120)},
			"dpd":                    {model.Dpd.ValueBool(), false},
			"state":                  {model.State.ValueString(), "connected"},
			"desired_applied":        {model.DesiredApplied.ValueBool(), false},
			"gateway_kind":           {model.GatewayKind.ValueString(), "appliance"},
			"local_endpoint_address": {model.LocalEndpointAddress.ValueString(), "198.51.100.7"},
		}
		for attribute, pair := range checks {
			if pair[0] != pair[1] {
				t.Errorf("%s: expected %v, got %v", attribute, pair[1], pair[0])
			}
		}
		if !model.PeerCidrs.Equal(cidrs("10.9.0.0/16")) {
			t.Errorf("Expected the API peer_cidrs, got %v", model.PeerCidrs)
		}
		if !model.FailureReason.IsNull() {
			t.Errorf("Expected a null failure_reason, got %v", model.FailureReason)
		}
		if got := model.LocalNetworks.Elements(); len(got) != 1 || got[0].String() != `"10.121.1.0/24"` {
			t.Errorf("Expected the parameter sheet networks, got %v", got)
		}
		if model.Psk.ValueString() != testPsk || !model.PskWoVersion.Equal(types.Int64Value(1)) {
			t.Errorf("Expected psk and psk_wo_version to keep their values, got %v and %v", model.Psk, model.PskWoVersion)
		}
		if !model.PskWo.IsNull() {
			t.Errorf("Expected a null psk_wo, got %v", model.PskWo)
		}
	})

	t.Run("create and update keep the planned stored attributes", func(t *testing.T) {
		t.Parallel()
		model, _ := MapConnectionToModel(context.Background(), response, prior, false)
		if model.Name.ValueString() != "branch-office" || model.PeerAddress.ValueString() != "203.0.113.10" || !model.PeerCidrs.Equal(prior.PeerCidrs) {
			t.Errorf("Expected the planned values, got %v %v %v", model.Name, model.PeerAddress, model.PeerCidrs)
		}
		if model.State.ValueString() != "connected" || model.GatewayKind.ValueString() != "appliance" {
			t.Errorf("Expected the computed attributes from the API, got %v %v", model.State, model.GatewayKind)
		}
	})

	t.Run("a null description reads as empty", func(t *testing.T) {
		t.Parallel()
		nullDescription := *response
		nullDescription.Description = nil
		model, _ := MapConnectionToModel(context.Background(), &nullDescription, prior, true)
		if model.Description.IsNull() || model.Description.ValueString() != "" {
			t.Errorf("Expected an empty description, got %v", model.Description)
		}
	})

	t.Run("a missing parameter sheet reads as an empty list", func(t *testing.T) {
		t.Parallel()
		bare := *response
		bare.ParameterSheet = ApiParameterSheet{}
		model, _ := MapConnectionToModel(context.Background(), &bare, prior, true)
		if model.LocalNetworks.IsNull() || len(model.LocalNetworks.Elements()) != 0 {
			t.Errorf("Expected an empty known list, got %v", model.LocalNetworks)
		}
	})
}

func runStringValidator(v validator.String, value types.String) (summary, detail string, failed bool) {
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("psk"), ConfigValue: value}, resp)
	if !resp.Diagnostics.HasError() {
		return "", "", false
	}
	return resp.Diagnostics[0].Summary(), resp.Diagnostics[0].Detail(), true
}

func TestVpcVpnConnectionUnitPskValidator(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"16 chars", strings.Repeat("a", 16), true},
		{"64 chars", strings.Repeat("a", 64), true},
		{"15 chars", strings.Repeat("a", 15), false},
		{"65 chars", strings.Repeat("a", 65), false},
		{"empty", "", false},
		{"space", "aaaaaaaa aaaaaaaa", false},
		{"double quote", `aaaaaaaa"aaaaaaaa`, false},
		{"backslash", `aaaaaaaa\aaaaaaaa`, false},
		{"tilde and bang", "!aaaaaaaaaaaaaa~", true},
		{"single quote and hash", "#'aaaaaaaaaaaaaa", true},
		{"slash and bracket", "/]aaaaaaaaaaaaa[", true},
		{"control character", "aaaaaaaa\x1faaaaaaaa", false},
		{"non-ASCII", "aaaaaaaaéaaaaaaaa", false},
		{"newline", "aaaaaaaa\naaaaaaaa", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary, detail, failed := runStringValidator(PskValidator{Attribute: "psk"}, types.StringValue(tc.value))
			if failed == tc.ok {
				t.Fatalf("Expected ok=%v for %q, got failed=%v", tc.ok, tc.value, failed)
			}
			if failed {
				const want = "psk must be 16–64 printable ASCII characters with no spaces, double quotes or backslashes"
				if detail != want || summary != "Invalid pre-shared key" {
					t.Errorf("Expected %q / %q, got %q / %q", "Invalid pre-shared key", want, summary, detail)
				}
				if tc.value != "" && strings.Contains(detail+summary, tc.value) {
					t.Errorf("Expected the diagnostic to omit the key")
				}
			}
		})
	}
}

func TestVpcVpnConnectionUnitNamePattern(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"a": true, "branch-office": true, "Branch Office 2": true, "a.b-c": true, "A1": true,
		"-a": false, "a-": false, " a": false, "a ": false, "a_b": false, "a/b": false, "": false, ".a": false,
	}
	for name, want := range cases {
		if got := NamePattern.MatchString(name); got != want {
			t.Errorf("name %q: expected %v, got %v", name, want, got)
		}
	}
}

func TestVpcVpnConnectionUnitMultipleOfValidator(t *testing.T) {
	t.Parallel()
	v := MultipleOfValidator{Attribute: "ike_lifetime_seconds", Step: LifetimeStep}
	for value, ok := range map[int64]bool{60: true, 120: true, 28800: true, 86400: true, 61: false, 90: false, 3599: false} {
		resp := &validator.Int64Response{}
		v.ValidateInt64(context.Background(), validator.Int64Request{Path: path.Root("x"), ConfigValue: types.Int64Value(value)}, resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("value %d: expected ok=%v, got diagnostics %v", value, ok, resp.Diagnostics)
		}
	}
	resp := &validator.Int64Response{}
	v.ValidateInt64(context.Background(), validator.Int64Request{ConfigValue: types.Int64Unknown()}, resp)
	v.ValidateInt64(context.Background(), validator.Int64Request{ConfigValue: types.Int64Null()}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("Expected null and unknown to pass, got %v", resp.Diagnostics)
	}
}

func TestVpcVpnConnectionUnitUtf16LengthAtMostValidator(t *testing.T) {
	t.Parallel()
	v := Utf16LengthAtMostValidator{Attribute: "description", Max: DescriptionMax}
	for name, test := range map[string]struct {
		value string
		ok    bool
	}{
		"300 accented letters, 300 units": {strings.Repeat("é", 300), true},
		"500 ASCII letters":               {strings.Repeat("a", DescriptionMax), true},
		"501 ASCII letters":               {strings.Repeat("a", DescriptionMax+1), false},
		"250 emoji, 500 units":            {strings.Repeat("😀", 250), true},
		"251 emoji, 502 units":            {strings.Repeat("😀", 251), false},
	} {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("description"), ConfigValue: types.StringValue(test.value)}, resp)
		if resp.Diagnostics.HasError() == test.ok {
			t.Errorf("%s: expected ok=%v, got diagnostics %v", name, test.ok, resp.Diagnostics)
		}
	}

	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("description"), ConfigValue: types.StringValue(strings.Repeat("😀", 251))}, resp)
	const want = "Attribute description must be at most 500 characters (counted in UTF-16 code units), got: 502"
	if resp.Diagnostics.HasError() && resp.Diagnostics.Errors()[0].Detail() != want {
		t.Errorf("Expected the detail %q, got %q", want, resp.Diagnostics.Errors()[0].Detail())
	}

	resp = &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringUnknown()}, resp)
	v.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringNull()}, resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("Expected null and unknown to pass, got %v", resp.Diagnostics)
	}
}

func apiServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *client.GpcnClient {
	t.Helper()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{T: t, Handler: handler})
	return gpcnClient
}

func TestVpcVpnConnectionUnitGetVpnConnectionMapsNotFoundToSentinel(t *testing.T) {
	t.Parallel()
	gpcnClient := apiServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"success":false,"message":"VPN connection not found","error":{"code":"Resource Not Found","statusCode":404,"details":null}}`)
	})

	_, err := GetVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID)
	if !errors.Is(err, ErrVpnConnectionNotFound) {
		t.Fatalf("Expected ErrVpnConnectionNotFound, got %v", err)
	}
	if !client.IsNotFound(err) {
		t.Errorf("Expected the API error to stay in the chain, got %v", err)
	}
}

func TestVpcVpnConnectionUnitGetVpnConnectionReadsParameterSheet(t *testing.T) {
	t.Parallel()
	gpcnClient := apiServer(t, func(w http.ResponseWriter, _ *http.Request) {
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "message": "", "meta": nil, "data": map[string]any{
			"id": testConnID, "name": "x", "state": "connected", "peerCidrs": []string{"10.9.0.0/16"}, "dhGroup": 14,
			"description": nil, "failureReason": nil, "activeJobId": nil,
			"parameterSheet": map[string]any{"localNetworks": []string{"10.121.1.0/24"}, "dhGroup": 14},
		}})
	})

	detail, err := GetVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(detail.ParameterSheet.LocalNetworks, []string{"10.121.1.0/24"}) || detail.DhGroup != 14 || detail.Description != nil {
		t.Errorf("Unexpected detail %+v", detail)
	}
}

func TestVpcVpnConnectionUnitCreateVpnConnectionReturnsIDWhenTheJobFails(t *testing.T) {
	t.Parallel()
	gpcnClient := apiServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/"+testVpcID+"/vpn-connections":
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{
				"jobId": "job-1", "vpnConnection": map[string]any{"id": testConnID},
			}})
		case r.URL.Path == "/v1/resource/jobs/":
			testutil.WriteJSONResponse(w, client.JobStatusMultiResponse{Success: true, Data: client.JobStatusDataResponse{
				Jobs: []client.JobResponse{{JobID: "job-1", HasFailed: true, ErrorMessage: "boom"}},
			}})
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	})

	id, _, err := CreateVpnConnection(gpcnClient, context.Background(), testVpcID, map[string]any{"name": "x"})
	if id != testConnID {
		t.Errorf("Expected the connection ID with the failed job, got %q", id)
	}
	if !errors.Is(err, client.ErrJobFailed) {
		t.Errorf("Expected a failed-job error, got %v", err)
	}
}

func TestVpcVpnConnectionUnitCreateVpnConnectionRefusalKeepsMessageAndReturnsNoID(t *testing.T) {
	t.Parallel()
	const message = "A VPN connection to 203.0.113.10 already exists on this VPC (other)."
	gpcnClient := apiServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{"success":false,"message":%q,"error":{"code":"Duplicate Resource","statusCode":409,"details":null}}`, message)
	})

	id, rowMayExist, err := CreateVpnConnection(gpcnClient, context.Background(), testVpcID, map[string]any{"name": "x"})
	if id != "" || rowMayExist {
		t.Errorf("Expected no ID and no row for a refused POST, got %q and %v", id, rowMayExist)
	}
	if detail := vpcvpngateways.ErrorDetail(err); detail != message {
		t.Errorf("Expected %q, got %q", message, detail)
	}
}

func TestVpcVpnConnectionUnitUpdateVpnConnectionSendsOnlyTheBodyItGets(t *testing.T) {
	t.Parallel()
	var putBody map[string]any
	gpcnClient := apiServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/resource/vpcs/"+testVpcID+"/vpn-connections/"+testConnID:
			putBody = testutil.ReadRequestBody(r)
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"jobId": "job-1"}})
		case r.URL.Path == "/v1/resource/jobs/":
			testutil.HandleJobResponse(w, "job-1", testConnID, true)
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	})

	if err := UpdateVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID, map[string]any{"description": nil}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value, found := putBody["description"]; len(putBody) != 1 || !found || value != nil {
		t.Errorf("Expected a body of description null, got %v", putBody)
	}
}

func TestVpcVpnConnectionUnitDeleteVpnConnectionStateConflictKeepsMessage(t *testing.T) {
	t.Parallel()
	const message = "The VPN connection is still being created; wait for it to finish."
	gpcnClient := apiServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{"success":false,"message":%q,"error":{"code":"VPN_STATE_CONFLICT","statusCode":409,"details":{"state":"creating","verb":"delete","failureReason":null}}}`, message)
	})

	err := DeleteVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID)
	if code, got := vpcvpngateways.ParseAPIError(err); code != "VPN_STATE_CONFLICT" || got != message {
		t.Errorf("Expected VPN_STATE_CONFLICT and the message, got %q and %q", code, got)
	}
}

func TestVpcVpnConnectionUnitDeleteVpnConnectionNotFoundIsReadable(t *testing.T) {
	t.Parallel()
	gpcnClient := apiServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"success":false,"message":"VPN connection not found","error":{"code":"Resource Not Found","statusCode":404,"details":null}}`)
	})

	if err := DeleteVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID); !client.IsNotFound(err) {
		t.Errorf("Expected a not-found error, got %v", err)
	}
}

func TestVpcVpnConnectionUnitCreateVpnConnection5xxReportsAMaybeRow(t *testing.T) {
	t.Parallel()
	var posts atomic.Int32
	gpcnClient := apiServer(t, func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"success":false,"message":"bad gateway"}`)
	})

	id, rowMayExist, err := CreateVpnConnection(gpcnClient, context.Background(), testVpcID, map[string]any{"name": "x"})
	if err == nil || id != "" || !rowMayExist {
		t.Errorf("Expected an error, no ID and a maybe-row, got %q, %v, %v", id, rowMayExist, err)
	}
	if got := posts.Load(); got != 1 {
		t.Errorf("Expected the POST once, got %d", got)
	}
}

func TestVpcVpnConnectionUnitFindVpnConnectionByNameReadsEveryPage(t *testing.T) {
	t.Parallel()
	gpcnClient := apiServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/resource/vpcs/"+testVpcID+"/vpn-connections" {
			testutil.LogUnexpectedRequest(t, w, r)
			return
		}
		page := r.URL.Query().Get("page")
		rows := []map[string]any{{"id": "other", "name": "first"}}
		if page == "2" {
			rows = []map[string]any{{"id": testConnID, "name": "wanted"}}
		}
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": rows, "meta": map[string]any{"hasNextPage": page == "1"}})
	})

	found, err := FindVpnConnectionByName(gpcnClient, context.Background(), testVpcID, "wanted")
	if err != nil || found == nil || found.ID != testConnID {
		t.Fatalf("Expected the connection on page 2, got %+v, %v", found, err)
	}
	missing, err := FindVpnConnectionByName(gpcnClient, context.Background(), testVpcID, "absent")
	if err != nil || missing != nil {
		t.Errorf("Expected no connection, got %+v, %v", missing, err)
	}
}

// A canceled apply must not stop the lookups that follow a failed create.
func TestVpcVpnConnectionUnitLookupsAfterFailureSurviveCanceledContext(t *testing.T) {
	t.Parallel()
	gpcnClient := apiServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/resource/vpcs/" + testVpcID + "/vpn-connections":
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": []map[string]any{{"id": testConnID, "name": "wanted"}},
				"meta": map[string]any{"hasNextPage": false}})
		case "/v1/resource/vpcs/" + testVpcID + "/vpn-connections/" + testConnID:
			testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{"id": testConnID, "name": "wanted"}})
		default:
			testutil.LogUnexpectedRequest(t, w, r)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	found, err := FindAfterFailure(gpcnClient, ctx, testVpcID, "wanted")
	if err != nil || found == nil || found.ID != testConnID {
		t.Errorf("Expected the lookup to find the row with a canceled context, got %+v, %v", found, err)
	}
	detail, err := ReadAfterFailure(gpcnClient, ctx, testVpcID, testConnID)
	if err != nil || detail == nil || detail.ID != testConnID {
		t.Errorf("Expected the read to succeed with a canceled context, got %+v, %v", detail, err)
	}
	if _, err := FindVpnConnectionByName(gpcnClient, ctx, testVpcID, "wanted"); err == nil {
		t.Error("Expected a plain lookup on the canceled context to fail")
	}
}

func TestVpcVpnConnectionUnitRetryUnappliedDue(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		applied   bool
		state     string
		jobActive bool
		want      bool
	}{
		"idle and unapplied": {false, StateConnected, false, true},
		"applied":            {true, StateConnected, false, false},
		"creating":           {false, StateCreating, false, false},
		"removing":           {false, StateRemoving, false, false},
		"job active":         {false, StateConnected, true, false},
	} {
		if got := RetryUnappliedDue(test.applied, test.state, test.jobActive); got != test.want {
			t.Errorf("%s: got %v, want %v", name, got, test.want)
		}
	}
}

func removingConflictServer(t *testing.T, details string, rows []string) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	gpcnClient := apiServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprintf(w, `{"success":false,"message":"busy","error":{"code":"VPN_STATE_CONFLICT","statusCode":409,"details":%s}}`, details)
			return
		}
		index := int(reads.Add(1)) - 1
		if index >= len(rows) {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"success":false,"message":"VPN connection not found","error":{"code":"Resource Not Found","statusCode":404,"details":null}}`)
			return
		}
		fmt.Fprint(w, rows[index])
	})
	return gpcnClient, &reads
}

const removingConnRow = `{"success":true,"data":{"id":"` + testConnID + `","name":"x","state":"removing"}}`

func TestVpcVpnConnectionUnitDeleteRemoving409WaitsForTheRow(t *testing.T) {
	t.Parallel()
	gpcnClient, reads := removingConflictServer(t, `{"state":"removing","verb":"delete","failureReason":null}`, []string{removingConnRow, removingConnRow})

	if err := deleteVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID, time.Millisecond, time.Minute); err != nil {
		t.Fatalf("Expected the delete to wait for the 404, got %v", err)
	}
	if got := reads.Load(); got != 3 {
		t.Errorf("Expected 3 reads (two removing, one 404), got %d", got)
	}
}

func TestVpcVpnConnectionUnitDeleteRemoving409TimesOut(t *testing.T) {
	t.Parallel()
	gpcnClient, _ := removingConflictServer(t, `{"state":"removing","verb":"delete","failureReason":null}`,
		slices.Repeat([]string{removingConnRow}, 1000))

	err := deleteVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID, time.Millisecond, 20*time.Millisecond)
	if !errors.Is(err, ErrRemovalTimeout) {
		t.Errorf("Expected a removal timeout, got %v", err)
	}
}

// A removal that failed, or a connection in another state, is a refusal.
func TestVpcVpnConnectionUnitDeleteOtherConflictsAreRefusals(t *testing.T) {
	t.Parallel()
	for name, details := range map[string]string{
		"removing with a failure": `{"state":"removing","verb":"delete","failureReason":"The tunnel is stuck."}`,
		"creating":                `{"state":"creating","verb":"delete","failureReason":null}`,
		"no details":              `null`,
	} {
		gpcnClient, reads := removingConflictServer(t, details, nil)
		err := deleteVpnConnection(gpcnClient, context.Background(), testVpcID, testConnID, time.Millisecond, time.Minute)
		if code, _ := vpcvpngateways.ParseAPIError(err); code != "VPN_STATE_CONFLICT" {
			t.Errorf("%s: expected the 409 to reach the caller, got %v", name, err)
		}
		if reads.Load() != 0 {
			t.Errorf("%s: expected no wait", name)
		}
	}
}
