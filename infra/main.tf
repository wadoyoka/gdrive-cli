provider "google" {}

# 個人アカウントのため、組織やフォルダには属させない。課金アカウントも紐づけない。
#
# auto_create_network は書かない（既定の true のまま）。false にすると、provider が既定のネットワークを
# 消すために Compute Engine API を有効にしようとし、課金のないプロジェクトでは apply が失敗する。
# 課金がなければ Compute Engine API は有効にならず、既定のネットワークも作られない。
# trivy の GCP-0010（既定のネットワークを作る設定）が心配するネットワークはできないので、この指摘だけを外す。
#trivy:ignore:GCP-0010
resource "google_project" "this" {
  project_id = var.project_id
  name       = var.project_name

  # プロジェクトを消すと、CLI が作ったファイルが CLI から見えなくなる。
  deletion_policy = "PREVENT"
}

resource "google_project_service" "drive" {
  project = google_project.this.project_id
  service = "drive.googleapis.com"

  disable_on_destroy         = false
  disable_dependent_services = false
}
