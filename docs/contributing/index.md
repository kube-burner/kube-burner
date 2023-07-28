# Contributing to kube-burner

If you want to contribute to kube-burner, you can do so by submitting a Pull Request, Issue or starting a Discussion.

## Making changes and opening a Pull Request

For submitting a change upstream, please fork the repository and clone your forked repository.
```bash
$ git clone http://github.com/YOUR-USERNAME/kube-burner
$ cd kube-burner
$ git checkout -b <branch_name>
$ <make change>
$ git add <changes>
$ git commit -a
$ <insert good message>
$ git push
```

## CI and Linting

For running pre-commit checks on your code before comitting code and opening a PR, you can use the `pre-commit run` functionality.  See [CI docs](https://cloud-bulldozer.github.io/kube-burner/latest/contributing/ci) for more information.

## Building

To build kube-burner just execute `make build`, once finished the kube-burner binary should be available at `./bin/<arch>/kube-burner`.

!!! Note
    Building kube-burner requires `golang >=1.19`

```console
$ make build
building kube-burner 0.1.0
GOPATH=/home/kube-burner/go
CGO_ENABLED=0 go build -v -ldflags "-X github.com/cloud-bulldozer/kube-burner/version.GitCommit=d91c8cc35cb458a4b80a5050704a51c7c6e35076 -X github.com/cloud-bulldozer/kube-burner/version.BuildDate=2020-08-19-19:10:09 -X github.com/cloud-bulldozer/kube-burner/version.GitBranch=master" -o bin/kube-burner
```
