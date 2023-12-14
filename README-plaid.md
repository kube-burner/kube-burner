# README

This file lists the changes that were made in this branch to fix OOM
issues with kube-burner patch jobs that have multiple iterations.

The changes are implemented quick and dirty to unblock Plaid.
They are not fit for upstreaming as is.

This branch cherry-picked `70037e3b3b4c1b509df90a7b205caa1710c56f9c` which fixes a concurrent
map iteration + read bug, and `d68d5c2c78471b177819d87d7dfa847403707700` which updates
the container file such that the kube-burner image is built within the container
to simplify multi arch image builds. 

## Changes

kube-burner instantiated a new `text/template` instance each time a template was
rendered with `RenderTemplate`. Cache `text/template` in a map instead.
Templates are looked up by a key that is passed into `RenderTemplate`.
If the key is an empty string, cache isn't used.
The problematic hot path passes the template file name as key for caching.
Other `RenderTemplate` call sites pass empty string.

kube-burner patch jobs with multiple `iterations` were updating the same object many times in parallel.
The code has been changed to patch all objects once in an iteration, then patch all objects again in
the next iteration and so on.

kube-burner spawns a goroutine for each k8s object that will be patched, times the number of `iterations`.
The number of routines reaches a million easiliy this way.
Each goroutine calls `RenderTemplate` to get the patch manifest.
Right after rendering the patch manifest, the routines have to pass a rate limiter.
The result is millions of goroutines that hold siginificant memory because of the rendered manifest
and progress very slowly because of the rate limiter.
This PR moves the rate limiter wait call in front of spawning the goroutines, greatly reducing the number of goroutines
and memory use.

## Build image

```shell
docker buildx build --push --platform linux/amd64,linux/arm64 \
-t 475133402591.dkr.ecr.us-east-1.amazonaws.com/plaid/kube-burner:nt-cowboy4 \
-f Containerfile .
```
