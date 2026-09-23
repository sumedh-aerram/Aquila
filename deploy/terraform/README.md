# Aquila on GCP

This module is the production-shaped deploy path: Artifact Registry, Cloud SQL
Postgres 16, Secret Manager, and Cloud Run for `aquila-server`. It does **not**
deploy `examples/shop`, Grafana, or a public load balancer. It has **not** been
applied from this repository.

## What you still do

1. Create a GCP project and enable billing.
2. `gcloud auth application-default login`
3. Push an image built from the repo `Dockerfile`:

   ```bash
   docker build -t IMAGE --build-arg VERSION=$(git describe --tags --always) .
   docker push IMAGE
   ```

4. Copy `terraform.tfvars.example` to `terraform.tfvars` (gitignored if you
   keep secrets there). Pass tokens on the CLI instead of committing them:

   ```bash
   terraform -chdir=deploy/terraform init
   terraform -chdir=deploy/terraform plan \
     -var='project_id=YOUR_PROJECT' \
     -var='image=REGION-docker.pkg.dev/PROJECT/aquila/api:TAG' \
     -var='api_token=...' \
     -var='ingest_token=...'
   ```

5. Apply only when you intend to create billable resources. Destroy is not
   automatic.

6. Run migrations against the instance (Cloud SQL Auth Proxy + `make migrate`
   with `AQUILA_POSTGRES_URL` pointing at the proxy). This module does not run
   `migrate` for you.

Cloud Run ingress is `INGRESS_TRAFFIC_INTERNAL_ONLY` and there is no
`allUsers` binding. Reach it with `gcloud run services proxy` or from a VPC.
`/healthz`, `/readyz`, and `/version` stay unauthenticated; `/v1/*` and
`/metrics` require `AQUILA_API_TOKEN`.

The shop stays on Compose. Do not point this Cloud Run service at a public
shop.
