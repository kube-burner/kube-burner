# Pre-commit Checks

To maintain code quality and catch issues early on, we use pre-commit checks. These checks are automatically executed before each commit to ensure that the code complies with our standards.

The following hooks have been enabled for this project:

- [golangci-lint](https://github.com/golangci/golangci-lint)
- [Shellcheck](https://github.com/jumanjihouse/pre-commit-hooks)
- [Markdown Lint](https://github.com/markdownlint/markdownlint)

Main purpose for pre-commit is to allow developers to pass the Lint Checks before commiting the code. Same checks will be executed on all the commits once they are pushed to GitHub

## Installation

To install pre-commit checks locally, follow these steps:

- Install [pre-commit](https://pre-commit.com/) by running the following command:

```console
pip install pre-commit
```

- `ruby` is required for running the Markdown Linter, installation will depends on your Operating System, for example, on Fedora:

```console
dnf install -y ruby
```

- Initialize pre-commit on the repo:

```console
pre-commit install
```

## Executing Manually

To run pre-commit manually for all files, you can use `make lint`

```console
make lint
```

Or you can run against an especific file:

```console
$ pre-commit run --files README.md
golangci-lint........................................(no files to check)Skipped
Markdownlint.............................................................Passed
```

```console
$ pre-commit run --files ./cmd/kube-burner/kube-burner.go
golangci-lint............................................................Passed
Markdownlint.........................................(no files to check)Skipped
```

```console
$ pre-commit run --all-files
golangci-lint............................................................Passed
Markdownlint.............................................................Passed
```

Using master as rev is not supported anymore on pre-commit, so reference has been pointed to the last version available.

Hooks can be updated using `pre-commit autoupdate`:

```console
$ pre-commit autoupdate
[WARNING] The 'rev' field of repo 'https://github.com/golangci/golangci-lint' appears to be a mutable reference (moving tag / branch).  Mutable references are never updated after first install and are not supported.  See https://pre-commit.com/#using-the-latest-version-for-a-repository for more details.  Hint: `pre-commit autoupdate` often fixes this.
[WARNING] The 'rev' field of repo 'https://github.com/markdownlint/markdownlint' appears to be a mutable reference (moving tag / branch).  Mutable references are never updated after first install and are not supported.  See https://pre-commit.com/#using-the-latest-version-for-a-repository for more details.  Hint: `pre-commit autoupdate` often fixes this.
[WARNING] The 'rev' field of repo 'https://github.com/jumanjihouse/pre-commit-hooks' appears to be a mutable reference (moving tag / branch).  Mutable references are never updated after first install and are not supported.  See https://pre-commit.com/#using-the-latest-version-for-a-repository for more details.  Hint: `pre-commit autoupdate` often fixes this.
[https://github.com/golangci/golangci-lint] updating master -> v1.52.2
[https://github.com/markdownlint/markdownlint] updating master -> v0.12.0
[https://github.com/jumanjihouse/pre-commit-hooks] updating master -> 3.0.0
```
