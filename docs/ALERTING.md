# Configuration

Kube-burner includes an alert mechanism able to evaluate Prometheus expressions after the end of the latest Kube-burner's job. Alerting is configured through a configuration file pointed by the flag `--alert-profile` or `-a`. This file looks like:

```yaml
- expr: avg_over_time(histogram_quantile(0.99, rate(etcd_disk_wal_fsync_duration_seconds_bucket[2m]))[5m:]) > 0.01
  description: 5 minutes avg. etcd fsync latency on {{$labels.pod}} higher than 10ms {{$value}}
  severity: error                                      
                                                           
- expr: avg_over_time(histogram_quantile(0.99, rate(etcd_network_peer_round_trip_time_seconds_bucket[5m]))[5m:]) > 0.1
  description: 5 minutes avg. etcd netowrk peer round trip on {{$labels.pod}} higher than 100ms {{$value}}
  severity: error

- expr: increase(etcd_server_leader_changes_seen_total[2m]) > 0
  description: etcd leader changes observed
  severity: error
```

Where expr holds the Prometheus expression to evaluate and description holds a description of the alert. In the description we can make use of prometheus labels to improve verbosity, using the syntax `{{$labels.<label_name>}}` and print the expression value that triggered the alarm using `{{$value}}`.
Alarm can be configured with a severity. Each one with different effects. At the moment they do the following:

- info: Prints an info message with the alarm description to stdout. By default all expressions have this severity.
- warning: Prints a warning message with the alarm description to stdout.
- error: Prints a error message with the alarm description to stdout and makes kube-burner rc = 1
- critical: Prints a fatal message with the alarm description to stdout and exits execution inmediatly with rc != 0

# Checking alerts

It's possible to look for alerts w/o triggering a kube-burner workload. To do so you can use the `check-alerts` option from the CLI, similar to the `index` CLI option, this one accepts the flags `--start` and `--end` to evaluate the alerts at a given time range.

```shell
$ kube-burner check-alerts -u https://prometheus.url.com -t ${token} -a alert-profile.yml                       
INFO[2020-12-10 11:47:23] Setting log level to info                                                                                                                                                                                           
INFO[2020-12-10 11:47:23] 👽 Initializing prometheus client                                                                                                                                                                                   
INFO[2020-12-10 11:47:24] 🔔 Initializaing alert manager                                                                                                                                                                                      
INFO[2020-12-10 11:47:24] Evaluating expression: 'avg_over_time(histogram_quantile(0.99, rate(etcd_disk_wal_fsync_duration_seconds_bucket[2m]))[5m:]) > 0.01'                                                                                 
ERRO[2020-12-10 11:47:24] Alert triggered at 2020-12-10 11:01:53 +0100 CET: '5 minutes avg. etcd fsync latency on etcd-ip-10-0-213-209.us-west-2.compute.internal higher than 10ms 0.010281314285714311' 
INFO[2020-12-10 11:47:24] Evaluating expression: 'avg_over_time(histogram_quantile(0.99, rate(etcd_network_peer_round_trip_time_seconds_bucket[5m]))[5m:]) > 0.1'                                                                             
INFO[2020-12-10 11:47:24] Evaluating expression: 'increase(etcd_server_leader_changes_seen_total[2m]) > 0'                                                                                                                                    
INFO[2020-12-10 11:47:24] Evaluating expression: 'avg_over_time(histogram_quantile(0.99, sum(apiserver_request_duration_seconds_bucket{apiserver="kube-apiserver",verb=~"POST|PUT|DELETE|PATCH|CREATE"}) by (verb,resource,subresource,le))[5m
:]) > 1'                                                                                                                                                                                                                                      
INFO[2020-12-10 11:47:25] Evaluating expression: 'avg_over_time(histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket{apiserver="kube-apiserver",verb="GET",scope="resource"}[2m])) by (verb,resource,subresource,le))[5
m:]) > 1'                                                                                                                                                                                                                                     
INFO[2020-12-10 11:47:25] Evaluating expression: 'avg_over_time(histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket{apiserver="kube-apiserver",verb="LIST",scope="namespace"}[2m])) by (verb,resource,subresource,le))
[5m:]) > 5'                                                                                                                                                                                                                                   
INFO[2020-12-10 11:47:26] Evaluating expression: 'avg_over_time(histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket{apiserver="kube-apiserver",verb="LIST",scope="cluster"}[2m])) by (verb,resource,subresource,le))[5
m:]) > 30'                                                                                                                                                                                                                                    
INFO[2020-12-10 11:47:27] Evaluating expression: 'avg_over_time(histogram_quantile(0.99,rate(coredns_kubernetes_dns_programming_duration_seconds_bucket[2m]))[5m:]) > 1'
```
