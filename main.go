// SPDX-License-Identifier: Apache-2.0

// Terraform provider for CMDash tenant configuration. ROADMAP-V70 E31.
// See docs/terraform-provider.md (repo root) for install/dev_overrides
// instructions and the resource/data-source rationale table.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/bgovanlu/terraform-provider-cmdash/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is overridden at build time via:
//
//	go build -ldflags "-X main.version=1.0.0"
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/cmdash/cmdash",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err)
	}
}
