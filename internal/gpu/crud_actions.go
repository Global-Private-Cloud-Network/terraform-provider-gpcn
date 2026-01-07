package gpu

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Response structures for API calls
type readGPUResponse struct {
	Data struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		CreatedAt  string `json:"created_at"`
		UpdatedAt  string `json:"updated_at"`
		Status     string `json:"status"`
		Datacenter struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Region      string `json:"region"`
			CountryAbbr string `json:"countryAbbr"`
			Country     string `json:"country"`
		} `json:"datacenter"`
	} `json:"data"`
}

func CreateGPU(client *http.Client, ctx context.Context, plan ResourceModel) (*readGPUResponse, error) {
	tflog.Info(ctx, LogStartingCreateGPU)

	// TODO: Implement me
	response := &readGPUResponse{}

	tflog.Info(ctx, LogSuccessfullyFinishedCreateGPU)
	return response, nil
}

func GetGPU(client *http.Client, ctx context.Context, id string) (*readGPUResponse, error) {
	tflog.Info(ctx, LogStartingReadGPU)

	// TODO: Implement me
	response := &readGPUResponse{}

	tflog.Info(ctx, LogSuccessfullyFinishedReadGPU)
	return response, nil
}

func UpdateGPU(client *http.Client, ctx context.Context, id string, plan ResourceModel) error {
	tflog.Info(ctx, LogStartingUpdateGPU)

	// TODO: Implement me

	tflog.Info(ctx, LogSuccessfullyFinishedUpdateGPU)
	return nil
}

func DeleteGPU(client *http.Client, ctx context.Context, id string) error {
	tflog.Info(ctx, LogStartingDeleteGPU)

	// TODO: Implement me

	tflog.Info(ctx, LogSuccessfullyFinishedDeleteGPU)
	return nil
}
