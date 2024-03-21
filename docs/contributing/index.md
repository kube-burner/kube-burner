# Contributing to kube-burner

If you want to contribute to kube-burner, you can do so by submitting a Pull Request, Issue or starting a Discussion.
You can also reach us out in the `#kube-burner` channel of the [kubernetes slack](https://kubernetes.slack.com/messages/kube-burner).

## TLDR;

1. Create your own fork of the repo
2. Make changes to the code in your fork
3. Check the code with linters
4. Verify your functinality through command line
5. Submit PR from your fork to main branch of the project repo

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

## Install

To install kube-burner in local, just execute `sudo make install`, once finished the kube-burner binary should be available across the system.

## Making changes relevant to OCP wrapper (.i.e. [kube-burner-ocp](https://github.com/kube-burner/kube-burner-ocp))

As of today, we have an OCP wrapper (.i.e. `kube-burner-ocp`) as one of our dependant repo. There are few steps that needs to be followed while making changes in this current repo that effect OCP wrapper's functionality as well.

### Changes at kube-burner/kube-burner repo level

1. Clone this repo and make changes in your fork.
2. Once done with the changes, commit you changes and execute the below command from your repo's root directory.
```
# This will replace all the imports in GO files with your fork path
find . -type f -name "*.go" -exec sed -i 's/github.com\/kube-burner\/kube-burner/github.com\/<your-fork>\/kube-burner/g' {} +
```
3. Finally update the `go.mod` to use your fork. For example
```
Replace - module github.com/kube-burner/kube-burner
With    -> module github.com/<your-fork>/kube-burner
```
4. Verify all your changes through `make build; sudo make install`, run some tests locally to verify functionality if required. After that please make a second `git commit` to your changes and push them to your fork.
5. Cut a release for your fork like one of these [here](https://github.com/kube-burner/kube-burner/releases) and make a note of version tag. Let's call it `v0.0.1` here.
6. Now in your local revert the second commit done in `Step 4` using below command.
```
git revert <commit-hash>
```
7. Now raise a PR for you changes in `kube-burner/kube-burner` repo if confident enough. Or else wait for your changes to get verified which is described in the below section.

### Changes at kube-burner/kube-burner-ocp level

1. Clone this repo and make changes in your fork.
2. Similar to `Step 2` in the previous section, commit your changes and execute the below command from your repo's root directory.
```
# This will replace all the imports in GO files with your fork path
find . -type f -name "*.go" -exec sed -i 's/github.com\/kube-burner\/kube-burner/github.com\/<your-fork>\/kube-burner/g' {} +
```
Make sure you point out the correct value for your fork of `kube-burner/kube-burner` which was done as part of `Step 1` in the previous section.  
3. Finally update the `go.mod` to use your fork. For example
```
Replace - module github.com/kube-burner/kube-burner v0.0.0
With    -> module github.com/<your-fork>/kube-burner v0.0.1 (in our case.i.e. your fork release version tag)
```
4. Now do a second `git commit` on top of these changes and raise a PR. 

Maintainers will take a look at both the PRs to understand how the changes in `kube-burner/kube-burner` are impacting `kube-burner/kube-burner-ocp`.

Once the pre-requisite PR in `kube-burner/kube-burner` is merged and goes through a release, revert your second commit done in `Step 4` of the above section and update your `kube-burner/kube-burner-ocp` PR with official repo's release version tag.

#### Example PRs:    
* PR at `kube-burner/kube-burner`: https://github.com/kube-burner/kube-burner/pull/612    
* Release of the fork branch: https://github.com/vishnuchalla/kube-burner/releases/tag/v0.0.7     
* PR at `kube-burner/kube-burner-ocp` using fork's release: https://github.com/kube-burner/kube-burner-ocp/pull/33/files#diff-33ef32bf6c23acb95f5902d7097b7a1d5128ca061167ec0716715b0b9eeaa5f6