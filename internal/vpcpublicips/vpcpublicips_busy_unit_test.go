package vpcpublicips

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

func TestAttachPublicIpWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPost, publicIpURL(testVpcID, testPublicIpID)+"/attach", acceptOwnJob, nil)

	if err := AttachPublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID, testNicID); err != nil {
		t.Fatalf("AttachPublicIp = %v, want nil", err)
	}
	recorded.check(t, `{"nicId":"`+testNicID+`"}`, "send", "poll holder-job", "send", "poll own-job")
}

func TestDetachPublicIpWaitsOutBusyHolder(t *testing.T) {
	gpcnClient, recorded := setupBusyOnce(t, http.MethodPost, publicIpURL(testVpcID, testPublicIpID)+"/detach", acceptOwnJob, nil)

	if err := DetachPublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID); err != nil {
		t.Fatalf("DetachPublicIp = %v, want nil", err)
	}
	recorded.check(t, "", "send", "poll holder-job", "send", "poll own-job")
}

func TestReleasePublicIpWaitsOutBusyHolder(t *testing.T) {
	answerNotFound := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}
	tests := []struct {
		name       string
		accepted   http.HandlerFunc
		wantEvents []string
	}{
		{"the second send is accepted", acceptOwnJob, []string{"send", "poll holder-job", "send", "poll own-job"}},
		{"the second send reports the address gone", answerNotFound, []string{"send", "poll holder-job", "send"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpcnClient, recorded := setupBusyOnce(t, http.MethodDelete, publicIpURL(testVpcID, testPublicIpID), tt.accepted, nil)

			if err := ReleasePublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID); err != nil {
				t.Fatalf("ReleasePublicIp = %v, want nil", err)
			}
			recorded.check(t, "", tt.wantEvents...)
		})
	}
}

func TestReleasePublicIpHolderReadNotFoundIsAnError(t *testing.T) {
	_, gpcnClient := testutil.SetupMockServerWithRealTransport(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodDelete && r.URL.Path == publicIpURL(testVpcID, testPublicIpID):
				testutil.HandleBusyResponse(w, "holder-job")
			case r.Method == http.MethodPost && r.URL.Path == client.JOBS_BASE_URL_V1:
				w.WriteHeader(http.StatusNotFound)
			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})

	if err := ReleasePublicIp(gpcnClient, context.Background(), testVpcID, testPublicIpID); err == nil {
		t.Error("ReleasePublicIp = nil, want the error of the holder read")
	}
}
