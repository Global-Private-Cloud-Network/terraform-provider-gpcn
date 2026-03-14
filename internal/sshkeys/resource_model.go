package sshkeys

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Algorithm   types.String `tfsdk:"algorithm"`
	Passphrase  types.String `tfsdk:"passphrase"`
	PrivateKey  types.String `tfsdk:"private_key"`
	CreatedTime types.String `tfsdk:"created_time"`
	LastUpdated types.String `tfsdk:"last_updated"`
}

// MapSSHKeyResponseToModel updates the model with values from a GET response.
// If the response includes a private_key, it is set on the model. Otherwise the
// existing model value is preserved (private keys are only returned at creation time).
func MapSSHKeyResponseToModel(response *readSSHKeyResponse, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.ID)
	model.Name = types.StringValue(response.Data.Name)
	model.Algorithm = types.StringValue(response.Data.Algorithm)

	createdTime, err := time.Parse(time.RFC3339, response.Data.CreatedAt)
	if err != nil {
		model.CreatedTime = types.StringValue("unknown")
	} else {
		model.CreatedTime = types.StringValue(createdTime.Format(time.RFC850))
	}

	updatedTime, err := time.Parse(time.RFC3339, response.Data.UpdatedAt)
	if err != nil {
		model.LastUpdated = types.StringValue("unknown")
	} else {
		model.LastUpdated = types.StringValue(updatedTime.Format(time.RFC850))
	}

	// Only overwrite private_key if the response includes one.
	// The API only returns the private key immediately after creation (generate mode).
	// Subsequent GETs will not include it, so we preserve whatever is already in state.
	if response.Data.PrivateKey != "" {
		model.PrivateKey = types.StringValue(response.Data.PrivateKey)
	}

	return model
}
