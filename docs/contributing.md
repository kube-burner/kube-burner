If you want to contribute to kube-burner, submit a Pull Request or Issue.
Kube-burner uses `golangci-lint` to run linters. Prior to send a PR, ensure to execute linters with `make lint`.

# Requirements

- `golang >= 1.16`
- `golang-ci-lint`
- `make`

# Building

To build kube-burner just execute `make build`, once finished `kube-burner`'s binary should be available at `./bin/kube-burner`

```console
$ make build
building kube-burner 0.1.0
GOPATH=/home/rsevilla/go
CGO_ENABLED=0 go build -v -mod vendor -ldflags "-X github.com/cloud-bulldozer/kube-burner/version.GitCommit=d91c8cc35cb458a4b80a5050704a51c7c6e35076 -X github.com/cloud-bulldozer/kube-burner/version.BuildDate=2020-08-19-19:10:09 -X github.com/cloud-bulldozer/kube-burner/version.GitBranch=master" -o bin/kube-burner
```
