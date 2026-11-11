output "project_id" {
  description = "作った GCP プロジェクトの ID。"
  value       = google_project.this.project_id
}

output "auth_overview_url" {
  description = "Google Auth Platform の概要画面。手作業の手順 1〜3（ブランディング、対象、データアクセス）はここから始める。"
  value       = "https://console.cloud.google.com/auth/overview?project=${google_project.this.project_id}"
}

output "auth_clients_url" {
  description = "OAuth クライアントの作成画面。手作業の手順 4 で使う。"
  value       = "https://console.cloud.google.com/auth/clients?project=${google_project.this.project_id}"
}
