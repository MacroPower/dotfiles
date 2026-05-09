# Sample fixture for e2e tests. Uses only `terraform_data` (built-in) so
# `tofu init` succeeds without network access.

variable "greeting" {
  type    = string
  default = "hello"
}

resource "terraform_data" "example" {
  input = var.greeting
}

output "greeting" {
  value = terraform_data.example.input
}
