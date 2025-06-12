# Reference

All of the magic that `kube-burner` does is described in its configuration file. As previously mentioned, the location of this configuration file is provided by the flag `-c`. This flag points to a YAML-formatted file that consists of several sections.

## Templating the configuraion file

[go-template](https://pkg.go.dev/text/template) semantics may be used within the configuration file.
The input for the templates is taken from a user data file (using the `--user-data` parameter) and/or environment variables.
Environment variables take precedence over those defined in the file when the same variable is defined in both.

 For example, you could define the `indexers` section of your own configuration file, such as:

```yaml
metricsEndpoints:
{{ if .OS_INDEXING }}
  - prometheusURL: http://localhost:9090
    indexer:
      type: opensearch
      esServers: ["{{ .ES_SERVER }}"]
      defaultIndex: {{ .ES_INDEX }}
{{ end }}
{{ if .LOCAL_INDEXING }}
  - prometheusURL: http://localhost:9090
    indexer:
      type: local
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
| `waitWhenFinished` | Wait for all pods/jobs (including probes) to be running/completed when all jobs are completed           | Boolean  | false   |
| `clusterHealth` | Checks if all the nodes are in "Ready" state                                             | Boolean        | false      |
| `timeout` | Global benchmark timeout                                             | Duration        | 4hr      |
| `functionTemplates` | Function template files to render at runtime                                             | List        | []      |

!!! note
    The precedence order to wait on resources is Global.waitWhenFinished > Job.waitWhenFinished > Job.podWait

kube-burner connects k8s clusters using the following methods in this order:

- `KUBECONFIG` environment variable
- `$HOME/.kube/config`
- In-cluster config (Used when kube-burner runs inside a pod)

### Function templating example
Using function templates we can define a block of code as function and reuse it in any parts of our configuration. For the purpose of this example, lets assume we have a configuration like below in our **deployment.yaml**
```
env:
- name: ENVVAR1_{{.name}}
  value: {{.envVar}}
- name: ENVVAR2_{{.name}}
  value: {{.envVar}}
- name: ENVVAR3_{{.name}}
  value: {{.envVar}}
- name: ENVVAR4_{{.name}}
  value: {{.envVar}}
```
Now I want to modularize and use it in my code. In order to do that, I will create a template **envs.tpl** as below.
```
{{- define "env_func" -}}
{{- range $i := until $.n }}
{{- printf "- name: ENVVAR%d_%s\n  value: %s" (add $i 1) $.name $.envVar | nindent $.indent }}
{{- end }}
{{- end }}
```
Once done we will make sure that our function template is invoked as a part of the global configuration as below so that it can be used across.
```
global:
  functionTemplates:
    - envs.tpl
```
Final step is to invoke this function with required parameters to make sure it replaces the redundant code in our **deployment.yaml** file.
```
env:
{{- template "env_func" (dict "name" .name "envVar" .envVar "n" 4 "indent" 8) }}
```
We are all set! We should have our function rendered at the runtime and can be reused in future as well.

## Jobs

This section contains the list of jobs `kube-burner` will execute. Each job can hold the following parameters.

| Option                       | Description                                                                                                                           | Type     | Default  |
|------------------------------|---------------------------------------------------------------------------------------------------------------------------------------|----------|----------|
| `name`                       | Job name                                                                                                                              | String   | ""       |
| `jobType`                    | Type of job to execute. More details at [job types](#job-types)                                                                       | String   | create   |
| `jobIterations`              | How many times to execute the job                                                                                                     | Integer  | 0        |
| `namespace`                  | Namespace base name to use                                                                                                            | String   | ""       |
| `namespacedIterations`       | Whether to create a namespace per job iteration                                                                                       | Boolean  | true     |
| `iterationsPerNamespace`     | The maximum number of `jobIterations` to create in a single namespace. Important for node-density workloads that create Services.     | Integer  | 1        |
| `cleanup`                    | Cleanup clean up old namespaces                                                                                                       | Boolean  | true     |
| `podWait`                    | Wait for all pods/jobs (including probes) to be running/completed before moving forward to the next job iteration                     | Boolean  | false    |
| `waitWhenFinished`           | Wait for all pods/jobs (including probes) to be running/completed when all job iterations are completed                               | Boolean  | true     |
| `maxWaitTimeout`             | Maximum wait timeout per namespace                                                                                                    | Duration | 4h       |
| `jobIterationDelay`          | How long to wait between each job iteration. This is also the wait interval between each delete operation                             | Duration | 0s       |
| `jobPause`                   | How long to pause after finishing the job                                                                                             | Duration | 0s       |
| `beforeCleanup`              | Allows to run a bash script before the workload is deleted                                                                            | String   | ""       |
| `qps`                        | Limit object creation queries per second                                                                                              | Integer  | 0        |
| `burst`                      | Maximum burst for throttle                                                                                                            | Integer  | 0        |
| `objects`                    | List of objects the job will create. Detailed on the [objects section](#objects)                                                      | List     | []       |
| `verifyObjects`              | Verify object count after running each job                                                                                            | Boolean  | true     |
| `errorOnVerify`              | Set RC to 1 when objects verification fails                                                                                           | Boolean  | true     |
| `skipIndexing`               | Skip metric indexing on this job                                                                                                      | Boolean  | false    |
| `preLoadImages`              | Kube-burner will create a DS before triggering the job to pull all the images of the job                                              | Boolean  |          |
| `preLoadPeriod`              | How long to wait for the preload DaemonSet                                                                                            | Duration | 1m       |
| `preloadNodeLabels`          | Add node selector labels for the resources created in preload stage                                                                   | Object   | {}       |
| `namespaceLabels`            | Add custom labels to the namespaces created by kube-burner                                                                            | Object   | {}       |
| `namespaceAnnotations`       | Add custom annotations to the namespaces created by kube-burner                                                                       | Object   | {}       |
| `churn`                      | Churn the workload. Only supports namespace based workloads                                                                           | Boolean  | false    |
| `churnCycles`                | Number of churn cycles to execute                                                                                                     | Integer  | 100      |
| `churnPercent`               | Percentage of the jobIterations to churn each period                                                                                  | Integer  | 10       |
| `churnDuration`              | Length of time that the job is churned for                                                                                            | Duration | 1h       |
| `churnDelay`                 | Length of time to wait between each churn period                                                                                      | Duration | 5m       |
| `churnDeletionStrategy`      | Churn deletion strategy to apply, `default` or `gvr` (where `default` churns namespaces and `gvr` churns objects within namespaces)   | String   | default  |
| `defaultMissingKeysWithZero` | Stops templates from exiting with an error when a missing key is found, meaning users will have to ensure templates hand missing keys | Boolean  | false    |
| `executionMode`              | Job execution mode. More details at [execution modes](#execution-modes)                                                               | String   | parallel |
| `objectDelay`                | How long to wait between each object in a job                                                                                         | Duration | 0s       |
| `objectWait`                 | Wait for each object to complete before processing the next one - not for Create jobs                                                 | Boolean  | 0s       |
| `metricsAggregate`           | Aggregate the metrics collected for this job with those of the next one                                                               | Boolean  | false    |
| `metricsClosing`             | To define when the metrics collection should stop. More details at [MetricsClosing](#MetricsClosing)                                  | String   | afterJobPause |

!!! note
    Both `churnCycles` and `churnDuration` serve as termination conditions, with the churn process halting when either condition is met first. If someone wishes to exclusively utilize `churnDuration` to control churn, they can achieve this by setting `churnCycles` to `0`. Conversely, to prioritize `churnCycles`, one should set a longer `churnDuration` accordingly.

!!! note
    When `jobType` is set to [Delete](#delete) the following settings are forced:
    `jobIterations` is set to `1`,
    `waitWhenFinished` is set to `false`,
    `executionMode` is set to `sequential`

Our configuration files strictly follow YAML syntax. To clarify on List and Object types usage, they are nothing but the [`Lists and Dictionaries`](https://gettaurus.org/docs/YAMLTutorial/#Lists-and-Dictionaries) in YAML syntax.

Examples of valid configuration files can be found in the [examples folder](https://github.com/kube-burner/kube-burner/tree/master/examples).

### Objects

The objects created by `kube-burner` are rendered using the default golang's [template library](https://golang.org/pkg/text/template/).
Each object element supports the following parameters:

| Option               | Description                                                       | Type    | Default |
|----------------------|-------------------------------------------------------------------|---------|---------|
| `objectTemplate`       | Object template file path or URL                                | String  | ""      |
| `replicas`             | How replicas of this object to create per job iteration           | Integer | -       |
| `inputVars`            | Map of arbitrary input variables to inject to the object template | Object  | -       |
| `wait`                 | Wait for object to be ready                                       | Boolean | true    |
| `waitOptions`          | Customize [how to wait](#object-wait-options) for object to be ready     | Object  | {}       |
| `runOnce`              | Create or delete this object only once during the entire job    | Boolean | false   |

!!! warning
    Kube-burner is only able to wait for a subset of resources, unless `waitOptions` are specified.

### Built-in support for object waiters

The following object types have built-in waiters:
- StatefulSet
- Deployment
- DaemonSet
- ReplicaSet
- Job
- Pod
- ReplicationController
- Build
- BuildConfig
- VirtualMachine
- VirtualMachineInstance
- VirtualMachineInstanceReplicaSet
- PersistentVolumeClaim
- VolumeSnapshot
- DataVolume
- DataSource

!!! info
    Find more info about the waiters implementation in the `pkg/burner/waiters.go` file

### Object wait Options

If you want to override the default waiter behaviors, you can specify wait options for your objects.

| Option       | Description                                             | Type    | Default |
|--------------|---------------------------------------------------------|---------|---------|
| `kind` | Object kind to consider for wait | String | "" |
| `labelSelector` | Objects with these labels will be considered for wait | Object | {} |
| `customStatusPaths` | list of jq path/values to verify readiness of the object | Object  | [] |

For example, the snippet below can be used to make kube-burner wait for all containers from the pod defined at `pod.yml` to be ready.

```yaml
objects:
- objectTemplate: deployment.yml
  replicas: 3
  waitOptions:
    kind: Pod
    labelSelector: {kube-burner-label : abcd}
```

Additionally, you can use `customStatusPaths` to specify custom paths to be checked for the readiness of the object. For example, to wait for a deployment to be available

```yaml
objects:
  - kind: Deployment
    objectTemplate: deployment.yml
    replicas: 1
    waitOptions:
      customStatusPaths:
        - key: '(.conditions.[] | select(.type == "Available")).status'
          value: "True"
```
This allows kube-burner to check the status at all the specified key/value pairs and verify readiness of the object. If any of them do not match then it is indicated as a failure.

!!! note
  Currently, the `value` field expects only strings.
  In order to test other types make sure to convert the result to a string in the `key`.

  For example, to verify that a `VolumeSnapshot` is `readyToUse` set the `customStatusPaths` to:
  ```yaml
  customStatusPaths:
    - key: '(.conditions.[] | select(.type == "Ready")).status'
      value: "True"
  ```

!!! note
    `waitOptions.kind`, `waitOptions.customStatusPaths` and `waitOptions.labelSelector` are fully optional. `waitOptions.kind` is used when an application has child objects to be waited & `waitOptions.labelSelector` is used when we want to wait on objects with specific labels.

### Default labels

All objects created by kube-burner are labeled with `kube-burner-uuid=<UUID>,kube-burner-job=<jobName>,kube-burner-index=<objectIndex>`. They are used for internal purposes, but they can also be used by the users.

## Job types

Configured by the parameter `jobType`, kube-burner supports four types of jobs with different parameters each:

- Create
- Delete
- Read
- Patch

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

This type of job supports the following parameters. Described in the [jobs section](#jobs):

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

This type of job supports the following parameters. Described in the [jobs section](#jobs):

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

### Kubevirt

This type of job can be used to execute `virtctl` commands described in the object list. This object list has the following structure:

```yaml
objects:
- kubeVirtOp: start
  labelSelector: {kube-burner-job: cluster-density}
  inputVars:
    force: true
```

Where:

- `kubeVirtOp`: virtctl operation to execute.
- `labelSelector`: Map with the labelSelector.
- `inputVars`: Additional command parameters

#### Supported Operations

##### `start`

Execute `virtctl start` on the VMs mapped by the `labelSelector`.
Additional parameters may be set using the `inputVars` field:

- `startPaused` - VM will start in `Paused` state. Default `false`

##### `stop`

Execute `virtctl stop` on the VMs mapped by the `labelSelector`.
Additional parameters may be set using the `inputVars` field:

- `force` - Force stop the VM without waiting. Default `false`

##### `restart`

Execute `virtctl restart` on the VMs mapped by the `labelSelector`.
Additional parameters may be set using the `inputVars` field:

- `force` - Force restart the VM without waiting. Default `false`

##### `pause`

Execute `virtctl pause` on the VMs mapped by the `labelSelector`.
No additional parametes are supported.

##### `unpause`

Execute `virtctl unpause` on the VMs mapped by the `labelSelector`.
No additional parametes are supported.


##### `migrate`

Execute `virtctl migrate` on the VMs mapped by the `labelSelector`.
No additional parametes are supported.

##### `add-volume`

Execute `virtctl addvolume` on the VMs mapped by the `labelSelector`.
Additional parameters should be set using the `inputVars` field:

- `volumeName` - Name of the already existing volume to add. Mandatory
- `diskType` - Type of the new volume (`disk`/`lun`). Default `disk`
- `serial` - serial number you want to assign to the disk. Defaults to the value of `volumeName`
- `cache` - caching options attribute control the cache mechanism. Default `''`
- `persist` - if set, the added volume will be persisted in the VM spec (if it exists). Default `false`

##### `remove-volume`

Execute `virtctl removevolume` on the VMs mapped by the `labelSelector`.
Additional parameters should be set using the `inputVars` field:

- `volumeName` - Name of the volume to remove. Mandatory
- `persist` - if set, the added volume will be persisted in the VM spec (if it exists). Default `false`

#### Wait for completion

Wait is supported for the following operations:

- `start` - Wait for the `Ready` state of the `VirtualMachine`  to become `True`
- `stop` - Wait for the  `Ready` state of the `VirtualMachine` state to become `False` with `reason` equal to `VMINotExists`
- `restart` - Wait for the `Ready` state of the `VirtualMachine`  to become `True`
- `pause` - Wait for the `Paused` state of the `VirtualMachine`  to become `True`
- `unpause` - Wait for the `Ready` state of the `VirtualMachine`  to become `True`
- `migrate` - Wait for the `Ready` state of the `VirtualMachine`  to become `True`

!!! note
    The waiter makes sure that the `lastTransitionTime` of the condition is after the time of the command.
    This requires that the timestamps on the cluster side are in UTC


## Execution Modes

Patch jobs support different execution modes

- `parallel` - run all steps without any waiting between objects or iterations
- `sequential` - Run for each object before moving to the next job iteration with an optional wait between objects and/or between iterations

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
- `RunID`: Internal run id. Can be used to match resources for metrics collection

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

On top of the default [golang template semantics](https://golang.org/pkg/text/template/), `kube-burner` supports additional template functions.

### External libraries

- [sprig library](http://masterminds.github.io/sprig/) which adds over 70 template functions for Go’s template language.

### Additional functions

- `Binomial` - returns the binomial coefficient of (n,k)
- `IndexToCombination` - returns the combination corresponding to the given index
- `GetSubnet24`
- `GetIPAddress` - returns number of addresses requested per iteration from the list of total provided addresses
- `ReadFile` - returns the content of the file in the provided path

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

## MetricsClosing

This config defines when the metrics collection should stop. The option supports three values:

- `afterJob` - collect metrics after the job completes
- `afterJobPause` - collect metrics after the jobPause duration ends (Default)
- `afterMeasurements` - collect metrics after all measurements are finished
