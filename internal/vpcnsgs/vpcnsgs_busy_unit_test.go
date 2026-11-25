package vpcnsgs

import (
	"context"
	"io"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/client"
	"terraform-provider-gpcn/internal/testutil"
)

// busyOnce records one function call whose first send finds a holder.
type busyOnce struct {
	mu     sync.Mutex
	events []string
	bodies []string
}

// setupBusyOnce answers the named request busy once and then with accepted. Every
// job ends with success. Every other request goes to other.
func setupBusyOnce(t *testing.T, method, path string, accepted, other http.HandlerFunc) (*client.GpcnClient, *busyOnce) {
	t.Helper()

	previous := BusyRetryInterval
	BusyRetryInterval = time.Millisecond
	t.Cleanup(func() { BusyRetryInterval = previous })

	recorded := &busyOnce{}
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			recorded.mu.Lock()
			defer recorded.mu.Unlock()
			switch {
			case r.Method == method && r.URL.Path == path:
				raw, _ := io.ReadAll(r.Body)
				recorded.bodies = append(recorded.bodies, string(raw))
				recorded.events = append(recorded.events, "send")
				if len(recorded.bodies) == 1 {
					testutil.HandleBusyResponse(w, "holder-job")
					return
				}
				accepted(w, r)
			case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
				ids, _ := testutil.ReadRequestBody(r)["jobIds"].([]any)
				jobID, _ := ids[0].(string)
				recorded.events = append(recorded.events, "poll "+jobID)
				testutil.HandleJobResponse(w, jobID, "", true)
			case other != nil:
				recorded.events = append(recorded.events, r.Method+" "+r.URL.Path)
				other(w, r)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	return gpcnClient, recorded
}

func (b *busyOnce) check(t *testing.T, wantBody string, wantEvents ...string) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.bodies) != 2 {
		t.Fatalf("sends = %d, want 2", len(b.bodies))
	}
	if b.bodies[1] != wantBody {
		t.Errorf("second body = %q, want %q", b.bodies[1], wantBody)
	}
	if !slices.Equal(b.events, wantEvents) {
		t.Errorf("events = %q, want %q", b.events, wantEvents)
	}
}

func acceptOwnJob(w http.ResponseWriter, _ *http.Request) {
	testutil.HandleCreateJobResponse(w, "own-job")
}

func acceptSync(w http.ResponseWriter, _ *http.Request) {
	testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{}})
}

func TestRenameNsgWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, nsgPath(unitTestVpcID, unitTestNsgID), acceptSync, nil)

	if err := RenameNsg(gpcnClient, context.Background(), unitTestVpcID, unitTestNsgID, "renamed", "text"); err != nil {
		t.Fatalf("RenameNsg = %v, want nil", err)
	}
	recorded.check(t, `{"description":"text","name":"renamed"}`, "send", "poll holder-job", "send")
}

// The holder adds a VPN-owned rule between the two sends, so only a body built
// again for the second send carries it.
func TestReplaceNsgRulesWaitsOutBusyHolder(t *testing.T) {
	groupPath := nsgPath(unitTestVpcID, unitTestNsgID)
	reads := 0
	readGroup := func(w http.ResponseWriter, _ *http.Request) {
		reads++
		rules := []map[string]any{}
		if reads > 1 {
			rules = append(rules, map[string]any{
				"id": "rule-vpn", "direction": "ingress", "protocol": "all", "portRangeMin": nil, "portRangeMax": nil,
				"remoteCidr": "10.9.0.0/16", "description": nil, "vpnConnectionId": "conn-1",
			})
		}
		testutil.WriteJSONResponse(w, map[string]any{"success": true, "data": map[string]any{
			"nsg": map[string]any{"id": unitTestNsgID}, "rules": rules,
		}})
	}
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPut, groupPath+rulesPathSegment, acceptOwnJob, readGroup)

	after, diags, err := ReplaceNsgRules(gpcnClient, context.Background(), unitTestVpcID, unitTestNsgID, nil)
	if err != nil || diags.HasError() || len(diags) != 0 {
		t.Fatalf("ReplaceNsgRules = %v with %v, want no error and no diagnostics", err, diags)
	}
	if after == nil || len(after.Rules) != 1 {
		t.Errorf("after = %+v, want the group with the VPN-owned rule", after)
	}
	recorded.check(t, `{"rules":[{"direction":"ingress","protocol":"all","remoteCidr":"10.9.0.0/16"}]}`,
		"GET "+groupPath, "send", "poll holder-job", "GET "+groupPath, "send", "poll own-job", "GET "+groupPath)
}

func TestDeleteNsgWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodDelete, nsgPath(unitTestVpcID, unitTestNsgID), acceptOwnJob, nil)

	if err := DeleteNsg(gpcnClient, context.Background(), unitTestVpcID, unitTestNsgID); err != nil {
		t.Fatalf("DeleteNsg = %v, want nil", err)
	}
	recorded.check(t, "", "send", "poll holder-job", "send", "poll own-job")
}
