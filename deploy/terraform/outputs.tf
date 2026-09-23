output "artifact_registry" {
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.aquila.repository_id}"
  description = "Push aquila-server here, then set var.image to that digest."
}

output "cloud_run_uri" {
  value       = google_cloud_run_v2_service.api.uri
  description = "Internal-only URI. There is no allUsers binding."
}

output "sql_connection_name" {
  value       = google_sql_database_instance.aquila.connection_name
  description = "project:region:instance for the Cloud SQL Unix socket."
}

output "service_account" {
  value = google_service_account.api.email
}
