package vpcvpngateways

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	testVpcID     = "11111111-1111-4111-8111-111111111111"
	testGatewayID = "22222222-2222-4222-8222-222222222222"
)

func ptr(value string) *string { return &value }

func TestVpcVpnGatewayUnitMapGatewayToModelRouterKindHasNullReservedBlock(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{
		ID: testGatewayID, VpcID: testVpcID, Kind: KindRouter, State: StateReady,
		Address: ptr("198.51.100.7"),
	}

	model := MapGatewayToModel(response, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if model.ID.ValueString() != testGatewayID {
		t.Errorf("Expected id %q, got %q", testGatewayID, model.ID.ValueString())
	}
	if model.Kind.ValueString() != KindRouter {
		t.Errorf("Expected kind %q, got %q", KindRouter, model.Kind.ValueString())
	}
	if model.Address.ValueString() != "198.51.100.7" {
		t.Errorf("Expected address 198.51.100.7, got %q", model.Address.ValueString())
	}
	if !model.ReservedBlock.IsNull() {
		t.Errorf("Expected a null reserved_block on the router kind, got %v", model.ReservedBlock)
	}
	if !model.FailureReason.IsNull() {
		t.Errorf("Expected a null failure_reason, got %v", model.FailureReason)
	}
	if model.State.ValueString() != StateReady {
		t.Errorf("Expected state %q, got %q", StateReady, model.State.ValueString())
	}
}

func TestVpcVpnGatewayUnitMapGatewayToModelApplianceKindCarriesReservedBlock(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{
		ID: testGatewayID, VpcID: testVpcID, Kind: KindAppliance, State: StateReady,
		Address: ptr("203.0.113.9"), ReservedBlock: ptr("10.112.255.240/28"),
	}

	model := MapGatewayToModel(response, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if model.ReservedBlock.ValueString() != "10.112.255.240/28" {
		t.Errorf("Expected reserved_block 10.112.255.240/28, got %v", model.ReservedBlock)
	}
	if model.Kind.ValueString() != KindAppliance {
		t.Errorf("Expected kind %q, got %q", KindAppliance, model.Kind.ValueString())
	}
}

func TestVpcVpnGatewayUnitMapGatewayToModelKeepsNullAddressWhileCreating(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{ID: testGatewayID, VpcID: testVpcID, Kind: KindRouter, State: StateCreating}

	model := MapGatewayToModel(response, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if !model.Address.IsNull() {
		t.Errorf("Expected a null address while the gateway is creating, got %v", model.Address)
	}
}

func TestVpcVpnGatewayUnitMapGatewayToModelFillsVpcIDOnImport(t *testing.T) {
	t.Parallel()
	response := &ApiVpnGateway{ID: testGatewayID, VpcID: testVpcID, Kind: KindRouter, State: StateReady}

	model := MapGatewayToModel(response, ResourceModel{})

	if model.VpcID.ValueString() != testVpcID {
		t.Errorf("Expected vpc_id %q, got %q", testVpcID, model.VpcID.ValueString())
	}
}

// errorServer answers every request with one status and body. The real
// transport stays in the stack, so the error arrives as *client.HTTPError.
func errorServer(t *testing.T, status int, body string) *client.GpcnClient {
	t.Helper()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, body)
		},
	})
	return gpcnClient
}

func TestVpcVpnGatewayUnitParseAPIErrorReturnsMessageVerbatimAndCode(t *testing.T) {
	t.Parallel()
	const message = "Remove the VPC's VPN connections before removing its VPN gateway."
	gpcnClient := errorServer(t, http.StatusConflict,
		`{"success":false,"message":"`+message+`","error":{"code":"VPN_GATEWAY_IN_USE","statusCode":409,"details":null}}`)

	err := DeleteVpnGateway(gpcnClient, context.Background(), testVpcID)
	if err == nil {
		t.Fatal("Expected the 409 to fail the delete")
	}

	code, got := ParseAPIError(err)
	if code != "VPN_GATEWAY_IN_USE" {
		t.Errorf("Expected code VPN_GATEWAY_IN_USE, got %q", code)
	}
	if got != message {
		t.Errorf("Expected message %q, got %q", message, got)
	}
	if detail := ErrorDetail(err); detail != message {
		t.Errorf("Expected the detail to be the message alone, got %q", detail)
	}
}

func TestVpcVpnGatewayUnitParseAPIErrorOnNonAPIErrorReadsEmpty(t *testing.T) {
	t.Parallel()
	err := errors.New("connection refused")

	code, message := ParseAPIError(err)
	if code != "" || message != "" {
		t.Errorf("Expected an empty code and message, got %q and %q", code, message)
	}
	if detail := ErrorDetail(err); detail != "connection refused" {
		t.Errorf("Expected the error text for a non-API error, got %q", detail)
	}
}

func TestVpcVpnGatewayUnitParseAPIErrorKeepsDataCenterRefusalMessage(t *testing.T) {
	t.Parallel()
	const message = "This data center does not offer site-to-site VPN yet."
	gpcnClient := errorServer(t, http.StatusConflict,
		`{"success":false,"message":"`+message+`","error":{"code":"Duplicate Resource","statusCode":409,"details":null}}`)

	err := CreateVpnGateway(gpcnClient, context.Background(), testVpcID)
	if err == nil {
		t.Fatal("Expected the 409 to fail the create")
	}
	if detail := ErrorDetail(err); detail != message {
		t.Errorf("Expected %q, got %q", message, detail)
	}
}

func TestVpcVpnGatewayUnitGetVpnGatewayMapsNotFoundToSentinel(t *testing.T) {
	t.Parallel()
	gpcnClient := errorServer(t, http.StatusNotFound,
		`{"success":false,"message":"This VPC has no VPN gateway.","error":{"code":"VPN_GATEWAY_NOT_FOUND","statusCode":404,"details":null}}`)

	_, err := GetVpnGateway(gpcnClient, context.Background(), testVpcID)
	if !errors.Is(err, ErrVpnGatewayNotFound) {
		t.Fatalf("Expected ErrVpnGatewayNotFound, got %v", err)
	}
	if !client.IsNotFound(err) {
		t.Errorf("Expected the API error to stay in the chain, got %v", err)
	}
}

func TestVpcVpnGatewayUnitCreateVpnGatewaySendsEmptyObjectBody(t *testing.T) {
	t.Parallel()
	var createBody atomic.Value
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/"+testVpcID+"/vpn-gateway":
				createBody.Store(testutil.ReadRequestBody(r))
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true, "message": "Operation initiated successfully",
					"data": map[string]any{"jobId": "job-create", "vpnGateway": map[string]any{"id": testGatewayID, "state": "creating"}},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.HandleJobResponse(w, "job-create", testGatewayID, true)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	if err := CreateVpnGateway(gpcnClient, context.Background(), testVpcID); err != nil {
		t.Fatalf("Expected the create to succeed, got %v", err)
	}
	body, _ := createBody.Load().(map[string]any)
	if body == nil || len(body) != 0 {
		t.Errorf("Expected an empty JSON object body, got %v", body)
	}
}

// removalServer serves the gateway the way a removal looks from outside. Each
// read takes the next scripted answer, and the last one repeats.
func removalServer(t *testing.T, answers []string) (*client.GpcnClient, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v1/resource/vpcs/"+testVpcID+"/vpn-gateway" {
				testutil.LogUnexpectedRequest(t, w, r)
				return
			}
			index := int(reads.Add(1)) - 1
			if index >= len(answers) {
				index = len(answers) - 1
			}
			answer := answers[index]
			w.Header().Set("Content-Type", "application/json")
			if answer == "gone" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"success":false,"message":"This VPC has no VPN gateway.","error":{"code":"VPN_GATEWAY_NOT_FOUND","statusCode":404,"details":null}}`)
				return
			}
			fmt.Fprint(w, answer)
		},
	})
	return gpcnClient, &reads
}

func removingRow(reason string) string {
	failure := "null"
	if reason != "" {
		failure = `"` + reason + `"`
	}
	return `{"success":true,"message":"","data":{"id":"` + testGatewayID + `","vpcId":"` + testVpcID +
		`","kind":"router","state":"removing","address":"198.51.100.7","reservedBlock":null,"failureReason":` + failure +
		`,"activeJobId":null,"createdAt":"2026-01-02T15:04:05Z","updatedAt":"2026-01-02T15:04:05Z"}}`
}

func TestVpcVpnGatewayUnitWaitForVpnGatewayGoneKeepsWaitingThroughParkedRemoval(t *testing.T) {
	t.Parallel()
	gpcnClient, reads := removalServer(t, []string{
		removingRow(""),
		removingRow("The VPN gateway's removal failed; it is retried automatically."),
		"gone",
	})

	err := waitForVpnGatewayGone(gpcnClient, context.Background(), testVpcID, time.Millisecond, time.Minute)
	if err != nil {
		t.Fatalf("Expected the wait to end at the 404, got %v", err)
	}
	if got := reads.Load(); got != 3 {
		t.Errorf("Expected 3 reads, got %d", got)
	}
}

func TestVpcVpnGatewayUnitWaitForVpnGatewayGoneTimeoutNamesLastStateAndFailureReason(t *testing.T) {
	t.Parallel()
	const reason = "The VPN gateway's removal failed; it is retried automatically."
	gpcnClient, _ := removalServer(t, []string{removingRow(""), removingRow(reason)})

	err := waitForVpnGatewayGone(gpcnClient, context.Background(), testVpcID, time.Millisecond, 20*time.Millisecond)
	if !errors.Is(err, ErrRemovalTimeout) {
		t.Fatalf("Expected ErrRemovalTimeout, got %v", err)
	}
	for _, want := range []string{"'removing'", reason, testVpcID} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the timeout error to contain %q, got %q", want, err.Error())
		}
	}
}

func TestVpcVpnGatewayUnitWaitForVpnGatewayGoneStopsOnReadError(t *testing.T) {
	t.Parallel()
	gpcnClient := errorServer(t, http.StatusForbidden,
		`{"success":false,"message":"You do not have permission to perform this action.","error":{"code":"Insufficient Permissions","statusCode":403,"details":null}}`)

	err := waitForVpnGatewayGone(gpcnClient, context.Background(), testVpcID, time.Millisecond, time.Minute)
	if err == nil {
		t.Fatal("Expected the 403 to stop the wait")
	}
	if got := ErrorDetail(err); got != "You do not have permission to perform this action." {
		t.Errorf("Expected the API message, got %q", got)
	}
}
