# Reference

All of the magic that `kube-burner` does is described in its configuration file. As previously mentioned, the location of this configuration file is provided by the flag `-c`. This flag points to a YAML-formatted file that consists of several sections.

It is possible to use [go-template](https://pkg.go.dev/text/template) semantics within this configuration file. It is also important to note that every environment variable is passed to this template, so we can reference them using the syntax `{{.MY_ENV_VAR}}`. For example, you could define the `indexers` section of your own configuration file, such as:

```yaml
indexers:
{{ if .OS_INDEXING }}
  - type: opensearch
    esServers: ["{{ .ES_SERVER }}"]
    defaultIndex: {{ .ES_INDEX }}
{{ end }}
{{ if .LOCAL_INDEXING }}
  - type: local
    metricsDirectory: {{ .METRICS_FOLDER }}
{{ end }}
```

This feature can be very useful at the time of defining secrets, such as the user and password of our indexer, or a token to use in pprof collection.

## Global

In this section is described global job configuration, it holds the following parameters:

| Option           | Description                                                                                              | Type           | Default      |
|------------------|----------------------------------------------------------------------------------------------------------|----------------|--------------|
| `measurements`     | List of measurements. Detailed in the [measurements section](/kube-burner/latest/measurements)                            | List          | []          |
| `requestTimeout`   | Client-go request timeout                                                                                | Duration      | 60s         |
| `gc`               | Garbage collect created namespaces                                                                       | Boolean        | false      |
| `gcMetrics`        | Flag to collect metrics during garbage collection                                                        | Boolean        |      false      |
| `gcTimeout`               | Garbage collection timeout                                                                       | Duration        | 1h   |
| `waitWhenFinished` | Wait for all pods to be running when all jobs are completed                                             | Boolean        | false      |
| `clusterHealth` | Checks if all the nodes are in "Ready" state                                             | Boolean        | false      |

!!! note
    The precedence order to wait on resources is Global.waitWhenFinished > Job.waitWhenFinished > Job.podWait

kube-burner connects k8s clusters using the following methods in this order:

- `KUBECONFIG` environment variable
- `$HOME/.kube/config`
- In-cluster config (Used when kube-burner runs inside a pod)

## Jobs

This section contains the list of jobs `kube-burner` will execute. Each job can hold the following parameters.

| Option                   | Description                                                                                                                       | Type     | Default |
|--------------------------|-----------------------------------------------------------------------------------------------------------------------------------|----------|---------|
| `name`                   | Job name                                                                                                                          | String   | ""      |
| `jobType`                | Type of job to execute. More details at [job types](#job-types)                                                                   | string   | create  |
| `jobIterations`          | How many times to execute the job                                                                                                 | Integer  | 0       |
| `namespace`              | Namespace base name to use                                                                                                        | String   | ""      |
| `namespacedIterations`   | Whether to create a namespace per job iteration                                                                                   | Boolean  | true    |
| `iterationsPerNamespace` | The maximum number of `jobIterations` to create in a single namespace. Important for node-density workloads that create Services. | Integer  | 1       |
| `cleanup`                | Cleanup clean up old namespaces                                                                                                   | Boolean  | true    |
| `podWait`                | Wait for all pods to be running before moving forward to the next job iteration                                                   | Boolean  | false   |
| `waitWhenFinished`       | Wait for all pods to be running when all iterations are completed                                                                 | Boolean  | true    |
| `maxWaitTimeout`         | Maximum wait timeout per namespace                                                                                                | Duration | 4h      |
| `jobIterationDelay`      | How long to wait between each job iteration. This is also the wait interval between each delete operation                         | Duration | 0s      |
| `jobPause`               | How long to pause after finishing the job                                                                                         | Duration | 0s      |
| `beforeCleanup`          | Allows to run a bash script before the workload is deleted                                                                        | String   | ""      |
| `qps`                    | Limit object creation queries per second                                                                                          | Integer  | 0       |
| `burst`                  | Maximum burst for throttle                                                                                                        | Integer  | 0       |
| `objects`                | List of objects the job will create. Detailed on the [objects section](#objects)                                                  | List     | []      |
| `verifyObjects`          | Verify object count after running each job                                                                                        | Boolean  | true    |
| `errorOnVerify`          | Set RC to 1 when objects verification fails                                                                                       | Boolean  | true    |
| `skipIndexing`           | Skip metric indexing on this job                                                                                                  | Boolean  | false   |
| `preLoadImages`          | Kube-burner will create a DS before triggering the job to pull all the images of the job                                          | true     |         |
| `preLoadPeriod`          | How long to wait for the preload daemonset                                                                                        | Duration | 1m      |
| `preloadNodeLabels`      | Add node selector labels for the resources created in preload stage                                                               | Object   | {}      |
| `namespaceLabels`        | Add custom labels to the namespaces created by kube-burner                                                                        | Object   | {}      |
| `namespaceAnnotations`   | Add custom annotations to the namespaces created by kube-burner                                                                   | Object   | {}      |
| `churn`                  | Churn the workload. Only supports namespace based workloads                                                                       | Boolean  | false   |
| `churnCycles`           | Number of churn cycles to execute   | Integer | 100
| `churnPercent`           | Percentage of the jobIterations to churn each period                                                                              | Integer  | 10      |
| `churnDuration`          | Length of time that the job is churned for                                                                                        | Duration | 1h      |
| `churnDelay`             | Length of time to wait between each churn period                                                                                  | Duration | 5m      |
| `churnDeletionStrategy`  | Churn deletion strategy to apply, "default" or "gvr" (where `default` churns namespaces and `gvr` churns objects within namespaces)                                                      | String   | default |

!!! note
    Both `churnCycles` and `churnDuration` serve as termination conditions, with the churn process halting when either condition is met first. If someone wishes to exclusively utilize `churnDuration` to control churn, they can achieve this by setting `churnCycles` to `0`. Conversely, to prioritize `churnCycles`, one should set a longer `churnDuration` accordingly.

Our configuration files strictly follow YAML syntax. To clarify on List and Object types usage, they are nothing but the [`Lists and Dictionaries`](https://gettaurus.org/docs/YAMLTutorial/#Lists-and-Dictionaries) in YAML syntax.

Examples of valid configuration files can be found in the [examples folder](https://github.com/kube-burner/kube-burner/tree/master/examples).

## Objects

The objects created by `kube-burner` are rendered using the default golang's [template library](https://golang.org/pkg/text/template/).
Each object element supports the following parameters:

| Option               | Description                                                       | Type    | Default |
|----------------------|-------------------------------------------------------------------|---------|---------|
| `objectTemplate`       | Object template file path or URL                                | String  | ""      |
| `replicas`             | How replicas of this object to create per job iteration           | Integer | -       |
| `inputVars`            | Map of arbitrary input variables to inject to the object template | Object  | -       |
| `wait`                 | Wait for object to be ready                                       | Boolean | true    |
| `waitOptions`          | Customize [how to wait](#wait-options) for object to be ready     | Object  | {}       |
| `runOnce`              | Create or delete this object only once during the entire job    | Boolean | false   |

!!! warning
    Kube-burner is only able to wait for a subset of resources, unless `waitOptions` are specified.

!!! info
    Find more info about the waiters implementation in the `pkg/burner/waiters.go` file

### Wait Options

If you want to override the default waiter behaviors, you can specify wait options for your objects.

| Option       | Description                                             | Type    | Default |
|--------------|---------------------------------------------------------|---------|---------|
| `forCondition` | Wait for the object condition with this name to be true | String  | ""      |

For example, the snippet below can be used to make kube-burner wait for all containers from the pod defined at `pod.yml` to be ready.

```yaml
objects:
- objectTemplate: pod.yml
  replicas: 3
  waitOptions:
    forCondition: Ready
```

### Default labels

All objects created by kube-burner are labeled with `kube-burner-uuid=<UUID>,kube-burner-job=<jobName>,kube-burner-index=<objectIndex>`. They are used for internal purposes, but they can also be used by the users.

## Job types

Configured by the parameter `jobType`, kube-burner supports three types of jobs with different parameters each.

### Create

The default `jobType` is __create__. Creates objects listed in the `objects` list as described in the [objects section](#objects). The amount of objects created is configured by `jobIterations`, `replicas`. If the object is namespaced and has an empty `.metadata.namespace` field, `kube-burner` creates a new namespace with the name `namespace-<iteration>`, and creates the defined amount of objects in it.

### Delete

This type of job deletes objects described in the objects list. Using delete as job type the objects list would have the following structure:

```yaml
objects:
- kind: Deployment
  labelSelector: {kube-burner-job: cluster-density}
  apiVersion: apps/v1

- kind: Secret
  labelSelector: {kube-burner-job: cluster-density}
```

Where:

- `kind`: Object kind of the k8s object to delete.
- `labelSelector`: Deletes the objects with the given labels.
- `apiVersion`: API version from the k8s object.

This type of job supports the following parameters. Some of them  are already described in the [create job type section](#create):

- `waitForDeletion`: Wait for objects to be deleted before finishing the job. Defaults to `true`.
- `name`
- `qps`
- `burst`
- `jobPause`
- `jobIterationDelay`

### Read

This type of job reads objects described in the objects list. Using read as job type the objects list would have the following structure:

```yaml
objects:
- kind: Deployment
  labelSelector: {kube-burner-job: cluster-density}
  apiVersion: apps/v1

- kind: Secret
  labelSelector: {kube-burner-job: cluster-density}
```

Where:

- `kind`: Object kind of the k8s object to read.
- `labelSelector`: Reads the objects with the given labels.
- `apiVersion`: API version from the k8s object.

This type of job supports the following parameters. Some of them  are already described in the [create job type section](#create):

- `name`
- `qps`
- `burst`
- `jobPause`
- `jobIterationDelay`
- `jobIterations`

### Patch

This type of job can be used to patch objects with the template described in the object list. This object list has the following structure:

```yaml
objects:
- kind: Deployment
  labelSelector: {kube-burner-job: cluster-density}
  objectTemplate: templates/deployment_patch_add_label.json
  patchType: "application/strategic-merge-patch+json"
  apiVersion: apps/v1
```

Where:

- `kind`: Object kind of the k8s object to patch.
- `labelSelector`: Map with the labelSelector.
- `objectTemplate`: The YAML template or JSON file to patch.
- `apiVersion`: API version from the k8s object.
- `patchType`: The Kubernetes request patch type (see below).

Valid patch types:

- application/json-patch+json
- application/merge-patch+json
- application/strategic-merge-patch+json
- application/apply-patch+yaml (requires YAML)

As mentioned previously, all objects created by kube-burner are labeled with `kube-burner-uuid=<UUID>,kube-burner-job=<jobName>,kube-burner-index=<objectIndex>`. Therefore, you can design a workload with one job to create objects and another one to patch or remove the objects created by the previous.

```yaml
jobs:
- name: create-objects
  namespace: job-namespace
  jobIterations: 100
  objects:
  - objectTemplate: deployment.yml
    replicas: 10

  - objectTemplate: service.yml
    replicas: 10

- name: remove-objects
  jobType: delete
  objects:
  - kind: Deployment
    labelSelector: {kube-burner-job: create-objects}
    apiVersion: apps/v1

  - kind: Secret
    labelSelector: {kube-burner-job: create-objects}
```

## Churning Jobs

Churn is the deletion and re-creation of objects, and is supported for namespace-based jobs only. This occurs after the job has completed
but prior to uploading metrics, if applicable. It deletes a percentage of contiguous namespaces randomly chosen and re-creates them
with all of the appropriate objects. It will then wait for a specified delay (or none if set to `0`) before deleting and recreating the
next randomly chosen set. This cycle continues until the churn duration has passed.

An example implementation that would churn 20% of the 100 job iterations for 2 hours with no delay between sets:

```yaml
jobs:
- name: cluster-density
  jobIterations: 100
  namespacedIterations: true
  namespace: churning
  churn: true
  churnPercent: 20
  churnDuration: 2h
  churnDelay: 0s
  objects:
  - objectTemplate: deployment.yml
    replicas: 10

  - objectTemplate: service.yml
    replicas: 10
```

## Injected variables

All object templates are injected with the variables below by default:

- `Iteration`: Job iteration number.
- `Replica`: Object replica number. Keep in mind that this number is reset to 1 with each job iteration.
- `JobName`: Job name.
- `UUID`: Benchmark UUID.

In addition, you can also inject arbitrary variables with the option `inputVars` of the object:

```yaml
- objectTemplate: service.yml
  replicas: 2
  inputVars:
    port: 80
    targetPort: 8080
```

The following code snippet shows an example of a k8s service using these variables:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: sleep-app-{{.Iteration}}-{{.Replica}}
  labels:
    name: my-app-{{.Iteration}}-{{.Replica}}
spec:
  selector:
    app: sleep-app-{{.Iteration}}-{{.Replica}}
  ports:
  - name: serviceport
    protocol: TCP
    port: "{{.port}}"
    targetPort: "{{.targetPort}}"
  type: ClusterIP
```
<!-- markdownlint-disable -->
!!! tip "You can also use [golang template semantics](https://golang.org/pkg/text/template/) in your `objectTemplate` definitions"
    ```yaml
    kind: ImageStream
    apiVersion: image.openshift.io/v1
    metadata:
      name: {{.prefix}}-{{.Replica}}
    spec:
    {{ if .image }}
      dockerImageRepository: {{.image}}
    {{ end }}
    ```
<!-- markdownlint-restore -->

## Template functions

In addition to the default [golang template semantics](https://golang.org/pkg/text/template/), kube-burner is compiled with the [sprig library](http://masterminds.github.io/sprig/), which adds over 70 template functions for Go’s template language.

## RunOnce 

All objects within the job will iteratively run based on the JobIteration number, 
but there may be a situation if an object need to be created only once (ex. clusterrole), in such cases
we can add an optional field as `runOnce` for that particular object to execute only once in the entire job.

An example scenario as below template, a job iteration of 100 but create the clusterrole only once.

```yaml
jobs:
- name: cluster-density
  jobIterations: 100
  namespacedIterations: true
  namespace: cluster-density
  objects:
  - objectTemplate: clusterrole.yml
    replicas: 1
    runOnce: true

  - objectTemplate: clusterrolebinding.yml
    replicas: 1
    runOnce: true

  - objectTemplate: deployment.yml
    replicas: 10
```
