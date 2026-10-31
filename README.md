# terraform-provider-cmdash

The Terraform provider for [CMDash](https://www.cmdash.com). It manages outbound webhook
registrations (`cmdash_webhook_registration`, with import) and reads compliance frameworks,
framework controls and integration connections as data sources, all through the CMDash public
API v2 with a `cmdash_ek_...` API key.

Licensed under the Apache License 2.0; see `LICENSE` and `NOTICE`. The CMDash platform the
provider talks to is a commercial service and is not covered by this licence.

## Status

The provider is not on the Terraform Registry yet: registry publishing is pending (it needs a
signing key and the registry namespace). Until then, install it from source.

## Install from source

Requires Go 1.25 or later.

```sh
git clone https://github.com/bgovanlu/terraform-provider-cmdash.git
cd terraform-provider-cmdash
go build -o terraform-provider-cmdash .
```

Point Terraform at the binary with a `~/.terraformrc` `dev_overrides` block:

```hcl
provider_installation {
  dev_overrides {
    "cmdash/cmdash" = "/path/to/terraform-provider-cmdash"
  }
  direct {}
}
```

`terraform plan` and `terraform apply` then use the local binary without `terraform init`.
Terraform prints a warning that `dev_overrides` is active; that is expected.

## Configure

```hcl
terraform {
  required_providers {
    cmdash = {
      source = "cmdash/cmdash"
    }
  }
}

provider "cmdash" {
  # Or set CMDASH_API_KEY and CMDASH_BASE_URL.
  api_key  = var.cmdash_api_key
  base_url = "https://app.cmdash.com/api"
}
```

A complete example is in `examples/basic/main.tf`.

## Test

```sh
go test ./...                              # unit tests, no network
TF_ACC=1 go test ./internal/provider/...   # acceptance tests against an in-process fake
```

## Reporting a security issue

Email security@cmdash.com. Do not open a public issue for a vulnerability.
