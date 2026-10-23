Fixture for the `ui-a11y` CI job: a config directory with two packs, the
upstream payload a local file server answers with, and one pack tree. The job
`git init`s the pack tree itself, because a nested repository cannot be
committed. Kept here, under the web package's testdata, so `go:embed` and
`go test ./...` never treat it as code.
