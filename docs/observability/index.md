# Overview

Performing a benchmark using kube-burner is relatively simple. However, it is sometimes necessary to analyze and be able to react to some KPIs in order to validate a benchmark. That  is why kube-burner ships  [metric-collection](metrics.md) and [alerting](alerting.md) systems based on Prometheus expressions.

Kube-burner also ships an [indexing](indexing.md) feature that, in combination with the metric-collection and alerting features, can be used to analyze these KPIs in an external tool, such as [Grafana](https://grafana.com/) or similar.

The benchmark stages include the following:

```mermaid
flowchart TD
    A[/"Init benchmark"/] -- Read config --> B(Start measurements)
    B --> C(Run Job)
    C --> D{Next job?}
    D --> |Yes| C
    D --> |No| E[Stop measurements]
    E --> F[Evaluate alerts]
    F --> G[(Index results)]
    G --> H[Indexing]
    H --> I*[/End/]
```
