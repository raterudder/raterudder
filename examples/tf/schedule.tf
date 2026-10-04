resource "google_cloud_scheduler_job" "raterudder_update_sites_tou_1" {
  name        = "raterudder-update-sites-tou-1"
  description = "Triggers the /api/updateSites endpoint for TOU partition 1 of 2 every 20 minutes at :00, :20, :40"
  schedule    = "0,20,40 * * * *"
  time_zone   = "America/Chicago"
  region      = "us-central1"
  project     = var.project_id
  paused      = !var.schedule_enabled
  # this just needs to be larger than the run service timeout
  attempt_deadline = "90s"

  http_target {
    http_method = "POST"
    uri         = "${local.run_deterministic_uri}/api/updateSites?kind=tou&idx=0&total=2"

    oidc_token {
      service_account_email = google_service_account.raterudder.email
      audience              = local.run_deterministic_uri
    }
  }
}

resource "google_cloud_scheduler_job" "raterudder_update_sites_tou_2" {
  name        = "raterudder-update-sites-tou-2"
  description = "Triggers the /api/updateSites endpoint for TOU partition 2 of 2 every 20 minutes at :02, :22, :42"
  schedule    = "2,22,42 * * * *"
  time_zone   = "America/Chicago"
  region      = "us-central1"
  project     = var.project_id
  paused      = !var.schedule_enabled
  # this just needs to be larger than the run service timeout
  attempt_deadline = "90s"

  http_target {
    http_method = "POST"
    uri         = "${local.run_deterministic_uri}/api/updateSites?kind=tou&idx=1&total=2"

    oidc_token {
      service_account_email = google_service_account.raterudder.email
      audience              = local.run_deterministic_uri
    }
  }
}

resource "google_cloud_scheduler_job" "raterudder_update_sites_comed" {
  name        = "raterudder-update-sites-comed"
  description = "Triggers the /api/updateSites endpoint for ComEd Hourly every 20 minutes after 5-minute price publication at :07, :27, :47"
  schedule    = "7,27,47 * * * *"
  time_zone   = "America/Chicago"
  region      = "us-central1"
  project     = var.project_id
  paused      = !var.schedule_enabled
  # this just needs to be larger than the run service timeout
  attempt_deadline = "90s"

  http_target {
    http_method = "POST"
    uri         = "${local.run_deterministic_uri}/api/updateSites?kind=comed&idx=0&total=1"

    oidc_token {
      service_account_email = google_service_account.raterudder.email
      audience              = local.run_deterministic_uri
    }
  }
}
