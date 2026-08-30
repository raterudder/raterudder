terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "7.17.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "7.17.0"
    }
  }
}

provider "google" {
  project = var.project_id
}


variable "project_id" {
  description = "gcp project ID"
  default     = "YOUR_PROJECT_ID"
}

data "google_project" "raterudder" {
  project_id = var.project_id
}

variable "schedule_enabled" {
  description = "Whether the scheduler is enabled"
  type        = bool
  default     = true
}


variable "github_connection_name" {
  description = "Name of GitHub connection on https://console.cloud.google.com/cloud-build/repositories/2nd-gen"
  default = ""
}

variable "repository_name" {
  description = "Name of repository on https://console.cloud.google.com/cloud-build/repositories/2nd-gen"
  default = "raterudder-raterudder"
}

module "enabled_google_apis" {
  source  = "terraform-google-modules/project-factory/google//modules/project_services"
  version = "~> 18.1"

  project_id                  = var.project_id
  disable_services_on_destroy = false

  activate_apis = [
    "run.googleapis.com",
    "iam.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
    "cloudtrace.googleapis.com",
    "cloudbuild.googleapis.com",
    "storage.googleapis.com",
    "containerregistry.googleapis.com",
    "containeranalysis.googleapis.com",
    "artifactregistry.googleapis.com",
    "compute.googleapis.com",
    "developerconnect.googleapis.com",
    "cloudscheduler.googleapis.com",
    "firestore.googleapis.com",
    "secretmanager.googleapis.com",
  ]
}
