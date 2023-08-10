# Kube-Burner Roadmap

This document is meant to define high-level plans for the kube-burner project and is neither complete nor prescriptive. Following are a list of enhancements that we are planning to work on adding support to our application. Each and every action item is tracked using github [issues](https://github.com/cloud-bulldozer/kube-burner/issues).


- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/409) Funtionality to compare kube-burner collected metrics to detect regressions automatically.
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/385) Provide index subcommand for OCP wrapper. This will enable us to do an automated discovery and helps us fetch cluster metadata without any manual intervention. 
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/389) Check if the cluster image-registry is up and running prior running workload with builds and stop the benchmark by logging the status message. Especially useful for bare metal use cases where the internal registry is not configured by default. 
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/403) To improvise our error handling and index metrics locally in case of failed run. This will help us to debug/investigate the root cause for repetitive failures.
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/402) Create directories with unique name for local indexing instead of overriding the same for every run.
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/408) Check if ingress controller is up & running prior running a workload with routes and stop the benchmark by logging the status message. Especially useful for some of the workloads in OCP wrapper. 
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/384) To simplify index subcommand usage by removing the need for a config file. Parameters should be minimal and should go through CLI.
- [ ] [[RFE Optimization]](https://github.com/cloud-bulldozer/kube-burner/issues/399) Improve client-go retry logic to continuously retry for creating resources until the benchmark job timeout.
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/374) A new workload with cluster maximums for OCP. 
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/332) Use the current working directory to get configuration files and run the workload. Similar RFE.
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/141) Add a global waitWhenFinished flag to wait until all the resources in all our jobs are created and are in running state instead of doing it per job.
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/138) A standalone measure command to just fetch measurements of a workload.
- [ ] [[RFE Enhancement]](https://github.com/cloud-bulldozer/kube-burner/issues/248) Functionality to capture prometheus dump inspired by [promdump](https://github.com/ihcsim/promdump) tool.
- [ ] [[RFE CI/CD]](https://github.com/cloud-bulldozer/kube-burner/issues/112) To have unit tests implemented in place as our application is growing continously.