variable "project_id" {
  type        = string
  description = "GCP project that will host Aquila. This module does not create the project."
}

variable "region" {
  type        = string
  description = "Region for Cloud SQL, Artifact Registry, and Cloud Run."
  default     = "us-central1"
}

variable "image" {
  type        = string
  description = "aquila-server container image already pushed to Artifact Registry or another registry Cloud Run can pull."
}

variable "api_token" {
  type        = string
  sensitive   = true
  description = "Bearer token for /v1 and /metrics. Required. Do not reuse the local Compose ingest token."
}

variable "ingest_token" {
  type        = string
  sensitive   = true
  description = "X-Aquila-Ingest-Token for POST /v1/traces."
}

variable "db_tier" {
  type        = string
  description = "Cloud SQL machine type. db-f1-micro is the intern floor, not HA."
  default     = "db-f1-micro"
}

variable "deletion_protection" {
  type        = bool
  description = "Cloud SQL deletion protection. Leave true once there is real data."
  default     = true
}
