run "default_greeting" {
  command = plan

  assert {
    condition     = output.greeting == "hello"
    error_message = "expected default greeting"
  }
}

run "custom_greeting" {
  command = plan

  variables {
    greeting = "world"
  }

  assert {
    condition     = output.greeting == "world"
    error_message = "expected greeting override"
  }
}
