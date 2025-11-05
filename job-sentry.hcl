job "sentry" {
    group "worker" {
        task "run" {
            driver = "raw_exec"
            config {
                command = "/home/katie/go/bin/dlv"
                args = ["debug", "cmd/sentry/sentry.go", "--headless"]
                work_dir = "/home/katie/src/local-nomad"
            }

            identity {
                env = true
            }
        }
    }
}