package sshkeys

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	CreatedTime types.String `tfsdk:"created_time"`
	LastUpdated types.String `tfsdk:"last_updated"`
}

// MapSSHKeyResponseToModel updates the model with values from a GET response.
func MapSSHKeyResponseToModel(response *readSSHKeyResponse, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.ID)
	model.Name = types.StringValue(response.Data.Name)
	model.PublicKey = types.StringValue(response.Data.PublicKey)

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

	return model
}
