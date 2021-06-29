# Kube-burner workloads

This directory structure holds several working kube-burner worloads that can be used as reference:

- api-intensive: This workload is meant to load kube-apiserver by creating pods mounting secrets and configmaps, and then delete them. You'll need to tweak QPS/Burst and jobIterations parameters according to the cluster size.
- cluster-density: This workload creates is meant to be used in OpenShift environments, as it contains resources as builds and routes which are only available in this k8s distribution. Useful to stress OpenShift control plane.
- kubelet-density: This is the most simple workload possible. It basically creates pods using an sleep image. Useful to verify max-pods in worker nodes.
- kubelet-density-heavy: Similar to the previous one, with the difference that the pods it creates are actually a client/server application consisting of a basic application which performes queries in a pod running PostgreSQL and uses a k8s service to communicate with it.
