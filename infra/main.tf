provider "google" {}

# 個人アカウントのため、組織やフォルダには属させない。課金アカウントも紐づけない。
resource "google_project" "this" {
  project_id = var.project_id
  name       = var.project_name

  # プロジェクトを消すと、CLI が作ったファイルが CLI から見えなくなる。
  deletion_policy = "PREVENT"

  # 既定のネットワークは使わない（trivy の GCP-0010）。
  auto_create_network = false
}

resource "google_project_service" "drive" {
  project = google_project.this.project_id
  service = "drive.googleapis.com"

  disable_on_destroy         = false
  disable_dependent_services = false
}
