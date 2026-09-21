package virtualmachines

import (
	"context"
	"testing"

	"terraform-provider-gpcn/internal/networks"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	testIPRelease  = "198.51.100.25"
	testIPPreserve = "203.0.113.50"
)

var testPlanModifierSchema = schema.Schema{
	Attributes: map[string]schema.Attribute{
		"allocate_public_ip": schema.BoolAttribute{Required: true},
		"public_ip":          schema.StringAttribute{Computed: true},
		"public_ip_id":       schema.StringAttribute{Optional: true},
	},
}

func createRawValue(allocatePublicIp bool, publicIp, publicIpId string) tftypes.Value {
	heldAddress := tftypes.NewValue(tftypes.String, nil)
	if publicIpId != "" {
		heldAddress = tftypes.NewValue(tftypes.String, publicIpId)
	}

	return tftypes.NewValue(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"allocate_public_ip": tftypes.Bool,
			"public_ip":          tftypes.String,
			"public_ip_id":       tftypes.String,
		},
	}, map[string]tftypes.Value{
		"allocate_public_ip": tftypes.NewValue(tftypes.Bool, allocatePublicIp),
		"public_ip":          tftypes.NewValue(tftypes.String, publicIp),
		"public_ip_id":       heldAddress,
	})
}

func createTestState(allocatePublicIp bool, publicIp, publicIpId string) tfsdk.State {
	return tfsdk.State{Raw: createRawValue(allocatePublicIp, publicIp, publicIpId), Schema: testPlanModifierSchema}
}

func createTestPlan(allocatePublicIp bool, publicIp, publicIpId string) tfsdk.Plan {
	return tfsdk.Plan{Raw: createRawValue(allocatePublicIp, publicIp, publicIpId), Schema: testPlanModifierSchema}
}

type publicIPTestCase struct {
	name          string
	stateValue    types.String
	planValue     types.String
	stateAllocate bool
	planAllocate  bool
	stateHeldID   string
	planHeldID    string
	stateIP       string
	expectUnknown bool
	expectedValue string
}

func (tc publicIPTestCase) run(t *testing.T) {
	t.Helper()
	req := planmodifier.StringRequest{
		StateValue: tc.stateValue,
		PlanValue:  tc.planValue,
		Path:       path.Root("public_ip"),
	}

	if !tc.stateValue.IsNull() {
		req.State = createTestState(tc.stateAllocate, tc.stateIP, tc.stateHeldID)
		req.Plan = createTestPlan(tc.planAllocate, tc.stateIP, tc.planHeldID)
	}

	resp := &planmodifier.StringResponse{PlanValue: tc.planValue}
	PublicIpPlanModifier{}.PlanModifyString(context.Background(), req, resp)

	if tc.expectUnknown && !resp.PlanValue.IsUnknown() {
		t.Errorf("expected unknown, got %v", resp.PlanValue)
	}
	if !tc.expectUnknown && resp.PlanValue.ValueString() != tc.expectedValue {
		t.Errorf("expected %q, got %q", tc.expectedValue, resp.PlanValue.ValueString())
	}
}

func TestPublicIpPlanModifier(t *testing.T) {
	tests := []publicIPTestCase{
		{
			name:          "on create leaves unknown",
			stateValue:    types.StringNull(),
			planValue:     types.StringUnknown(),
			expectUnknown: true,
		},
		{
			name:          "allocate changes false to true",
			stateValue:    types.StringValue(""),
			planValue:     types.StringValue(""),
			stateAllocate: false,
			planAllocate:  true,
			expectUnknown: true,
		},
		{
			name:          "allocate changes true to false",
			stateValue:    types.StringValue(testIPRelease),
			planValue:     types.StringValue(testIPRelease),
			stateAllocate: true,
			planAllocate:  false,
			stateIP:       testIPRelease,
			expectUnknown: true,
		},
		{
			name:          "public_ip_id changes from unset",
			stateValue:    types.StringValue(""),
			planValue:     types.StringValue(""),
			planHeldID:    "address-1",
			expectUnknown: true,
		},
		{
			name:          "public_ip_id changes to unset",
			stateValue:    types.StringValue(testIPRelease),
			planValue:     types.StringValue(testIPRelease),
			stateHeldID:   "address-1",
			stateIP:       testIPRelease,
			expectUnknown: true,
		},
		{
			name:          "allocate unchanged preserves value",
			stateValue:    types.StringValue(testIPPreserve),
			planValue:     types.StringUnknown(),
			stateAllocate: true,
			planAllocate:  true,
			stateIP:       testIPPreserve,
			expectedValue: testIPPreserve,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}

// Tests for NetworkInterfacesPlanModifier
var testNetworkInterfacesSchema = schema.Schema{
	Attributes: map[string]schema.Attribute{
		"subnet_id":          schema.StringAttribute{Required: true},
		"l2_segment_ids":     schema.ListAttribute{Required: true, ElementType: types.StringType},
		"allocate_public_ip": schema.BoolAttribute{Required: true},
		"public_ip_id":       schema.StringAttribute{Optional: true},
	},
}

var interfaceElemType = types.ObjectType{AttrTypes: networks.ReadVirtualMachineNetworkDataResponseTF{}.AttrTypes()}

// networkInputs names the four attributes that shape the interface set.
type networkInputs struct {
	subnetID         string
	segmentIDs       []string
	allocatePublicIP bool
	publicIPID       *string
}

func createNetworkRawValue(inputs networkInputs) tftypes.Value {
	ids := make([]tftypes.Value, len(inputs.segmentIDs))
	for i, id := range inputs.segmentIDs {
		ids[i] = tftypes.NewValue(tftypes.String, id)
	}

	publicIPID := tftypes.NewValue(tftypes.String, nil)
	if inputs.publicIPID != nil {
		publicIPID = tftypes.NewValue(tftypes.String, *inputs.publicIPID)
	}

	return tftypes.NewValue(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"subnet_id":          tftypes.String,
			"l2_segment_ids":     tftypes.List{ElementType: tftypes.String},
			"allocate_public_ip": tftypes.Bool,
			"public_ip_id":       tftypes.String,
		},
	}, map[string]tftypes.Value{
		"subnet_id":          tftypes.NewValue(tftypes.String, inputs.subnetID),
		"l2_segment_ids":     tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, ids),
		"allocate_public_ip": tftypes.NewValue(tftypes.Bool, inputs.allocatePublicIP),
		"public_ip_id":       publicIPID,
	})
}

func testInterfaceList(t *testing.T) types.List {
	t.Helper()
	list, diags := types.ListValueFrom(context.Background(), interfaceElemType, []networks.ReadVirtualMachineNetworkDataResponseTF{{
		ID:               types.StringValue("interface-001"),
		NetworkInterface: types.Int64Value(1),
		IsPrimary:        types.BoolValue(true),
		MacAddress:       types.StringValue("fa:16:3e:00:00:01"),
		PublicIP:         types.StringNull(),
		PublicIPID:       types.StringNull(),
		PrivateIP:        types.StringValue("10.0.0.5"),
		World:            types.StringValue(networks.NicWorldVpc),
		NetworkName:      types.StringNull(),
		NetworkID:        types.StringNull(),
		CIDRBlock:        types.StringValue("10.0.0.0/24"),
		GatewayIP:        types.StringNull(),
		NetworkType:      types.StringNull(),
		VpcSubnetID:      types.StringValue("subnet-a"),
		SubnetName:       types.StringValue("web"),
		VpcID:            types.StringValue("vpc-a"),
		VpcName:          types.StringValue("prod"),
		L2SegmentID:      types.StringNull(),
		L2SegmentName:    types.StringNull(),
	}})
	if diags.HasError() {
		t.Fatalf("failed to build interface list: %v", diags)
	}
	return list
}

func TestNetworkInterfacesPlanModifier(t *testing.T) {
	stateList := testInterfaceList(t)
	baseline := networkInputs{subnetID: "subnet-a", segmentIDs: []string{"segment-a"}}
	heldAddress := "address-1"

	t.Run("on create leaves unknown", func(t *testing.T) {
		req := planmodifier.ListRequest{
			StateValue: types.ListNull(interfaceElemType),
			PlanValue:  types.ListUnknown(interfaceElemType),
			Path:       path.Root("network_interfaces"),
		}
		resp := &planmodifier.ListResponse{PlanValue: req.PlanValue}
		NetworkInterfacesPlanModifier{}.PlanModifyList(context.Background(), req, resp)
		if !resp.PlanValue.IsUnknown() {
			t.Errorf("expected unknown, got %v", resp.PlanValue)
		}
	})

	changes := []struct {
		name string
		plan networkInputs
	}{
		{"subnet_id", networkInputs{subnetID: "subnet-b", segmentIDs: []string{"segment-a"}}},
		{"l2_segment_ids", networkInputs{subnetID: "subnet-a", segmentIDs: []string{"segment-a", "segment-b"}}},
		{"allocate_public_ip", networkInputs{subnetID: "subnet-a", segmentIDs: []string{"segment-a"}, allocatePublicIP: true}},
		{"public_ip_id", networkInputs{subnetID: "subnet-a", segmentIDs: []string{"segment-a"}, publicIPID: &heldAddress}},
	}
	for _, change := range changes {
		t.Run(change.name+" change marks unknown", func(t *testing.T) {
			req := planmodifier.ListRequest{
				StateValue: stateList,
				PlanValue:  stateList,
				Path:       path.Root("network_interfaces"),
				State:      tfsdk.State{Raw: createNetworkRawValue(baseline), Schema: testNetworkInterfacesSchema},
				Plan:       tfsdk.Plan{Raw: createNetworkRawValue(change.plan), Schema: testNetworkInterfacesSchema},
			}
			resp := &planmodifier.ListResponse{PlanValue: req.PlanValue}
			NetworkInterfacesPlanModifier{}.PlanModifyList(context.Background(), req, resp)
			if !resp.PlanValue.IsUnknown() {
				t.Errorf("expected unknown when %s changes, got %v", change.name, resp.PlanValue)
			}
		})
	}

	t.Run("inputs unchanged preserves value", func(t *testing.T) {
		req := planmodifier.ListRequest{
			StateValue: stateList,
			PlanValue:  types.ListUnknown(interfaceElemType),
			Path:       path.Root("network_interfaces"),
			State:      tfsdk.State{Raw: createNetworkRawValue(baseline), Schema: testNetworkInterfacesSchema},
			Plan:       tfsdk.Plan{Raw: createNetworkRawValue(baseline), Schema: testNetworkInterfacesSchema},
		}
		resp := &planmodifier.ListResponse{PlanValue: req.PlanValue}
		NetworkInterfacesPlanModifier{}.PlanModifyList(context.Background(), req, resp)
		if !resp.PlanValue.Equal(stateList) {
			t.Errorf("expected preserved state value, got %v", resp.PlanValue)
		}
	})
}

// Tests for ConfigurationPlanModifier
var (
	testConfigurationPlanModifierSchema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"size_id":       schema.StringAttribute{Required: true},
			"configuration": schema.MapAttribute{Computed: true, ElementType: types.StringType},
		},
	}

	testConfiguration = map[string]string{
		"name":         "G-Small-1",
		"cpu":          "2 cores",
		"ram":          "4 GB",
		"base_storage": "50 GB",
	}
)

func createConfigRawValue(sizeId string, configuration map[string]string) tftypes.Value {
	configMap := make(map[string]tftypes.Value, len(configuration))
	for k, v := range configuration {
		configMap[k] = tftypes.NewValue(tftypes.String, v)
	}

	return tftypes.NewValue(tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"size_id":       tftypes.String,
			"configuration": tftypes.Map{ElementType: tftypes.String},
		},
	}, map[string]tftypes.Value{
		"size_id":       tftypes.NewValue(tftypes.String, sizeId),
		"configuration": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, configMap),
	})
}

func createConfigTestState(sizeId string, configuration map[string]string) tfsdk.State {
	return tfsdk.State{Raw: createConfigRawValue(sizeId, configuration), Schema: testConfigurationPlanModifierSchema}
}

func createConfigTestPlan(sizeId string, configuration map[string]string) tfsdk.Plan {
	return tfsdk.Plan{Raw: createConfigRawValue(sizeId, configuration), Schema: testConfigurationPlanModifierSchema}
}

type configurationTestCase struct {
	name            string
	stateValue      types.Map
	planValue       types.Map
	stateSizeId     string
	planSizeId      string
	stateConfig     map[string]string
	expectUnknown   bool
	expectPreserved bool
}

func (tc configurationTestCase) run(t *testing.T) {
	t.Helper()
	req := planmodifier.MapRequest{
		StateValue: tc.stateValue,
		PlanValue:  tc.planValue,
		Path:       path.Root("configuration"),
	}

	if !tc.stateValue.IsNull() {
		req.State = createConfigTestState(tc.stateSizeId, tc.stateConfig)
		req.Plan = createConfigTestPlan(tc.planSizeId, tc.stateConfig)
	}

	resp := &planmodifier.MapResponse{PlanValue: tc.planValue}
	ConfigurationPlanModifier{}.PlanModifyMap(context.Background(), req, resp)

	switch {
	case tc.expectUnknown:
		if !resp.PlanValue.IsUnknown() {
			t.Errorf("expected unknown, got %v", resp.PlanValue)
		}
	case tc.expectPreserved:
		assertConfigPreserved(t, resp.PlanValue, tc.stateConfig)
	}
}

func assertConfigPreserved(t *testing.T, actual types.Map, expected map[string]string) {
	t.Helper()
	if actual.IsUnknown() || actual.IsNull() {
		t.Error("expected configuration to be preserved")
		return
	}
	elements := actual.Elements()
	for key, expectedVal := range expected {
		if elem, ok := elements[key]; !ok {
			t.Errorf("missing key %q", key)
		} else if elem.(types.String).ValueString() != expectedVal {
			t.Errorf("key %q: expected %q, got %q", key, expectedVal, elem.(types.String).ValueString())
		}
	}
}

func TestConfigurationPlanModifier(t *testing.T) {
	configMap, _ := types.MapValueFrom(context.Background(), types.StringType, testConfiguration)

	tests := []configurationTestCase{
		{
			name:          "on create leaves unknown",
			stateValue:    types.MapNull(types.StringType),
			planValue:     types.MapUnknown(types.StringType),
			expectUnknown: true,
		},
		{
			name:          "size_id changes marks unknown",
			stateValue:    configMap,
			planValue:     configMap,
			stateSizeId:   "sku-abc-111",
			planSizeId:    "sku-abc-222",
			stateConfig:   testConfiguration,
			expectUnknown: true,
		},
		{
			name:            "size_id unchanged preserves value",
			stateValue:      configMap,
			planValue:       types.MapUnknown(types.StringType),
			stateSizeId:     "sku-abc-111",
			planSizeId:      "sku-abc-111",
			stateConfig:     testConfiguration,
			expectPreserved: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}
