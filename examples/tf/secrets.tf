resource "google_secret_manager_secret" "raterudder_secrets" {
  project   = var.project_id
  secret_id = "raterudder-secrets"

  replication {
    auto {}
  }

  depends_on = [module.enabled_google_apis]
}

resource "google_secret_manager_secret_iam_member" "raterudder_accessor" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.raterudder_secrets.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.raterudder.email}"
}

resource "google_secret_manager_secret" "raterudder_credentials_encryption_key" {
  project   = var.project_id
  secret_id = "raterudder-credentials-encryption-key"

  replication {
    auto {}
  }

  depends_on = [module.enabled_google_apis]
}

resource "google_secret_manager_secret_iam_member" "raterudder_credentials_encryption_key_accessor" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.raterudder_credentials_encryption_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.raterudder.email}"
}
