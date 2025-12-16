# Kube-burner workloads

This directory structure holds several working kube-burner worloads that can be used as reference:

- api-intensive: This workload is meant to load kube-apiserver by creating pods mounting secrets and configmaps, and then delete them. You'll need to tweak QPS/Burst and jobIterations parameters according to the cluster size.
- service-latency-example: Showcases service latency measurment using a series of deloyments and services.
- kubelet-density: This is the most simple workload possible. It basically creates pods using an sleep image. Useful to verify max-pods in worker nodes.
- kubelet-density-profiling: Similar to the previous one, but with the addition of pprof measurements.
- kubelet-density-heavy: Similar to the previous one, with the difference that the pods it creates are actually a client/server application consisting of a basic application which performes queries in a pod running PostgreSQL and uses a k8s service to communicate with it.
- deployment-pvc-move: This workload is meant to test the CSI's ability to move volumes between nodes by creating node bound deployments with volumes and moving the deployments between nodes. When running the workload set the `workerHostNames` according to your cluster. Adjust the `replica` and `jobIteration` values to your test
