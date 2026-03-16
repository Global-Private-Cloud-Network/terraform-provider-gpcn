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

func newSSHKeyResponse(id, name, privateKey string) *readSSHKeyResponse {
	resp := &readSSHKeyResponse{}
	resp.Data.ID = id
	resp.Data.Name = name
	resp.Data.CreatedAt = time.Now().Format(time.RFC3339)
	resp.Data.UpdatedAt = time.Now().Format(time.RFC3339)
	resp.Data.Algorithm = "ed25519"
	resp.Data.PrivateKey = privateKey
	return resp
}

func newAlgorithmsResponse(codes []string) algorithmsResponse {
	entries := make([]algorithmEntry, len(codes))
	for i, code := range codes {
		entries[i] = algorithmEntry{Code: code}
	}
	return algorithmsResponse{Data: entries}
}

func createGenerateModel(name, algorithm, passphrase string) ResourceModel {
	model := ResourceModel{
		Name:      types.StringValue(name),
		PublicKey: types.StringNull(),
		Algorithm: types.StringValue(algorithm),
	}
	if passphrase != "" {
		model.Passphrase = types.StringValue(passphrase)
	} else {
		model.Passphrase = types.StringNull()
	}
	model.PrivateKey = types.StringNull()
	return model
}

func createUploadModel(name, publicKey string) ResourceModel {
	return ResourceModel{
		Name:       types.StringValue(name),
		PublicKey:  types.StringValue(publicKey),
		Algorithm:  types.StringNull(),
		Passphrase: types.StringNull(),
		PrivateKey: types.StringNull(),
	}
}

// Tests for MapSSHKeyResponseToModel

func TestMapSSHKeyResponseToModelSetsBasicFields(t *testing.T) {
	response := newSSHKeyResponse("key-123", "test-key", "private-key-data")
	model := createGenerateModel("test-key", "ed25519", "")

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

func TestMapSSHKeyResponseToModelSetsPrivateKeyWhenPresent(t *testing.T) {
	response := newSSHKeyResponse("key-123", "test-key", "-----BEGIN OPENSSH PRIVATE KEY-----")
	model := createGenerateModel("test-key", "ed25519", "")

	result := MapSSHKeyResponseToModel(response, model)

	if result.PrivateKey.ValueString() != "-----BEGIN OPENSSH PRIVATE KEY-----" {
		t.Errorf("Expected PrivateKey to be set, got '%s'", result.PrivateKey.ValueString())
	}
}

func TestMapSSHKeyResponseToModelPreservesPrivateKeyWhenNotInResponse(t *testing.T) {
	// Response without private key (as returned on subsequent GETs)
	response := newSSHKeyResponse("key-123", "test-key", "")
	model := createGenerateModel("test-key", "ed25519", "")
	model.PrivateKey = types.StringValue("existing-private-key")

	result := MapSSHKeyResponseToModel(response, model)

	if result.PrivateKey.ValueString() != "existing-private-key" {
		t.Errorf("Expected PrivateKey to be preserved, got '%s'", result.PrivateKey.ValueString())
	}
}

func TestMapSSHKeyResponseToModelInvalidTimestamp(t *testing.T) {
	response := newSSHKeyResponse("key-123", "test-key", "")
	response.Data.CreatedAt = "not-a-timestamp"
	response.Data.UpdatedAt = "also-not-a-timestamp"
	model := createGenerateModel("test-key", "ed25519", "")

	result := MapSSHKeyResponseToModel(response, model)

	if result.CreatedTime.ValueString() != "unknown" {
		t.Errorf("Expected CreatedTime 'unknown' for invalid timestamp, got '%s'", result.CreatedTime.ValueString())
	}
	if result.LastUpdated.ValueString() != "unknown" {
		t.Errorf("Expected LastUpdated 'unknown' for invalid timestamp, got '%s'", result.LastUpdated.ValueString())
	}
}

// Tests for CheckAlgorithms

func TestCheckAlgorithmsMockHTTPSupported(t *testing.T) {
	var algorithmsCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/ssh-keys/algorithms") {
				algorithmsCalled = true
				testutil.WriteJSONResponse(w, newAlgorithmsResponse(SupportedAlgorithms))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := CheckAlgorithms(gpcnClient, context.Background(), "ed25519")
	if err != nil {
		t.Fatalf("CheckAlgorithms failed unexpectedly: %v", err)
	}
	if !algorithmsCalled {
		t.Error("Expected algorithms endpoint to be called")
	}
}

func TestCheckAlgorithmsMockHTTPUnsupported(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/ssh-keys/algorithms") {
				testutil.WriteJSONResponse(w, newAlgorithmsResponse([]string{"ed25519", "rsa-2048"}))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := CheckAlgorithms(gpcnClient, context.Background(), "rsa-4096")
	if err == nil {
		t.Fatal("Expected error for unsupported algorithm, got nil")
	}
	if !strings.Contains(err.Error(), "rsa-4096") {
		t.Errorf("Expected error to mention 'rsa-4096', got '%s'", err.Error())
	}
}

func TestCheckAlgorithmsMockHTTPEmptyResponse(t *testing.T) {
	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/ssh-keys/algorithms") {
				testutil.WriteJSONResponse(w, newAlgorithmsResponse([]string{}))
			} else {
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	err := CheckAlgorithms(gpcnClient, context.Background(), "ed25519")
	if err == nil {
		t.Fatal("Expected error for empty algorithms response, got nil")
	}
	if !strings.Contains(err.Error(), ErrDetailMalformedAlgorithmsResponse) {
		t.Errorf("Expected error '%s', got '%s'", ErrDetailMalformedAlgorithmsResponse, err.Error())
	}
}

// Tests for CreateSSHKey

func TestCreateSSHKeyMockHTTPGenerateMode(t *testing.T) {
	const (
		keyID   = "key-456"
		keyName = "test-key"
		privKey = "-----BEGIN OPENSSH PRIVATE KEY-----"
	)

	var createCalled, getKeyCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/ssh-keys/"):
				createCalled = true
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				_ = json.Unmarshal(body, &req)
				if req["type"] != "generate" {
					t.Errorf("Expected type 'generate', got '%v'", req["type"])
				}
				if req["algorithm"] != "ed25519" {
					t.Errorf("Expected algorithm 'ed25519', got '%v'", req["algorithm"])
				}
				if req["name"] != keyName {
					t.Errorf("Expected name '%s', got '%v'", keyName, req["name"])
				}
				testutil.WriteJSONResponse(w, createSSHKeyResponse{
					Data: struct {
						ID         string `json:"id"`
						PrivateKey string `json:"privateKey"`
					}{ID: keyID, PrivateKey: privKey},
				})

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/ssh-keys/"+keyID):
				getKeyCalled = true
				testutil.WriteJSONResponse(w, newSSHKeyResponse(keyID, keyName, ""))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createGenerateModel(keyName, "ed25519", "")
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
	if response.Data.PrivateKey != privKey {
		t.Errorf("Expected PrivateKey '%s', got '%s'", privKey, response.Data.PrivateKey)
	}
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
	if !getKeyCalled {
		t.Error("Expected GET key endpoint to be called")
	}
}

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
				req := testutil.ReadRequestBody(r)
				if req["type"] != "upload" {
					t.Errorf("Expected type 'upload', got '%v'", req["type"])
				}
				if req["publicKey"] != pubKey {
					t.Errorf("Expected publicKey '%s', got '%v'", pubKey, req["publicKey"])
				}
				if _, hasAlgorithm := req["algorithm"]; hasAlgorithm {
					t.Error("Expected algorithm to not be set in upload mode")
				}
				testutil.WriteJSONResponse(w, createSSHKeyResponse{
					Data: struct {
						ID         string `json:"id"`
						PrivateKey string `json:"privateKey"`
					}{ID: keyID},
				})

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/ssh-keys/"+keyID):
				testutil.WriteJSONResponse(w, newSSHKeyResponse(keyID, keyName, ""))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createUploadModel(keyName, pubKey)
	response, err := CreateSSHKey(gpcnClient, context.Background(), model)
	if err != nil {
		t.Fatalf("CreateSSHKey (upload mode) failed: %v", err)
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
				testutil.WriteJSONResponse(w, newSSHKeyResponse(keyID, "test-key", ""))
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

// Tests for CreateSSHKey with passphrase

func TestCreateSSHKeyMockHTTPGenerateModeWithPassphrase(t *testing.T) {
	const (
		keyID      = "key-passcode-456"
		keyName    = "secure-key"
		passphrase = "my-secret"
	)

	var createCalled bool

	server, gpcnClient := testutil.SetupMockServerWithGpcnClient(testutil.MockServerConfig{
		T: t,
		Handler: func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/ssh-keys/"):
				createCalled = true
				req := testutil.ReadRequestBody(r)
				if req["passphrase"] != passphrase {
					t.Errorf("Expected passphrase '%s', got '%v'", passphrase, req["passphrase"])
				}
				testutil.WriteJSONResponse(w, createSSHKeyResponse{
					Data: struct {
						ID         string `json:"id"`
						PrivateKey string `json:"privateKey"`
					}{ID: keyID, PrivateKey: "private-key"},
				})

			case r.Method == "GET" && strings.Contains(r.URL.Path, "/ssh-keys/"+keyID):
				testutil.WriteJSONResponse(w, newSSHKeyResponse(keyID, keyName, ""))

			default:
				testutil.LogUnexpectedRequest(t, w, r)
			}
		},
	})
	defer server.Close()

	model := createGenerateModel(keyName, "rsa-4096", passphrase)
	_, err := CreateSSHKey(gpcnClient, context.Background(), model)
	if err != nil {
		t.Fatalf("CreateSSHKey with passphrase failed: %v", err)
	}
	if !createCalled {
		t.Error("Expected create endpoint to be called")
	}
}
