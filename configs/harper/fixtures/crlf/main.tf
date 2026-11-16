# Pass the host and/or the port.
resource "aws_instance" "web" {
  user_data = <<-EOF
    # Copy the file(s) to the server, e.g. the build output.
    cp a b
  EOF
}
