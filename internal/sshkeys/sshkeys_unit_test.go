package sshkeys

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"terraform-provider-gpcn/internal/testutil"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Test helpers

func newSSHKeyResponse(id, name string) *readSSHKeyResponse {
	resp := &readSSHKeyResponse{}
	resp.Data.ID = id
	resp.Data.Name = name
	resp.Data.CreatedAt = time.Now().Format(time.RFC3339)
	resp.Data.UpdatedAt = time.Now().Format(time.RFC3339)
	return resp
}

func createUploadModel(name, publicKey string) ResourceModel {
	return ResourceModel{
		Name:      types.StringValue(name),
		PublicKey: types.StringValue(publicKey),
	}
}

// Tests for MapSSHKeyResponseToModel

func TestMapSSHKeyResponseToModelSetsBasicFields(t *testing.T) {
	response := newSSHKeyResponse("key-123", "test-key")
	model := createUploadModel("test-key", "ssh-ed25519 AAAA...")

	result := MapSSHKeyResponseToModel(response, model)

	if result.ID.ValueString() != "key-123" {
		t.Errorf("Expected ID 'key-123', got '%s'", result.ID.ValueString())
	}
	if result.Name.ValueString() != "test-key" {
		t.Errorf("Expected Name 'test-key', got '%s'", result.Name.ValueString())
	}
	if result.CreatedTime.IsNull() || result.CreatedTime.ValueString() == "unknown" {
		t.Errorf("Expected CreatedTime to be set, got '%s'", result.CreatedTime.ValueString())
	}
	if result.LastUpdated.IsNull() || result.LastUpdated.ValueString() == "unknown" {
		t.Errorf("Expected LastUpdated to be set, got '%s'", result.LastUpdated.ValueString())
	}
}

func TestMapSSHKeyResponseToModelInvalidTimestamp(t *testing.T) {
	response := newSSHKeyResponse("key-123", "test-key")
	response.Data.CreatedAt = "not-a-timestamp"
	response.Data.UpdatedAt = "also-not-a-timestamp"
	model := createUploadModel("test-key", "ssh-ed25519 AAAA...")

	result := MapSSHKeyResponseToModel(response, model)

	if result.CreatedTime.ValueString() != "unknown" {
		t.Errorf("Expected CreatedTime 'unknown' for invalid timestamp, got '%s'", result.CreatedTime.ValueString())
	}
	if result.LastUpdated.ValueString() != "unknown" {
		t.Errorf("Expected LastUpdated 'unknown' for invalid timestamp, got '%s'", result.LastUpdated.ValueString())
	}
}

// Tests for CreateSSHKey

func TestCreateSSHKeyMockHTTPUploadMode(t *testing.T) {
	const (
		keyID   = "key-012"
		keyName = "uploaded-key"
		pubKey  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI..."
	)

	var createCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/ssh-keys/"):
				createCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)
				if req["type"] != "upload" {
					t.Errorf("Expected type 'upload', got '%v'", req["type"])
				}
				if req["publicKey"] != pubKey {
					t.Errorf("Expected publicKey '%s', got '%v'", pubKey, req["publicKey"])
				}
				testutil.WriteJSONResponse(w, createSSHKeyResponse{
					Data: struct {
						ID string `json:"id"`
					}{ID: keyID},
				})

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/ssh-keys/"+keyID):
				testutil.WriteJSONResponse(w, newSSHKeyResponse(keyID, keyName))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createUploadModel(keyName, pubKey)
	response, err := CreateSSHKey(gpcnClient, context.Background(), model)
	if err != nil {
		t.Fatalf("CreateSSHKey failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != keyID {
		t.Errorf("Expected key ID '%s', got '%s'", keyID, response.Data.ID)
	}
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
}

// Tests for GetSSHKey

func TestGetSSHKeyMockHTTP(t *testing.T) {
	const keyID = "key-get-123"

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.Contains(r.URL.Path, "/ssh-keys/"+keyID) {
				testutil.WriteJSONResponse(w, newSSHKeyResponse(keyID, "test-key"))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	response, err := GetSSHKey(gpcnClient, context.Background(), keyID)
	if err != nil {
		t.Fatalf("GetSSHKey failed: %v", err)
	}
	if response == nil {
		t.Fatal("Expected response, got nil")
		return
	}
	if response.Data.ID != keyID {
		t.Errorf("Expected key ID '%s', got '%s'", keyID, response.Data.ID)
	}
}

// Tests for UpdateSSHKey

func TestUpdateSSHKeyMockHTTP(t *testing.T) {
	const (
		keyID   = "key-update-123"
		newName = "updated-key"
	)

	var updateCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" && strings.Contains(r.URL.Path, "/ssh-keys/"+keyID) {
				updateCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)
				if req["name"] != newName {
					t.Errorf("Expected name '%s', got '%v'", newName, req["name"])
				}
				testutil.WriteJSONResponse(w, map[string]bool{"success": true})
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := UpdateSSHKey(gpcnClient, context.Background(), keyID, newName)
	if err != nil {
		t.Fatalf("UpdateSSHKey failed: %v", err)
	}
	if !updateCalled {
		t.Error("Expected update endpoint to be called")
	}
}

// Tests for DeleteSSHKey

func TestDeleteSSHKeyMockHTTP(t *testing.T) {
	const keyID = "key-delete-123"

	var deleteCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "DELETE" && strings.Contains(r.URL.Path, "/ssh-keys/"+keyID) {
				deleteCalled = true
				w.WriteHeader(http.StatusNoContent)
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := DeleteSSHKey(gpcnClient, context.Background(), keyID)
	if err != nil {
		t.Fatalf("DeleteSSHKey failed: %v", err)
	}
	if !deleteCalled {
		t.Error("Expected delete endpoint to be called")
	}
}
