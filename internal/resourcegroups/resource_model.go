package resourcegroups

import (
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	CreatedTime types.String `tfsdk:"created_time"`
	LastUpdated types.String `tfsdk:"last_updated"`
}

// MapResourceGroupResponseToModel updates the model with values from a GET response.
func MapResourceGroupResponseToModel(response *readResourceGroupResponse, model ResourceModel) ResourceModel {
	model.ID = types.StringValue(response.Data.ID)
	model.Name = types.StringValue(response.Data.Name)

	if response.Data.Description != "" {
		model.Description = types.StringValue(response.Data.Description)
	} else {
		model.Description = types.StringNull()
	}

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
