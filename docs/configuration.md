# Configuration

All the magic `kube-burner` does is described in the configuration file. As previously mentioned the location of this configuration file is provided by the flag `-c`. This file is written in YAML format and consists of several sections.
It's possible to use `go-template` syntax within this configuration file, also it's important to note that every environment variable is passed to this template, so we can reference them using the syntax `{{ .MY_ENV_VAR}}`. For example, we could define the indexerConfig section of our configuration file like:

```yaml
  enabled: true
  type: elastic
  esServers: [{{ .ES_SERVER }}]
  defaultIndex: elasticsearch-index
```

This feature can be very useful at the time of defining secrets such as the user and password of our indexer, or a token to use in pprof collection.

## Global

In this section is described global job configuration, it holds the following parameters:

| Option           | Description                                                                                              | Type           | Example        | Default     |
|------------------|----------------------------------------------------------------------------------------------------------|----------------|----------------|-------------|
| kubeconfig       | Points to a valid kubeconfig file. Can be omitted if using the KUBECONFIG environment variable, or running from a pod | String  | ~/mykubeconfig | in-cluster |             |
| writeToFile      | Whether to dump collected metrics to files                                                               | Boolean        | true           | true        |
| metricsDirectory | Directory where collected metrics will be dumped into. It will be created if it doesn't exist previously | String         | ./metrics      | ./collected-metrics |
| measurements     | List of measurements. Detailed in the [measurements section]                                             | List           | -              | []          |
| indexerConfig    | Holds the indexer configuration. Detailed in the [indexers section]                                      | Object         | -              | -           |
| requestTimeout   | Client-go request timeout                                                                                | Duration       | 5s             | 15s         |

## Jobs

This section contains the list of jobs `kube-burner` will execute. Each job can hold the following parameters.

| Option               | Description                                                                      | Type    | Example  | Default |
|----------------------|----------------------------------------------------------------------------------|---------|----------|---------|
| name                 | Job name                                                                         | String  | myjob    | ""      |
| jobType              | Type of job to execute. More details at [job types](#job-types)                  | string  | create   | create  |
| jobIterations        | How many times to execute the job                                                | Integer | 10       | 0       |
| namespace            | Namespace base name to use                                                       | String  | firstjob | ""      |
| namespacedIterations | Whether to create a namespace per job iteration                                  | Boolean | true     | true    |
| cleanup              | Cleanup clean up old namespaces                                                  | Boolean | true     | true    |
| podWait              | Wait for all pods to be running before moving forward to the next job iteration  | Boolean | true     | true    |
| waitWhenFinished     | Wait for all pods to be running when all iterations are completed                | Boolean | true     | false   |
| maxWaitTimeout       | Maximum wait timeout in seconds. (If podWait is enabled this timeout will be reseted with each iteration) | Integer | 1h     | 12h |
| waitFor              | List containing the objects Kind wait for. Wait for all if empty                 | List    | ["Deployment", "Build", "DaemonSet"]| []      |
| jobIterationDelay    | How long to wait between each job iteration                                      | Duration| 2s       | 0s      |
| jobPause             | How long to pause after finishing the job                                        | Duration| 10s      | 0s      |
| qps                  | Limit object creation queries per second                                         | Integer | 25       | 0       |
| burst                | Maximum burst for throttle                                                       | Integer | 50       | 0       |
| objects              | List of objects the job will create. Detailed on the [objects section](#objects) | List    | -        | []      |
| verifyObjects        | Verify object count after running each job. Return code will be 1 if failed      | Boolean | true     | true    |
| errorOnVerify        | Exit with rc 1 before indexing when objects verification fails                   | Boolean | true     | false   |


A valid example of a configuration file can be found at the [examples](https://github.com/cloud-bulldozer/kube-burner/tree/master/examples) folder.

## Objects

The objects created by `kube-burner` are rendered using the default golang's [template library](https://golang.org/pkg/text/template/).
Each object element supports the following parameters:

| Option               | Description                                                       | Type    | Example                                             | Default |
|----------------------|-------------------------------------------------------------------|---------|-----------------------------------------------------|---------|
| objectTemplate       | Object template file or URL                                       | String  | deployment.yml or https://domain.com/deployment.yml | ""      |
| replicas             | How replicas of this object to create per job iteration           | Integer | 10                                                  | -       |
| inputVars            | Map of arbitrary input variables to inject to the object template | Object  | -                                                   | -       |



### Default labels

All objects created by kube-burner are labeled with. `kube-burner-uuid=<UUID>,kube-burner-job=<jobName>,kube-burner-index=<objectIndex>`, these labels are appended to the actual object labels described at the template. They are used for internal purposes but they can also be used by the users.


## Job types

kube-burner support two types of jobs with different parameters each. The default job type is __create__. Which basically creates objects as described in the section [objects](#objects).

The other type is __delete__, this type of job deletes objects described in the objects list. Using delete as job type the objects list would have the following structure.

```yaml
objects:
- kind: Deployment
  labelSelector: {kube-burner-job: cluster-density}
  apiVersion: apps/v1

- kind: Secret
  labelSelector: {kube-burner-job: cluster-density}
```

Where:

- kind: Object kind of the k8s object to delete.
- labelSelector: Map with the labelSelector.
- apiVersion: API version from the k8s object.

As mentioned previously, all objects created by kube-burner are labeled with `kube-burner-uuid=<UUID>,kube-burner-job=<jobName>,kube-burner-index=<objectIndex>`. Thanks to this we could design a workload with one job to create objects and another one able to remove the objects created by the previous

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

This job type supports the some of the same parameters as the create job type:

- **waitForDeletion**: Wait for objects to be deleted before finishing the job. Defaults to true
- name
- qps
- burst
- jobPause

## Injected variables

All object templates are injected a series of variables by default:

- Iteration: Job iteration number.
- Replica: Object replica number. Keep in mind that this number is reset to 1 with each job iteration.
- JobName: Job name.
- UUID: Benchmark UUID.

In addition, you can also inject arbitrary variables with the option **inputVars** from the objectTemplate object:

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

It's worth to say that you can also use [golang template semantics](https://golang.org/pkg/text/template/) in your *objectTemplate* files.

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

## Template functions

Apart from the default [golang template semantics](https://golang.org/pkg/text/template/), Kube-burner ships several functions to provide higher dynamism to the template language:
- add: Add two integers

```yaml
apiVersion: v1
data:
  two: {{add 1 1}}
  anotherInt: {{add .inputIntVariable 1}}
kind: ConfigMap
metadata:
  name: configmap-{{.Replica}}
```

- multiply: Multiply two integers

```yaml
apiVersion: v1
data:
  eight: {{multiply 2 4}}
  anotherInt: {{multiply .inputIntVariable 5}}
kind: ConfigMap
metadata:
  name: configmap-{{.Replica}}
```

- randInteger: Generates a positive random integer between two numbers.

```yaml
apiVersion: v1
data:
  number: {{randInteger 0 100}}
kind: ConfigMap
metadata:
  name: configmap-{{.Replica}}
```

- rand: This function can be used to generate a random string with the given length. i.e

```yaml
apiVersion: v1
data:
  myfile: {{rand 512}}
  myOtherFile: {{rand .inputIntVariable}}
kind: ConfigMap
metadata:
  name: configmap-{{.Replica}}
```

- sequence: This function can be used to generate an array with elements to loop over

```yaml
apiVersion: v1
data:
  blah: "This has many labels"
kind: ConfigMap
metadata:
  name: configmap-{{.Replica}}
  labels:
    {{ range $index, $element := sequence 1 10 }}
    label-{{ $element }}: "true"
    {{ end }}
```

[measurements section]: ../measurements/
[indexers section]: ../indexers/
