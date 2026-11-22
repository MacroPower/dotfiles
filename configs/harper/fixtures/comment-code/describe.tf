variable "first" {
  description = "The job runs --"
}

variable "second" {
  description = "The job runs -- x"
}

variable "third" {
  # The job runs --
  description = "The cache refills."
}

variable "fourth" {
  description = <<-EOT
    The job runs --
  EOT
}

variable "fifth" {
  validation {
    condition     = true
    error_message = "The \"job\" runs --"
  }
}

variable "sixth" {
  description = "The job runs --"
  # The cache refills.
}

variable "seventh" {
  description = "The input."
  # 2. The job runs --
  # 2. The cache refills.
}

variable "eighth" {
  description = <<-EOT
    The job runs --
    // the cache refills.
  EOT
}
