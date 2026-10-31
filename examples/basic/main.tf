terraform {
  required_providers {
    cmdash = {
      source = "cmdash/cmdash"
    }
  }
}

provider "cmdash" {
  # api_key / base_url may also come from CMDASH_API_KEY / CMDASH_BASE_URL.
  api_key = var.cmdash_api_key
}

variable "cmdash_api_key" {
  type      = string
  sensitive = true
}

resource "cmdash_webhook_registration" "posture_alerts" {
  url    = "https://example.com/hooks/cmdash"
  events = ["posture.changed", "risk.created"]
}

data "cmdash_compliance_frameworks" "all" {}

data "cmdash_integration_connections" "all" {}

output "webhook_id" {
  value = cmdash_webhook_registration.posture_alerts.id
}

output "webhook_secret" {
  value     = cmdash_webhook_registration.posture_alerts.secret
  sensitive = true
}
