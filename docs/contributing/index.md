# Contributing to kube-burner

If you want to contribute to kube-burner, you can do so by submitting a Pull Request, Issue or starting a Discussion.
You can also reach us out in the `#kube-burner` channel of the [kubernetes slack](https://kubernetes.slack.com/messages/kube-burner).

## CI and Linting

For running pre-commit checks on your code before committing code and opening a PR, you can use the `pre-commit run` functionality.  See [CI docs](https://kube-burner.github.io/kube-burner/latest/contributing/pullrequest/#running-local-pre-commit) for more information on running pre-commits.

## Building

To build kube-burner just execute `make build`, once finished the kube-burner binary should be available at `./bin/<arch>/kube-burner`.

!!! Note
    Building kube-burner requires `golang >=1.19`

```console
$ make build
Building bin/amd64/kube-burner
GOPATH=/home/rsevilla/go/
GOARCH=amd64 CGO_ENABLED=0 go build -v -ldflags "-X github.com/cloud-bulldozer/go-commons/version.GitCommit=4c9c3f43db83adb053efc58220ddd696d1d19a35 -X github.com/cloud-bulldozer/go-commons/version.BuildDate=2024-01-10-21:24:20 -X github.com/cloud-bulldozer/go-commons/version.Version=main" -o bin/amd64/kube-burner ./cmd/kube-burner
github.com/kube-burner/kube-burner/cmd/kube-burner
```
