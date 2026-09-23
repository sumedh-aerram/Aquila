resource "google_project_service" "services" {
  for_each = toset([
    "run.googleapis.com",
    "sqladmin.googleapis.com",
    "artifactregistry.googleapis.com",
    "secretmanager.googleapis.com",
    "iam.googleapis.com",
  ])
  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "aquila" {
  location      = var.region
  repository_id = "aquila"
  description   = "Aquila control-plane images"
  format        = "DOCKER"
  depends_on    = [google_project_service.services]
}

resource "random_password" "db" {
  length  = 24
  special = false
}

resource "google_sql_database_instance" "aquila" {
  name                = "aquila"
  database_version    = "POSTGRES_16"
  region              = var.region
  deletion_protection = var.deletion_protection

  settings {
    tier              = var.db_tier
    availability_type = "ZONAL"
    disk_autoresize   = true
    backup_configuration {
      enabled = true
    }
    ip_configuration {
      ipv4_enabled = true
    }
  }

  depends_on = [google_project_service.services]
}

resource "google_sql_database" "aquila" {
  name     = "aquila"
  instance = google_sql_database_instance.aquila.name
}

resource "google_sql_user" "aquila" {
  name     = "aquila"
  instance = google_sql_database_instance.aquila.name
  password = random_password.db.result
}

resource "google_secret_manager_secret" "api_token" {
  secret_id = "aquila-api-token"
  replication {
    auto {}
  }
  depends_on = [google_project_service.services]
}

resource "google_secret_manager_secret_version" "api_token" {
  secret      = google_secret_manager_secret.api_token.id
  secret_data = var.api_token
}

resource "google_secret_manager_secret" "ingest_token" {
  secret_id = "aquila-ingest-token"
  replication {
    auto {}
  }
  depends_on = [google_project_service.services]
}

resource "google_secret_manager_secret_version" "ingest_token" {
  secret      = google_secret_manager_secret.ingest_token.id
  secret_data = var.ingest_token
}

resource "google_secret_manager_secret" "postgres_url" {
  secret_id = "aquila-postgres-url"
  replication {
    auto {}
  }
  depends_on = [google_project_service.services]
}

resource "google_secret_manager_secret_version" "postgres_url" {
  secret      = google_secret_manager_secret.postgres_url.id
  secret_data = "postgres://${google_sql_user.aquila.name}:${random_password.db.result}@/${google_sql_database.aquila.name}?host=/cloudsql/${google_sql_database_instance.aquila.connection_name}&sslmode=disable"
}

resource "google_service_account" "api" {
  account_id   = "aquila-api"
  display_name = "Aquila control plane"
}

resource "google_project_iam_member" "api_sql" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "api_secrets" {
  for_each = {
    api      = google_secret_manager_secret.api_token.secret_id
    ingest   = google_secret_manager_secret.ingest_token.secret_id
    postgres = google_secret_manager_secret.postgres_url.secret_id
  }
  secret_id = each.value
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_cloud_run_v2_service" "api" {
  name     = "aquila-api"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_INTERNAL_ONLY"

  template {
    service_account = google_service_account.api.email
    scaling {
      max_instance_count = 2
    }
    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.aquila.connection_name]
      }
    }
    containers {
      image = var.image
      ports {
        container_port = 8080
      }
      env {
        name  = "PORT"
        value = "8080"
      }
      env {
        name  = "AQUILA_SERVER_ADDR"
        value = ":8080"
      }
      env {
        name  = "AQUILA_WORKER_ADDR"
        value = "127.0.0.1:8091"
      }
      env {
        name  = "AQUILA_SOURCE_SNAPSHOT"
        value = "/etc/aquila/source.json"
      }
      env {
        name = "AQUILA_API_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.api_token.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "AQUILA_INGEST_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.ingest_token.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "AQUILA_POSTGRES_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.postgres_url.secret_id
            version = "latest"
          }
        }
      }
      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }
      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }
      startup_probe {
        http_get {
          path = "/healthz"
        }
        period_seconds    = 5
        failure_threshold = 12
      }
      liveness_probe {
        http_get {
          path = "/healthz"
        }
      }
    }
  }

  depends_on = [
    google_project_iam_member.api_sql,
    google_secret_manager_secret_iam_member.api_secrets,
    google_sql_database.aquila,
  ]
}
