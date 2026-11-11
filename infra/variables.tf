variable "project_id" {
  type        = string
  description = "作る GCP プロジェクトの ID。全世界で重複できない。"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project_id))
    error_message = "project_id は、小文字で始まり、小文字・数字・ハイフンだけを使った 6〜30 文字で、最後は小文字か数字にしてください。"
  }
}

variable "project_name" {
  type        = string
  description = "プロジェクトの表示名。"
  default     = "gdrive-cli"
}
