# GPCN Terraform Provider

This repository contains a Terraform provider used to provision GPCN resources. Example tf files can be found in the `examples` directory.

## Environment Setup

This provider is still in development, so you will need to configure your provider to build it locally.

Install [go](https://go.dev/) and [terraform](https://developer.hashicorp.com/terraform/install).

In the root of this project, run `go mod tidy` and `go install .` This will install your terraform provider locally.

Run the command `go env GOBIN`. This will output your local Go binary installation directory. If you've just installed Go for the first time, this will likely be unchanged at `~/go/bin`. Create a file named `.terraformrc` here. In it, you will add the following lines:

```
provider_installation {
  dev_overrides {
      "gpcn.com/dev/gpcn"   = "<<output of go env GOBIN>>"
  }

  # For all other providers, install them directly from their origin provider
  # registries as normal. If you omit this, Terraform will _only_ use
  # the dev_overrides block, and so no other providers will be available.
  direct {}
}
```

You should also change the required_providers block in the resource.tf file you're testing to

```
terraform {
  required_providers {
    gpcn = {
      source  = "gpcn.com/dev/gpcn"
    }
  }
}
```

This means when you run the provider examples locally and access the GPCN provider, it will use the compiled binary from your local GOBIN directory.

An API key (GPCN_API_KEY) and address of the base URL (GPCN_HOST) must be exposed as environment variables to run the provider or any associated resource.tf files. They can also optionally be passed in as parameters to the `provider "gpcn" {}` block in resource.tf, but it is considered better practice to expose them at runtime through the environment.

## Examples

Examples can be found in the `examples` directory. You can create new resources through `terraform plan` and `terraform apply` and destroy them with `terraform destroy`. You can also optionally create a resource outside of Terraform and import it with `terraform import <resource_name> <id>`. More information can be found on [Hashicorp's website](https://developer.hashicorp.com/terraform) for instructions on using terraform providers.

## Testing

Testing can be done by running `make testacc` in the root of this project using [make](https://www.gnu.org/software/make/manual/html_node/Running.html). You can run an individual test by running `make testaccnamed TEST={test_name}` instead. Depending on the test, you may want to increase the default timeout of 10m via the `-timeout 60m` flag, where 60m corresponds to 60 minutes. These are _acceptance_ tests so they will actually create and destroy resources and require the correct environment variables to be set for the target environment. This will take a long time. Terraform testing framework automatically destroys resources after running, but you should still exercise caution when running the full test suite. More information on Terraform acceptance tests can be found [here](https://developer.hashicorp.com/terraform/plugin/sdkv2/testing/acceptance-tests).
