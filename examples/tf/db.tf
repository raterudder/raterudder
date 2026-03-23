# only the default database supports the free tier
resource "google_firestore_database" "default" {
  project                           = var.project_id
  name                              = "(default)"
  location_id                       = "us-central1"
  type                              = "FIRESTORE_NATIVE"
  database_edition                  = "STANDARD"
  concurrency_mode                  = "OPTIMISTIC"
  app_engine_integration_mode       = "DISABLED"
  point_in_time_recovery_enablement = "POINT_IN_TIME_RECOVERY_ENABLED"
  delete_protection_state           = "DELETE_PROTECTION_ENABLED"

  depends_on = [module.enabled_google_apis]
}

resource "google_project_iam_member" "raterudder_firestore" {
  project = var.project_id
  role    = "roles/datastore.user"
  member  = "serviceAccount:${google_service_account.raterudder.email}"
}

locals {
  collections = [
    "config",
    "action_history",
    "energy_history",
    "price_history",
    "users",
    "sites",
    "feedback",
    "hourly_prices",
  ]
}

resource "google_firestore_field" "json" {
  for_each   = toset(local.collections)
  project    = var.project_id
  database   = google_firestore_database.default.name
  collection = each.value
  field      = "json"

  # disable indexing on json fields
  index_config {}
}

