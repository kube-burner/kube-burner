# Kube-burner wrappers

It's possible to extend and take advantage of the kube-burner's features by developing wrappers. A kube-burner wrapper is basically a new binary that uses some of the exposed functions and interfaces of kube-burner.

## Helper functions

There're some helper functions meant to be consumed by wrappers in the `workloads` package. These functions provide some shortcuts to incorporate workloads in golang embedded filesystems.

## Customizing template rendering

It's possible to provide custom template rendering functions from a wrapper, this can be done by calling the function `AddRenderingFunction()` of the util package. For example:

```golang
util.AddRenderingFunction("isEven", func(n int) bool {
    return n%2 == 0
})
```

With the above code snippet we're provisioning a new template rendering function `isEven`, that basically returns true or false depending if the provided number is even. This function can be consumed in any of the kube-burner configuration files, for example

```yaml
jobs:
  - name: job
    podWait: {{ isEven 5 }}
```
