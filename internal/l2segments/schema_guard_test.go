package l2segments_test

import (
	"context"
	"testing"

	"terraform-provider-gpcn/internal/helpers"
	"terraform-provider-gpcn/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// The shared validator changes nothing until the schema carries it. The
// attachment therefore needs a guard of its own.
func TestL2SegmentResourceSchemaAttachesWhitespaceValidators(t *testing.T) {
	t.Parallel()

	var resp resource.SchemaResponse
	provider.NewL2SegmentResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}

	for _, attributeName := range []string{"name", "description"} {
		attribute, ok := resp.Schema.Attributes[attributeName].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s is %T, want schema.StringAttribute", attributeName, resp.Schema.Attributes[attributeName])
		}
		found := false
		for _, v := range attribute.Validators {
			whitespace, isWhitespace := v.(helpers.NoOuterWhitespaceValidator)
			if isWhitespace && whitespace.Attribute == attributeName && whitespace.Summary == "Invalid L2 segment %s" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s carries %d validators, none of them a NoOuterWhitespaceValidator for %q", attributeName, len(attribute.Validators), attributeName)
		}
	}
}
