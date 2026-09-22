package vpcpublicips

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	testVpcID      = "11111111-1111-1111-1111-111111111111"
	testPublicIpID = "22222222-2222-2222-2222-222222222222"
	testNicID      = "33333333-3333-3333-3333-333333333333"
	testVmID       = "44444444-4444-4444-4444-444444444444"
	testJobID      = "job-1"
)

// recorder keeps every request path, method and raw body the mock served. A
// test can then pin the exact bytes the package puts on the wire.
type recorder struct {
	mu       sync.Mutex
	methods  []string
	paths    []string
	queries  []string
	bodies   []string
	requests int
}

func (rec *recorder) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.methods = append(rec.methods, r.Method)
	rec.paths = append(rec.paths, r.URL.Path)
	rec.queries = append(rec.queries, r.URL.RawQuery)
	rec.bodies = append(rec.bodies, string(body))
	rec.requests++
}

func (rec *recorder) bodyFor(method, path string) (string, bool) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i := range rec.paths {
		if rec.methods[i] == method && rec.paths[i] == path {
			return rec.bodies[i], true
		}
	}
	return "", false
}

func (rec *recorder) queriesFor(method, path string) []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var found []string
	for i := range rec.paths {
		if rec.methods[i] == method && rec.paths[i] == path {
			found = append(found, rec.queries[i])
		}
	}
	return found
}

func publicIpRow(id string, ipAddress, virtualMachineID *string) map[string]any {
	return map[string]any{
		"id":                 id,
		"ipAddress":          ipAddress,
		"state":              PUBLIC_IP_STATE_READY,
		"failureReason":      nil,
		"virtualMachineId":   virtualMachineID,
		"virtualMachineName": nil,
		"held":               virtualMachineID == nil,
		"activeJobId":        nil,
		"createdAt":          "2026-01-02T15:04:05Z",
		"updatedAt":          "2026-01-02T15:04:05Z",
	}
}

func listBody(rows []map[string]any, page, totalPages int) map[string]any {
	return map[string]any{
		"success": true,
		"message": "",
		"data":    rows,
		"meta": map[string]any{
			"total":           len(rows),
			"page":            page,
			"pageSize":        PUBLIC_IP_LIST_PAGE_SIZE,
			"totalPages":      totalPages,
			"hasNextPage":     page < totalPages,
			"hasPreviousPage": page > 1,
		},
	}
}

// A newly acquired address answers with its id before the job finishes. The
// package must read the id from the acquire response, not from the job.
func TestAcquirePublicIpReturnsIdAndPollsMockHTTP(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/"+testVpcID+"/public-ips":
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"publicIpId": testPublicIpID, "jobId": testJobID},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.HandleJobResponse(w, testJobID, "", true)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	publicIpID, err := AcquirePublicIp(gpcnClient, context.Background(), testVpcID)
	if err != nil {
		t.Fatalf("AcquirePublicIp returned an error: %v", err)
	}
	if publicIpID != testPublicIpID {
		t.Fatalf("Expected public IP ID %q, got %q", testPublicIpID, publicIpID)
	}

	body, ok := rec.bodyFor(http.MethodPost, "/v1/resource/vpcs/"+testVpcID+"/public-ips")
	if !ok {
		t.Fatalf("Expected an acquire POST, got none")
	}
	if body != "" {
		t.Fatalf("Expected an empty acquire body, got %q", body)
	}
}

// The attach body names the NIC, never the machine. The backend schema is
// strict, so an extra key turns the attach into a 422.
func TestAttachPublicIpSendsNicIdMockHTTP(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	attachPath := "/v1/resource/vpcs/" + testVpcID + "/public-ips/" + testPublicIpID + "/attach"
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			switch {
			case r.Method == http.MethodPost && r.URL.Path == attachPath:
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"jobId": testJobID},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.HandleJobResponse(w, testJobID, "", true)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	if err := AttachPublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID, testNicID); err != nil {
		t.Fatalf("AttachPublicIp returned an error: %v", err)
	}

	body, ok := rec.bodyFor(http.MethodPost, attachPath)
	if !ok {
		t.Fatalf("Expected an attach POST, got none")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Expected a JSON attach body, got %q: %v", body, err)
	}
	if len(decoded) != 1 {
		t.Fatalf("Expected exactly one key in the attach body, got %v", decoded)
	}
	if got, _ := decoded["nicId"].(string); got != testNicID {
		t.Fatalf("Expected attach body nicId %q, got %q", testNicID, got)
	}
	if _, ok := rec.bodyFor(http.MethodPost, "/v1/resource/jobs/"); !ok {
		t.Fatalf("Expected the attach to poll its job, got no job request")
	}
}

// Detach is a bodiless POST. A body carrying keys is refused by the strict
// schema behind the route.
func TestDetachPublicIpSendsEmptyBodyMockHTTP(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	detachPath := "/v1/resource/vpcs/" + testVpcID + "/public-ips/" + testPublicIpID + "/detach"
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			switch {
			case r.Method == http.MethodPost && r.URL.Path == detachPath:
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"jobId": testJobID},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.HandleJobResponse(w, testJobID, "", true)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	if err := DetachPublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID); err != nil {
		t.Fatalf("DetachPublicIp returned an error: %v", err)
	}

	body, ok := rec.bodyFor(http.MethodPost, detachPath)
	if !ok {
		t.Fatalf("Expected a detach POST, got none")
	}
	if body != "" {
		t.Fatalf("Expected an empty detach body, got %q", body)
	}
	if _, ok := rec.bodyFor(http.MethodPost, "/v1/resource/jobs/"); !ok {
		t.Fatalf("Expected the detach to poll its job, got no job request")
	}
}

func TestReleasePublicIpPollsMockHTTP(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	releasePath := "/v1/resource/vpcs/" + testVpcID + "/public-ips/" + testPublicIpID
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			switch {
			case r.Method == http.MethodDelete && r.URL.Path == releasePath:
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"jobId": testJobID},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.HandleJobResponse(w, testJobID, "", true)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	if err := ReleasePublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID); err != nil {
		t.Fatalf("ReleasePublicIp returned an error: %v", err)
	}
	if _, ok := rec.bodyFor(http.MethodPost, "/v1/resource/jobs/"); !ok {
		t.Fatalf("Expected the release to poll its job, got no job request")
	}
}

// An address another operator already released answers 404. Destroy has nothing
// left to do, so the verb reports success.
func TestReleasePublicIpTreatsNotFoundAsDoneMockHTTP(t *testing.T) {
	t.Parallel()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			testutil.WriteJSONResponse(w, map[string]any{
				"success": false,
				"message": "Public IP not found",
				"error":   map[string]any{"code": "Resource Not Found", "statusCode": 404, "details": nil},
			})
		},
	})

	if err := ReleasePublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID); err != nil {
		t.Fatalf("Expected a 404 release to report success, got: %v", err)
	}
}

// The API has no single-read route for an address, so a read walks the parent
// listing. A row on the second page must still be found.
func TestGetPublicIpPagesTheListingMockHTTP(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	listPath := "/v1/resource/vpcs/" + testVpcID + "/public-ips"
	address := "203.0.113.10"
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			rec.record(r)
			if r.Method != http.MethodGet || r.URL.Path != listPath {
				testutil.LogUnexpectedRequest(t, w, r)
				return
			}
			if r.URL.Query().Get("page") == "2" {
				testutil.WriteJSONResponse(w, listBody([]map[string]any{publicIpRow(testPublicIpID, &address, nil)}, 2, 2))
				return
			}
			testutil.WriteJSONResponse(w, listBody([]map[string]any{publicIpRow("other-id", nil, nil)}, 1, 2))
		},
	})

	publicIp, err := GetPublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID)
	if err != nil {
		t.Fatalf("GetPublicIp returned an error: %v", err)
	}
	if publicIp.Id != testPublicIpID {
		t.Fatalf("Expected the address with ID %q, got %q", testPublicIpID, publicIp.Id)
	}
	if publicIp.IpAddress == nil || *publicIp.IpAddress != address {
		t.Fatalf("Expected IP address %q, got %v", address, publicIp.IpAddress)
	}
	queries := rec.queriesFor(http.MethodGet, listPath)
	if len(queries) != 2 {
		t.Fatalf("Expected two listing requests, got %d: %v", len(queries), queries)
	}
	// The route caps its page size at 100 and defaults to 20. A smaller limit
	// multiplies the requests every read of one address costs.
	for i, query := range queries {
		values, err := url.ParseQuery(query)
		if err != nil {
			t.Fatalf("Expected a parsable listing query, got %q: %v", query, err)
		}
		if values.Get("limit") != "100" {
			t.Fatalf("Expected listing request %d to ask for limit=100, got %q", i+1, query)
		}
		if values.Get("page") != strconv.Itoa(i+1) {
			t.Fatalf("Expected listing request %d to ask for page=%d, got %q", i+1, i+1, query)
		}
	}
}

// A failed acquire job frames its error like every other verb in the package.
// One shape for one meaning keeps the diagnostics readable.
func TestAcquirePublicIpFramesPollingFailureWithTheActionMockHTTP(t *testing.T) {
	t.Parallel()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/"+testVpcID+"/public-ips":
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"publicIpId": testPublicIpID, "jobId": testJobID},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Job status retrieved",
					"data": map[string]any{"jobs": []map[string]any{{
						"jobId":        testJobID,
						"isCompleted":  false,
						"isTerminal":   true,
						"hasFailed":    true,
						"errorMessage": "No addresses are free in this region",
					}}},
				})
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	_, err := AcquirePublicIp(gpcnClient, context.Background(), testVpcID)
	if err == nil {
		t.Fatalf("Expected an error when the acquire job fails, got none")
	}
	if !strings.HasPrefix(err.Error(), ActionAcquirePublicIp+" polling failed: ") {
		t.Fatalf("Expected the error to start with %q, got %q", ActionAcquirePublicIp+" polling failed: ", err.Error())
	}
	if !strings.Contains(err.Error(), "No addresses are free in this region") {
		t.Fatalf("Expected the platform reason in the error, got %q", err.Error())
	}
}

// An absent address must read like any other not-found. Read can then remove
// the resource from state instead of failing every later operation.
func TestGetPublicIpReturnsNotFoundWhenAbsentMockHTTP(t *testing.T) {
	t.Parallel()
	listPath := "/v1/resource/vpcs/" + testVpcID + "/public-ips"
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != listPath {
				testutil.LogUnexpectedRequest(t, w, r)
				return
			}
			testutil.WriteJSONResponse(w, listBody([]map[string]any{publicIpRow("other-id", nil, nil)}, 1, 1))
		},
	})

	_, err := GetPublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID)
	if err == nil {
		t.Fatalf("Expected an error for an absent address, got none")
	}
	if !client.IsNotFound(err) {
		t.Fatalf("Expected a not-found error, got: %v", err)
	}
}

// A holding answers null for every machine field and for the address it has not
// been given yet. The model must carry those nulls through, because an empty
// string would read as a value the platform never sent.
func TestMapPublicIpResponseToModelKeepsNullsUnit(t *testing.T) {
	t.Parallel()
	publicIp := &PublicIp{
		Id:        testPublicIpID,
		State:     PUBLIC_IP_STATE_ACQUIRING,
		CreatedAt: "2026-01-02T15:04:05Z",
		UpdatedAt: "2026-01-02T15:04:05Z",
	}

	model := MapPublicIpResponseToModel(publicIp, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if !model.IPAddress.IsNull() {
		t.Fatalf("Expected a null ip_address, got %v", model.IPAddress)
	}
	if !model.VirtualMachineID.IsNull() {
		t.Fatalf("Expected a null virtual_machine_id, got %v", model.VirtualMachineID)
	}
	if !model.FailureReason.IsNull() {
		t.Fatalf("Expected a null failure_reason, got %v", model.FailureReason)
	}
	if !model.Held.ValueBool() {
		t.Fatalf("Expected held to be true for an address no machine holds")
	}
	if model.CreatedTime.ValueString() != "Friday, 02-Jan-26 15:04:05 UTC" {
		t.Fatalf("Expected an RFC850 created_time, got %q", model.CreatedTime.ValueString())
	}
}

// An attached address fills the machine id and clears held. The attachment
// resource reads those fields to decide whether its binding still exists.
func TestMapPublicIpResponseToModelFillsAttachedMachineUnit(t *testing.T) {
	t.Parallel()
	address := "203.0.113.10"
	vmID := testVmID
	publicIp := &PublicIp{
		Id:               testPublicIpID,
		IpAddress:        &address,
		State:            PUBLIC_IP_STATE_READY,
		VirtualMachineId: &vmID,
		CreatedAt:        "2026-01-02T15:04:05Z",
		UpdatedAt:        "2026-01-02T15:04:05Z",
	}

	model := MapPublicIpResponseToModel(publicIp, ResourceModel{VpcID: types.StringValue(testVpcID)})

	if model.IPAddress.ValueString() != address {
		t.Fatalf("Expected ip_address %q, got %q", address, model.IPAddress.ValueString())
	}
	if model.VirtualMachineID.ValueString() != vmID {
		t.Fatalf("Expected virtual_machine_id %q, got %q", vmID, model.VirtualMachineID.ValueString())
	}
	if model.Held.ValueBool() {
		t.Fatalf("Expected held to be false for an attached address")
	}
}

// Only the release itself can report that the address is already gone. A 404
// from the job poll is a routing failure, and reporting success there would
// leave a billable address behind.
func TestReleasePublicIpReportsNotFoundFromTheJobPollMockHTTP(t *testing.T) {
	t.Parallel()
	releasePath := "/v1/resource/vpcs/" + testVpcID + "/public-ips/" + testPublicIpID
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete && r.URL.Path == releasePath {
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"jobId": testJobID},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			testutil.WriteJSONResponse(w, map[string]any{
				"success": false,
				"message": "Not Found",
				"error":   map[string]any{"code": "Resource Not Found", "statusCode": 404, "details": nil},
			})
		},
	})

	if err := ReleasePublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID); err == nil {
		t.Fatalf("Expected an error when the job poll answers 404, got none")
	}
}

// A listing that always claims another page must not read as an absent address.
// A not-found there would drop a live address out of state.
func TestGetPublicIpFailsRatherThanReportNotFoundAtThePageCapMockHTTP(t *testing.T) {
	t.Parallel()
	listPath := "/v1/resource/vpcs/" + testVpcID + "/public-ips"
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != listPath {
				testutil.LogUnexpectedRequest(t, w, r)
				return
			}
			body := listBody([]map[string]any{publicIpRow("other-id", nil, nil)}, 1, 2)
			meta, _ := body["meta"].(map[string]any)
			meta["hasNextPage"] = true
			testutil.WriteJSONResponse(w, body)
		},
	})

	_, err := GetPublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID)
	if err == nil {
		t.Fatalf("Expected an error when the listing never ends, got none")
	}
	if client.IsNotFound(err) {
		t.Fatalf("Expected an error that is not a not-found, got: %v", err)
	}
}

// The API inserts the row before it dispatches the acquire job. A failed job
// therefore leaves a real address behind, and the caller needs its id to
// release it.
func TestAcquirePublicIpReturnsTheIdWhenTheJobFailsMockHTTP(t *testing.T) {
	t.Parallel()
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/vpcs/"+testVpcID+"/public-ips":
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Operation initiated successfully",
					"data":    map[string]any{"publicIpId": testPublicIpID, "jobId": testJobID},
				})
			case r.Method == http.MethodPost && r.URL.Path == "/v1/resource/jobs/":
				testutil.WriteJSONResponse(w, map[string]any{
					"success": true,
					"message": "Job status retrieved",
					"data": map[string]any{"jobs": []map[string]any{{
						"jobId":        testJobID,
						"isCompleted":  false,
						"isTerminal":   true,
						"hasFailed":    true,
						"errorMessage": "No addresses are free in this region",
					}}},
				})
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	publicIpID, err := AcquirePublicIp(gpcnClient, context.Background(), testVpcID)
	if err == nil {
		t.Fatalf("Expected an error when the acquire job fails, got none")
	}
	if publicIpID != testPublicIpID {
		t.Fatalf("Expected the failed acquire to still report ID %q, got %q", testPublicIpID, publicIpID)
	}
}

// Create writes the id to state before it polls, so the row is never orphaned
// outside Terraform. Nothing about the address is read yet. A computed
// attribute left unknown fails the apply with a missing value.
func TestMapAcquiredIdToModelNullsEveryComputedAttributeUnit(t *testing.T) {
	t.Parallel()

	// A plan carries Unknown, not null, in every Computed attribute. State.Set
	// refuses an unknown value, so the mapper has to null each one.
	model := MapAcquiredIdToModel(testPublicIpID, ResourceModel{
		VpcID:            types.StringValue(testVpcID),
		IPAddress:        types.StringUnknown(),
		State:            types.StringUnknown(),
		Held:             types.BoolUnknown(),
		VirtualMachineID: types.StringUnknown(),
		FailureReason:    types.StringUnknown(),
		CreatedTime:      types.StringUnknown(),
		LastUpdated:      types.StringUnknown(),
	})

	if got := model.ID.ValueString(); got != testPublicIpID {
		t.Errorf("expected the id %q, got %q", testPublicIpID, got)
	}
	if got := model.VpcID.ValueString(); got != testVpcID {
		t.Errorf("expected the configured VPC %q, got %q", testVpcID, got)
	}

	nulls := map[string]bool{
		"ip_address":         model.IPAddress.IsNull(),
		"state":              model.State.IsNull(),
		"held":               model.Held.IsNull(),
		"virtual_machine_id": model.VirtualMachineID.IsNull(),
		"failure_reason":     model.FailureReason.IsNull(),
		"created_time":       model.CreatedTime.IsNull(),
		"last_updated":       model.LastUpdated.IsNull(),
	}
	for attribute, isNull := range nulls {
		if !isNull {
			t.Errorf("expected %s to be null", attribute)
		}
	}
}

// The attach writes the binding to state before the read-back, so a failed
// read-back leaves an attachment Terraform owns. The address itself is not
// read yet, so the machine it serves stays null.
func TestMapAttachedIdToAttachmentModelNullsEveryComputedAttributeUnit(t *testing.T) {
	t.Parallel()

	// A plan carries Unknown, not null, in every Computed attribute. State.Set
	// refuses an unknown value, so the mapper has to null each one.
	model := MapAttachedIdToAttachmentModel(AttachmentResourceModel{
		ID:               types.StringUnknown(),
		VpcID:            types.StringValue(testVpcID),
		PublicIpID:       types.StringValue(testPublicIpID),
		NicID:            types.StringValue(testNicID),
		VirtualMachineID: types.StringUnknown(),
	})

	if got := model.ID.ValueString(); got != testPublicIpID {
		t.Errorf("expected the id %q, got %q", testPublicIpID, got)
	}
	if got := model.VpcID.ValueString(); got != testVpcID {
		t.Errorf("expected the configured VPC %q, got %q", testVpcID, got)
	}
	if got := model.PublicIpID.ValueString(); got != testPublicIpID {
		t.Errorf("expected the configured address %q, got %q", testPublicIpID, got)
	}
	if got := model.NicID.ValueString(); got != testNicID {
		t.Errorf("expected the configured interface %q, got %q", testNicID, got)
	}
	if !model.VirtualMachineID.IsNull() {
		t.Errorf("expected virtual_machine_id to be null, got %q", model.VirtualMachineID.ValueString())
	}
}
